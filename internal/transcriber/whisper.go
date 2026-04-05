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
	ModelPath string
	Language  string // e.g. "en", "auto", or "" (defaults to "en")
	Threads   int    // 0 means runtime.NumCPU()
}

// Transcriber wraps a whisper.cpp context for speech-to-text.
// It is NOT safe for concurrent use -- access is serialized internally with a mutex.
type Transcriber struct {
	ctx    *C.struct_whisper_context
	config TranscriberConfig
	mu     sync.Mutex
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

// Close releases the whisper context. Safe to call multiple times.
func (t *Transcriber) Close() {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.ctx != nil {
		C.whisper_free(t.ctx)
		t.ctx = nil
	}
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
