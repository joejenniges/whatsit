package transcriber

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// Compile-time check that ParakeetEngine implements ASREngine.
var _ ASREngine = (*ParakeetEngine)(nil)

// ortInitOnce ensures the ONNX Runtime environment is initialized exactly once.
// Multiple ParakeetEngine instances (or recreation after Close) share the same
// runtime environment because onnxruntime_go uses a global singleton.
var ortInitOnce sync.Once

// ParakeetConfig holds settings for creating a ParakeetEngine.
type ParakeetConfig struct {
	ModelPath  string // path to model.onnx
	VocabPath  string // path to vocab.txt
	WindowSize int    // samples for rolling window (e.g. 160000 = 10s)
	WindowStep int    // samples for rolling step (e.g. 48000 = 3s)
}

// ParakeetEngine implements ASREngine using NVIDIA Parakeet CTC via ONNX Runtime.
//
// The pipeline: audio -> preemphasis -> mel spectrogram -> ONNX encoder ->
// CTC greedy decode -> text.
type ParakeetEngine struct {
	session *ort.DynamicAdvancedSession
	vocab   []string
	blankID int
	fb      *melFilterbank
	mu      sync.Mutex

	// Rolling window state (same pattern as whisper Transcriber).
	window     []float32
	newSamples int
	prevText   string
	config     ParakeetConfig
}

// NewParakeetEngine loads the Parakeet ONNX model and vocabulary.
// The ONNX Runtime shared library (onnxruntime.dll) must be next to the
// executable or on the system PATH.
func NewParakeetEngine(cfg ParakeetConfig) (*ParakeetEngine, error) {
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("parakeet: model path is required")
	}
	if cfg.VocabPath == "" {
		return nil, fmt.Errorf("parakeet: vocab path is required")
	}
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = 160000 // 10s at 16kHz
	}
	if cfg.WindowStep <= 0 {
		cfg.WindowStep = 48000 // 3s at 16kHz
	}

	// Load vocabulary.
	vocab, err := loadVocab(cfg.VocabPath)
	if err != nil {
		return nil, fmt.Errorf("parakeet: load vocab: %w", err)
	}

	// The blank token is the last token in the NeMo CTC vocab.
	blankID := len(vocab) - 1
	log.Printf("parakeet: loaded vocab with %d tokens, blank_id=%d", len(vocab), blankID)

	// Initialize ONNX Runtime (once per process).
	var initErr error
	ortInitOnce.Do(func() {
		// WHY: SetSharedLibraryPath must be called before InitializeEnvironment.
		// We look for onnxruntime.dll next to the executable first, then fall
		// back to the default (which searches PATH / system dirs).
		dllPath := findOrtDLL()
		if dllPath != "" {
			ort.SetSharedLibraryPath(dllPath)
			log.Printf("parakeet: using ONNX Runtime at %s", dllPath)
		}
		initErr = ort.InitializeEnvironment()
		if initErr != nil {
			log.Printf("parakeet: ONNX Runtime init error: %v", initErr)
		}
	})
	if initErr != nil {
		return nil, fmt.Errorf("parakeet: init onnx runtime: %w", initErr)
	}

	// Create ONNX session.
	// WHY DynamicAdvancedSession: Input tensor dimensions vary with audio
	// length (n_frames changes). DynamicAdvancedSession lets us provide
	// differently-shaped tensors on each Run() call.
	session, err := ort.NewDynamicAdvancedSession(
		cfg.ModelPath,
		[]string{"audio_signal", "length"},
		[]string{"logprobs"},
		nil, // default session options (CPU provider)
	)
	if err != nil {
		return nil, fmt.Errorf("parakeet: create session: %w", err)
	}

	// Build mel filterbank (reused across all inference calls).
	fb := newMelFilterbank(80, melNFFT, melSampleRate)

	log.Printf("parakeet: engine ready (model=%s)", cfg.ModelPath)
	return &ParakeetEngine{
		session: session,
		vocab:   vocab,
		blankID: blankID,
		fb:      fb,
		config:  cfg,
	}, nil
}

