package classifier

import (
	"fmt"
	"log"
	"math"
	"sort"
	"sync"

	ort "github.com/yalue/onnxruntime_go"
)

// CEDClassifier wraps the CED-tiny ONNX model for audio classification.
// It runs inference on PCM audio chunks and returns top-K AudioSet labels
// with speech/music/singing flags.
//
// WHY CED-tiny: 5.5M params, ~6MB int8 quantized, ~5-10ms inference on CPU
// for a 2-second chunk. Gives us speech/music classification AND genre
// identification in a single pass, with negligible overhead compared to
// the rhythm detector (<1ms) and whisper (300-5000ms).
type CEDClassifier struct {
	session     *ort.AdvancedSession
	inputTensor *ort.Tensor[float32]
	outputTensor *ort.Tensor[float32]
	labels      []string

	// Mel filterbank, precomputed once.
	melFB *MelFilterbank

	// Preprocessing params (must match CED's training config).
	sampleRate   int // 16000
	nMels        int // 64
	fftSize      int // 512
	hopLength    int // 160 (10ms at 16kHz)
	targetLength int // 1001 frames (~10 seconds, from CED feature extractor)

	mu sync.Mutex
}

// CEDResult holds classification results from a single inference.
type CEDResult struct {
	Labels     []LabelScore // Top-K labels sorted by confidence
	IsSpeech   bool         // Any speech label in top results
	IsMusic    bool         // Music label in top results
	IsSinging  bool         // Singing label in top results
	Genre      string       // Best music genre if music detected
	GenreScore float64      // Confidence of genre classification
	TopLabel   string       // Highest confidence label
	TopScore   float64      // Confidence of top label
}

// LabelScore pairs a label name with its sigmoid confidence score.
type LabelScore struct {
	Label string
	Score float64 // 0.0 to 1.0 (sigmoid output)
}

// cedOrtInitOnce ensures the ONNX Runtime environment is initialized.
// WHY separate from parakeet's ortInitOnce: They're in different packages.
// ort.InitializeEnvironment() is idempotent if already called, but we guard
// with sync.Once anyway to avoid the overhead of repeated calls.
var cedOrtInitOnce sync.Once
var cedOrtInitErr error

// initCEDOrt initializes the ONNX Runtime for CED. If parakeet already
// initialized it (same process), InitializeEnvironment returns nil.
func initCEDOrt() error {
	cedOrtInitOnce.Do(func() {
		// The shared library path should already be set by parakeet or
		// by the application startup. If not, try to find it.
		cedOrtInitErr = ort.InitializeEnvironment()
		if cedOrtInitErr != nil {
			log.Printf("ced: ONNX Runtime init error: %v", cedOrtInitErr)
		}
	})
	return cedOrtInitErr
}

// NewCEDClassifier loads the CED-tiny ONNX model.
//
// The model expects:
//   - Input: "input" -- float32 tensor of shape [1, 64, 1001]
//     (batch=1, mel_bins=64, time_frames=1001)
//   - Output: "output" -- float32 tensor of shape [1, 527]
//     (batch=1, num_classes=527)
//
// WHY [1, 64, 1001] not [1, 1024, 64]: The CED feature extractor produces
// mel spectrograms in [mel_bins, time_frames] layout. Verified by running
// the HuggingFace model with AutoFeatureExtractor.
func NewCEDClassifier(modelPath string) (*CEDClassifier, error) {
	if err := initCEDOrt(); err != nil {
		return nil, fmt.Errorf("ced: init onnx runtime: %w", err)
	}

	labels := LoadAudioSetLabels()
	numClasses := int64(len(labels))

	inputShape := ort.NewShape(1, 64, 1001)
	outputShape := ort.NewShape(1, numClasses)

	inputTensor, err := ort.NewEmptyTensor[float32](inputShape)
	if err != nil {
		return nil, fmt.Errorf("ced: create input tensor: %w", err)
	}

	outputTensor, err := ort.NewEmptyTensor[float32](outputShape)
	if err != nil {
		inputTensor.Destroy()
		return nil, fmt.Errorf("ced: create output tensor: %w", err)
	}

	session, err := ort.NewAdvancedSession(
		modelPath,
		[]string{"input"},
		[]string{"output"},
		[]ort.ArbitraryTensor{inputTensor},
		[]ort.ArbitraryTensor{outputTensor},
		nil, // default session options (CPU provider)
	)
	if err != nil {
		inputTensor.Destroy()
		outputTensor.Destroy()
		return nil, fmt.Errorf("ced: create session: %w", err)
	}

	// Build mel filterbank once (64 mels, 512 FFT, 16kHz).
	melFB := NewMelFilterbank(64, 512, 16000)

	log.Printf("ced: model loaded (path=%s, labels=%d)", modelPath, len(labels))
	return &CEDClassifier{
		session:      session,
		inputTensor:  inputTensor,
		outputTensor: outputTensor,
		labels:       labels,
		melFB:        melFB,
		sampleRate:   16000,
		nMels:        64,
		fftSize:      512,
		hopLength:    160,
		targetLength: 1001,
	}, nil
}

