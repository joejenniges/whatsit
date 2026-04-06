package transcriber

// #cgo pkg-config: whisper
// #cgo LDFLAGS: -lm -lstdc++ -lpthread
// #include <whisper.h>
// #include <stdlib.h>
//
// // Helper to configure whisper_full_params via C because the struct is large
// // and has nested fields that are awkward to manipulate from Go via CGo.
// static struct whisper_full_params make_params(int n_threads, const char *lang) {
//     struct whisper_full_params params = whisper_full_default_params(WHISPER_SAMPLING_GREEDY);
//     params.n_threads       = n_threads;
//     params.no_context      = true;
//     params.single_segment  = true;
//     params.print_special   = false;
//     params.print_progress  = false;
//     params.print_realtime  = false;
//     params.print_timestamps = false;
//     params.suppress_blank  = true;
//     params.suppress_nst    = true;
//     params.language        = lang;
//     params.translate       = false;
//     return params;
// }
//
// // WHY: Rolling-window mode uses no_context=false so whisper can leverage
// // earlier audio in the window for better context, and single_segment=false
// // so it can find natural segment boundaries within the full window.
// static struct whisper_full_params make_params_rolling(int n_threads, const char *lang) {
//     struct whisper_full_params params = whisper_full_default_params(WHISPER_SAMPLING_GREEDY);
//     params.n_threads       = n_threads;
//     params.no_context      = false;
//     params.single_segment  = false;
//     params.print_special   = false;
//     params.print_progress  = false;
//     params.print_realtime  = false;
//     params.print_timestamps = false;
//     params.suppress_blank  = true;
//     params.suppress_nst    = true;
//     params.language        = lang;
//     params.translate       = false;
//     return params;
// }
import "C"

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"unsafe"
)

// TranscriberConfig holds settings for creating a Transcriber.
type TranscriberConfig struct {
	ModelPath  string
	Language   string // e.g. "en", "auto", or "" (defaults to "en")
	Threads    int    // 0 means runtime.NumCPU()
	WindowSize int    // samples (window_size_secs * 16000), default 160000 (10s)
	WindowStep int    // samples (window_step_secs * 16000), default 48000 (3s)
}

// Transcriber wraps a whisper.cpp context for speech-to-text.
// It is NOT safe for concurrent use -- access is serialized internally with a mutex.
type Transcriber struct {
	ctx    *C.struct_whisper_context
	config TranscriberConfig
	mu     sync.Mutex

	// Rolling window state for FeedChunk.
	window     []float32 // the current audio window
	newSamples int       // samples added since last transcription
	prevText   string    // previous full transcription (for diffing)
}

// NewTranscriber loads a whisper model from disk and returns a ready Transcriber.
func NewTranscriber(cfg TranscriberConfig) (*Transcriber, error) {
	if cfg.ModelPath == "" {
		return nil, fmt.Errorf("model path is required")
	}
	if cfg.Language == "" {
		cfg.Language = "en"
	}
	if cfg.Threads <= 0 {
		cfg.Threads = runtime.NumCPU()
	}
	if cfg.WindowSize <= 0 {
		cfg.WindowSize = 160000 // 10s at 16kHz
	}
	if cfg.WindowStep <= 0 {
		cfg.WindowStep = 48000 // 3s at 16kHz
	}

	cPath := C.CString(cfg.ModelPath)
	defer C.free(unsafe.Pointer(cPath))

	cparams := C.whisper_context_default_params()
	ctx := C.whisper_init_from_file_with_params(cPath, cparams)
	if ctx == nil {
		return nil, fmt.Errorf("failed to load whisper model from %s", cfg.ModelPath)
	}

	return &Transcriber{
		ctx:    ctx,
		config: cfg,
	}, nil
}