// Transcribe runs Parakeet inference on a complete audio segment.
// Input: 16kHz mono float32 PCM samples.
func (p *ParakeetEngine) Transcribe(samples []float32) (string, error) {
	if len(samples) == 0 {
		return "", nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	return p.infer(samples)
}

// FeedChunk implements rolling-window transcription, matching the whisper
// Transcriber pattern: accumulate audio, trigger inference when enough new
// audio has arrived, return only the new text.
func (p *ParakeetEngine) FeedChunk(chunk []float32) (string, bool, error) {
	if len(chunk) == 0 {
		return "", false, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.window = append(p.window, chunk...)
	p.newSamples += len(chunk)

	if len(p.window) < p.config.WindowSize {
		return "", false, nil
	}
	if p.newSamples < p.config.WindowStep {
		return "", false, nil
	}

	fullText, err := p.infer(p.window)
	if err != nil {
		return "", false, err
	}

	newText := diffText(p.prevText, fullText)

	if p.config.WindowStep < len(p.window) {
		p.window = p.window[p.config.WindowStep:]
	} else {
		p.window = p.window[:0]
	}
	p.newSamples = 0
	p.prevText = fullText

	return newText, true, nil
}

// ClassifyChunk runs inference and returns text + average probability.
// Parakeet CTC outputs log probabilities, so we convert to probabilities
// for the avg. This is a best-effort implementation -- the whisper classifier
// was designed around whisper's token probabilities, so results may differ.
func (p *ParakeetEngine) ClassifyChunk(samples []float32) (string, float32, error) {
	if len(samples) == 0 {
		return "", 0, nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	text, err := p.infer(samples)
	if err != nil {
		return "", 0, err
	}

	// WHY return 0.5 as a fixed confidence: Parakeet CTC logprobs aren't
	// directly comparable to whisper token probabilities. The classifier
	// tier that uses ClassifyChunk was tuned for whisper. Returning a
	// mid-range value prevents the classifier from making bad decisions
	// based on incompatible probability scales. A proper implementation
	// would need classifier threshold retuning for Parakeet.
	return text, 0.5, nil
}

// Reset clears the rolling window state.
func (p *ParakeetEngine) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.window = p.window[:0]
	p.newSamples = 0
	p.prevText = ""
}

// Close releases the ONNX session. The engine must not be used after Close.
// Note: we do NOT call ort.DestroyEnvironment() because it's a global
// singleton and other sessions might still be active.
func (p *ParakeetEngine) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.session != nil {
		p.session.Destroy()
		p.session = nil
	}
}

// infer runs the full pipeline: mel spectrogram -> ONNX -> CTC decode.
// Caller must hold p.mu.
func (p *ParakeetEngine) infer(samples []float32) (string, error) {
	if p.session == nil {
		return "", fmt.Errorf("parakeet: engine is closed")
	}

	// 1. Compute log-mel spectrogram.
	melData, nFrames := computeLogMelSpectrogram(samples, melSampleRate, 80, p.fb)
	if nFrames == 0 {
		return "", nil
	}

	// 2. Create input tensors.
	// audio_signal shape: [1, n_mels, n_frames]
	audioTensor, err := ort.NewTensor(ort.NewShape(1, 80, int64(nFrames)), melData)
	if err != nil {
		return "", fmt.Errorf("parakeet: create audio tensor: %w", err)
	}
	defer audioTensor.Destroy()

	// length shape: [1] -- number of frames (before subsampling).
	lengthData := []int64{int64(nFrames)}
	lengthTensor, err := ort.NewTensor(ort.NewShape(1), lengthData)
	if err != nil {
		return "", fmt.Errorf("parakeet: create length tensor: %w", err)
	}
	defer lengthTensor.Destroy()

	// 3. Run inference. Output is allocated by ONNX Runtime.
	outputTensors := []ort.Value{nil}
	err = p.session.Run(
		[]ort.Value{audioTensor, lengthTensor},
		outputTensors,
	)
	if err != nil {
		return "", fmt.Errorf("parakeet: inference: %w", err)
	}

	// Extract output tensor. The session allocated it, so we must destroy it.
	if outputTensors[0] == nil {
		return "", nil
	}
	defer outputTensors[0].Destroy()

	// WHY type assertion: DynamicAdvancedSession.Run with nil output allocates
	// the tensor internally. We need to cast it to *Tensor[float32] to access
	// the data. If the model outputs a different type, this will fail.
	outTensor, ok := outputTensors[0].(*ort.Tensor[float32])
	if !ok {
		return "", fmt.Errorf("parakeet: unexpected output tensor type")
	}

	logits := outTensor.GetData()
	shape := outTensor.GetShape()

	// shape should be [1, output_frames, vocab_size]
	if len(shape) != 3 {
		return "", fmt.Errorf("parakeet: expected 3D output, got %d dims", len(shape))
	}
	outFrames := int(shape[1])
	vocabSize := int(shape[2])

	// 4. CTC greedy decode.
	text := ctcGreedyDecode(logits, outFrames, vocabSize, p.vocab, p.blankID)
	return text, nil
}

// loadVocab reads a NeMo-style vocab.txt file.
// Format: one line per token, either "token index" or just "token".
// The blank token <blk> is appended if not present.
func loadVocab(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var vocab []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		if line == "" {
			continue
		}
		// Format: "token index" -- take the token part.
		parts := strings.Fields(line)
		if len(parts) >= 1 {
			vocab = append(vocab, parts[0])
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	// Ensure blank token is at the end (NeMo convention).
	if len(vocab) == 0 || vocab[len(vocab)-1] != "<blk>" {
		vocab = append(vocab, "<blk>")
	}

	return vocab, nil
}

// findOrtDLL looks for onnxruntime.dll next to the executable.
func findOrtDLL() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	candidate := filepath.Join(filepath.Dir(exe), "onnxruntime.dll")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}