// Classify runs inference on a PCM audio chunk.
//
// Parameters:
//   - samples: []float32 PCM audio, 16kHz mono, any length
//     (will be padded/truncated to 10.24 seconds internally)
//
// Algorithm:
//  1. Compute log-mel spectrogram (64 mel bins, 10ms hop)
//  2. Pad or truncate to 1024 frames
//  3. Run ONNX inference
//  4. Apply sigmoid to output logits
//  5. Build result with top-K labels and flags
func (c *CEDClassifier) Classify(samples []float32) (*CEDResult, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Step 1: Compute log-mel spectrogram.
	melSpec := c.computeMelSpectrogram(samples)

	// Step 2: Pad or truncate to target length (1001 frames).
	melSpec = padOrTruncate(melSpec, c.targetLength, c.nMels)

	// Step 3: Copy mel spectrogram into input tensor.
	// WHY [mel_bins, time_frames] layout: CED's ONNX model expects [1, 64, 1001].
	// melSpec is [time_frames][mel_bins], so we transpose during copy.
	inputData := c.inputTensor.GetData()
	for m := 0; m < c.nMels; m++ {
		for t := 0; t < c.targetLength; t++ {
			inputData[m*c.targetLength+t] = melSpec[t][m]
		}
	}

	// Step 4: Run inference.
	if err := c.session.Run(); err != nil {
		return nil, fmt.Errorf("ced: inference: %w", err)
	}

	// Step 5: Get output and apply sigmoid.
	outputData := c.outputTensor.GetData()
	scores := make([]float64, len(outputData))
	for i, logit := range outputData {
		scores[i] = 1.0 / (1.0 + math.Exp(-float64(logit)))
	}

	return c.buildResult(scores), nil
}

// computeMelSpectrogram computes a 64-bin log-mel spectrogram.
//
// This must match CED's training preprocessing:
//   - 16kHz sample rate
//   - 512-point FFT (32ms window)
//   - 160-sample hop (10ms)
//   - 64 mel filterbanks
//   - Log scaling: log(mel + 1e-7)
//   - No normalization (CED handles this internally via patch embedding)
//
// Returns [][]float32 of shape [num_frames][64].
func (c *CEDClassifier) computeMelSpectrogram(samples []float32) [][]float32 {
	numFrames := (len(samples) - c.fftSize) / c.hopLength + 1
	if numFrames <= 0 {
		return nil
	}

	bins := c.fftSize/2 + 1
	fftBufSize := nextPow2(c.fftSize) // should be 512 already (power of 2)

	result := make([][]float32, numFrames)

	for i := 0; i < numFrames; i++ {
		start := i * c.hopLength

		// Build complex buffer with Hann window applied.
		buf := make([]complex128, fftBufSize)
		for j := 0; j < c.fftSize && start+j < len(samples); j++ {
			w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(j)/float64(c.fftSize-1)))
			buf[j] = complex(float64(samples[start+j])*w, 0)
		}

		// In-place FFT using existing radix-2 implementation from spectral.go.
		fft(buf)

		// Power spectrum (positive frequencies only).
		powerSpec := make([]float64, bins)
		for j := 0; j < bins; j++ {
			re := real(buf[j])
			im := imag(buf[j])
			powerSpec[j] = re*re + im*im
		}

		// Apply mel filterbank.
		melEnergies := c.melFB.Apply(powerSpec)

		// Log scaling.
		melFrame := make([]float32, c.nMels)
		for m := 0; m < c.nMels; m++ {
			melFrame[m] = float32(math.Log(melEnergies[m] + 1e-7))
		}

		result[i] = melFrame
	}

	return result
}

// padOrTruncate ensures the spectrogram is exactly targetLen frames.
// Pads with zeros (silence) or truncates from the end.
func padOrTruncate(spec [][]float32, targetLen, nMels int) [][]float32 {
	if len(spec) >= targetLen {
		return spec[:targetLen]
	}
	for len(spec) < targetLen {
		spec = append(spec, make([]float32, nMels))
	}
	return spec
}

// buildResult interprets the 527-class sigmoid scores into a useful result.
func (c *CEDClassifier) buildResult(scores []float64) *CEDResult {
	type idxScore struct {
		idx   int
		score float64
	}
	ranked := make([]idxScore, len(scores))
	for i, s := range scores {
		ranked[i] = idxScore{i, s}
	}
	sort.Slice(ranked, func(i, j int) bool {
		return ranked[i].score > ranked[j].score
	})

	topK := 10
	if topK > len(ranked) {
		topK = len(ranked)
	}

	result := &CEDResult{
		Labels:   make([]LabelScore, topK),
		TopLabel: c.labels[ranked[0].idx],
		TopScore: ranked[0].score,
	}

	for i := 0; i < topK; i++ {
		result.Labels[i] = LabelScore{
			Label: c.labels[ranked[i].idx],
			Score: ranked[i].score,
		}
	}

	// Check for speech/music/singing in top results.
	for i := 0; i < topK; i++ {
		label := c.labels[ranked[i].idx]
		score := ranked[i].score

		if score < 0.1 {
			continue // ignore low-confidence labels
		}

		if isSpeechLabel(label) {
			result.IsSpeech = true
		}
		if isMusicLabel(label) {
			result.IsMusic = true
		}
		if isSingingLabel(label) {
			result.IsSinging = true
		}
		if result.Genre == "" && isGenreLabel(label) {
			result.Genre = label
			result.GenreScore = score
		}
	}

	return result
}

// Destroy cleans up the ONNX session and tensors.
func (c *CEDClassifier) Destroy() {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.session != nil {
		c.session.Destroy()
		c.session = nil
	}
	if c.inputTensor != nil {
		c.inputTensor.Destroy()
		c.inputTensor = nil
	}
	if c.outputTensor != nil {
		c.outputTensor.Destroy()
		c.outputTensor = nil
	}
}