// Transcribe runs whisper inference on 16kHz mono float32 PCM samples.
// Returns the transcribed text, or empty string if nothing meaningful was detected.
func (t *Transcriber) Transcribe(samples []float32) (string, error) {
	if len(samples) == 0 {
		return "", nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ctx == nil {
		return "", fmt.Errorf("transcriber is closed")
	}

	cLang := C.CString(t.config.Language)
	defer C.free(unsafe.Pointer(cLang))

	params := C.make_params(C.int(t.config.Threads), cLang)

	ret := C.whisper_full(t.ctx, params, (*C.float)(&samples[0]), C.int(len(samples)))
	if ret != 0 {
		return "", fmt.Errorf("whisper_full failed with code %d", int(ret))
	}

	nSegments := int(C.whisper_full_n_segments(t.ctx))
	if nSegments == 0 {
		return "", nil
	}

	var parts []string
	for i := 0; i < nSegments; i++ {
		text := C.GoString(C.whisper_full_get_segment_text(t.ctx, C.int(i)))
		text = stripMusicNotes(text)
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if isHallucination(text, t.ctx, i) {
			continue
		}
		parts = append(parts, text)
	}

	result := strings.Join(parts, " ")
	result = strings.TrimSpace(result)
	return result, nil
}

// ClassifyChunk runs a single-shot whisper inference and returns the raw text
// and average token probability. Used by the whisper classifier tier to
// determine if audio is speech or music.
//
// WHY separate from Transcribe: Transcribe filters out hallucinations, but
// the whisper classifier NEEDS to see those markers (e.g., [Music]) to make
// classification decisions. ClassifyChunk returns the raw, unfiltered text
// and the average probability so the classifier can apply its own logic.
func (t *Transcriber) ClassifyChunk(samples []float32) (string, float32, error) {
	if len(samples) == 0 {
		return "", 0, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ctx == nil {
		return "", 0, fmt.Errorf("transcriber is closed")
	}

	cLang := C.CString(t.config.Language)
	defer C.free(unsafe.Pointer(cLang))

	// WHY make_params (not make_params_rolling): These are independent
	// classification chunks with no continuity between them.
	params := C.make_params(C.int(t.config.Threads), cLang)

	ret := C.whisper_full(t.ctx, params, (*C.float)(&samples[0]), C.int(len(samples)))
	if ret != 0 {
		return "", 0, fmt.Errorf("whisper_full failed with code %d", int(ret))
	}

	nSegments := int(C.whisper_full_n_segments(t.ctx))
	if nSegments == 0 {
		return "", 0, nil
	}

	var parts []string
	var totalProb float32
	var totalTokens int

	for i := 0; i < nSegments; i++ {
		text := C.GoString(C.whisper_full_get_segment_text(t.ctx, C.int(i)))
		text = strings.TrimSpace(text)
		if text != "" {
			parts = append(parts, text)
		}

		// Accumulate token probabilities across all segments.
		nTokens := int(C.whisper_full_n_tokens(t.ctx, C.int(i)))
		for j := 0; j < nTokens; j++ {
			totalProb += float32(C.whisper_full_get_token_p(t.ctx, C.int(i), C.int(j)))
			totalTokens++
		}
	}

	var avgProb float32
	if totalTokens > 0 {
		avgProb = totalProb / float32(totalTokens)
	}

	result := strings.TrimSpace(strings.Join(parts, " "))
	return result, avgProb, nil
}

// Close releases the whisper context. Safe to call multiple times.
func (t *Transcriber) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ctx != nil {
		C.whisper_free(t.ctx)
		t.ctx = nil
	}
}

// FeedChunk adds a speech chunk to the rolling window.
// If enough new audio has accumulated (>= WindowStep), triggers transcription
// and returns only the NEW text (diffed against previous transcription).
//
// Returns:
//   - text: only the newly transcribed portion (empty if no transcription ran)
//   - triggered: whether a transcription was actually performed
//   - error: any whisper error
func (t *Transcriber) FeedChunk(chunk []float32) (text string, triggered bool, err error) {
	if len(chunk) == 0 {
		return "", false, nil
	}

	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ctx == nil {
		return "", false, fmt.Errorf("transcriber is closed")
	}

	t.window = append(t.window, chunk...)
	t.newSamples += len(chunk)

	// Not enough audio to fill the window yet.
	if len(t.window) < t.config.WindowSize {
		return "", false, nil
	}

	// Not enough new audio since last transcription.
	if t.newSamples < t.config.WindowStep {
		return "", false, nil
	}

	// Transcribe the full window using rolling params.
	fullText, err := t.transcribeRolling(t.window)
	if err != nil {
		return "", false, err
	}

	// Diff against previous to extract only new content.
	newText := diffText(t.prevText, fullText)

	// Slide the window forward by WindowStep.
	if t.config.WindowStep < len(t.window) {
		t.window = t.window[t.config.WindowStep:]
	} else {
		t.window = t.window[:0]
	}
	t.newSamples = 0
	t.prevText = fullText

	return newText, true, nil
}

// Reset clears the rolling window and previous text.
// Call when switching from music back to speech to start fresh.
func (t *Transcriber) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()

	t.window = t.window[:0]
	t.newSamples = 0
	t.prevText = ""
}

// transcribeRolling runs whisper inference with rolling-window parameters.
// Caller must hold t.mu.
func (t *Transcriber) transcribeRolling(samples []float32) (string, error) {
	if len(samples) == 0 {
		return "", nil
	}

	cLang := C.CString(t.config.Language)
	defer C.free(unsafe.Pointer(cLang))

	params := C.make_params_rolling(C.int(t.config.Threads), cLang)

	ret := C.whisper_full(t.ctx, params, (*C.float)(&samples[0]), C.int(len(samples)))
	if ret != 0 {
		return "", fmt.Errorf("whisper_full failed with code %d", int(ret))
	}

	nSegments := int(C.whisper_full_n_segments(t.ctx))
	if nSegments == 0 {
		return "", nil
	}

	var parts []string
	for i := 0; i < nSegments; i++ {
		text := C.GoString(C.whisper_full_get_segment_text(t.ctx, C.int(i)))
		text = stripMusicNotes(text)
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		if isHallucination(text, t.ctx, i) {
			continue
		}
		parts = append(parts, text)
	}

	return strings.TrimSpace(strings.Join(parts, " ")), nil
}

// diffText extracts the new portion of currentText that wasn't in prevText.
// Uses word-level matching for robustness.
//
// Strategy: find the longest suffix of prevText words that matches a prefix
// of currentText words, then return everything after the overlap.
//
// Example:
//
//	prevText:    "The weather today will be"
//	currentText: "weather today will be sunny with highs"
//	result:      "sunny with highs"
func diffText(prevText, currentText string) string {
	if prevText == "" {
		return currentText
	}
	if currentText == "" {
		return ""
	}

	prevWords := strings.Fields(prevText)
	currWords := strings.Fields(currentText)

	if len(prevWords) == 0 {
		return currentText
	}
	if len(currWords) == 0 {
		return ""
	}

	// Find the longest suffix of prevWords that matches a prefix of currWords.
	// We try decreasing suffix lengths -- first match wins (longest overlap).
	bestOverlap := 0
	maxCheck := len(prevWords)
	if len(currWords) < maxCheck {
		maxCheck = len(currWords)
	}

	for suffixLen := maxCheck; suffixLen > 0; suffixLen-- {
		// Compare prevWords[len(prevWords)-suffixLen:] with currWords[:suffixLen]
		match := true
		for i := 0; i < suffixLen; i++ {
			if prevWords[len(prevWords)-suffixLen+i] != currWords[i] {
				match = false
				break
			}
		}
		if match {
			bestOverlap = suffixLen
			break
		}
	}

	if bestOverlap == 0 {
		// No overlap found -- context was lost, return full current text.
		return currentText
	}

	// Return everything after the overlap.
	remaining := currWords[bestOverlap:]
	if len(remaining) == 0 {
		return ""
	}
	return strings.Join(remaining, " ")
}

// stripMusicNotes removes unicode music symbols whisper emits on music audio.
func stripMusicNotes(text string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '♪', '♫', '♩', '♬', '🎵', '🎶':
			return -1
		}
		return r
	}, text)
}

// isHallucination returns true if a segment looks like a whisper hallucination.
// Combines text-based checks with token probability analysis.
func isHallucination(text string, ctx *C.struct_whisper_context, segIdx int) bool {
	if isHallucinationText(text) {
		return true
	}

	// Check average token probability -- low confidence segments are likely hallucinations.
	nTokens := int(C.whisper_full_n_tokens(ctx, C.int(segIdx)))
	if nTokens > 0 {
		var totalP float32
		for j := 0; j < nTokens; j++ {
			totalP += float32(C.whisper_full_get_token_p(ctx, C.int(segIdx), C.int(j)))
		}
		avgP := totalP / float32(nTokens)
		// WHY 0.4: Empirically, real speech averages 0.6-0.9 probability.
		// Hallucinations on silence/music tend to be 0.1-0.3. 0.4 is a
		// conservative threshold that catches most hallucinations without
		// rejecting real but uncertain speech.
		if avgP < 0.4 {
			return true
		}
	}

	return false
}

// isHallucinationText checks text content only, without needing a whisper context.
// Separated from isHallucination so it can be unit tested without a model.
func isHallucinationText(text string) bool {
	lower := strings.ToLower(text)

	// Known hallucination markers.
	hallucinations := []string{
		"[blank_audio]",
		"[music]",
		"[silence]",
		"[applause]",
		"[laughter]",
		"(music)",
		"(silence)",
		"[no speech detected]",
		"thank you.",
		"thanks for watching.",
		"thanks for watching!",
		"thank you for watching.",
		"thank you for watching!",
		"subscribe",
		"please subscribe",
	}
	for _, h := range hallucinations {
		if lower == h {
			return true
		}
	}

	// Bracketed/parenthesized non-speech markers like [Music], (Music playing), etc.
	trimmed := strings.TrimSpace(lower)
	if (strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]")) ||
		(strings.HasPrefix(trimmed, "(") && strings.HasSuffix(trimmed, ")")) {
		return true
	}

	// Very short text (1-2 chars) is almost always hallucination.
	if len(strings.TrimSpace(text)) <= 2 {
		return true
	}

	// Repeated short phrases: "you you you" or "the the the".
	// Split into words, check if all words are the same.
	words := strings.Fields(lower)
	if len(words) >= 3 {
		allSame := true
		for _, w := range words[1:] {
			if w != words[0] {
				allSame = false
				break
			}
		}
		if allSame {
			return true
		}
	}

	return false
}
