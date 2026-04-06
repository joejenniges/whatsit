package musicid

// WHY CGo chromaprint: We need Chromaprint-compatible fingerprints for AcoustID
// lookups. The MSYS2 ucrt64 package provides libchromaprint with pkg-config,
// same pattern as our whisper.cpp bindings. Pure Go alternatives don't exist
// for Chromaprint's specific algorithm.

// #cgo pkg-config: libchromaprint
// #include <chromaprint.h>
// #include <stdlib.h>
import "C"

import (
	"fmt"
	"math"
	"unsafe"
)

// Fingerprinter generates Chromaprint-compatible audio fingerprints from PCM data.
// The fingerprints can be submitted to AcoustID for song identification.
type Fingerprinter struct {
	ctx *C.ChromaprintContext
}

// NewFingerprinter creates a new Chromaprint fingerprinter using the default algorithm.
func NewFingerprinter() (*Fingerprinter, error) {
	ctx := C.chromaprint_new(C.CHROMAPRINT_ALGORITHM_DEFAULT)
	if ctx == nil {
		return nil, fmt.Errorf("chromaprint: failed to create context")
	}
	return &Fingerprinter{ctx: ctx}, nil
}

// Fingerprint generates a base64-encoded Chromaprint fingerprint from float32 PCM samples.
// The samples should be mono (single channel). Returns the fingerprint string and the
// duration in seconds.
//
// WHY float32-to-int16 conversion here: Our audio pipeline produces float32 samples
// (resampler output), but Chromaprint's C API expects int16_t PCM. We convert inline
// rather than requiring callers to manage format differences.
func (f *Fingerprinter) Fingerprint(samples []float32, sampleRate int) (string, int, error) {
	if f.ctx == nil {
		return "", 0, fmt.Errorf("chromaprint: fingerprinter is closed")
	}
	if len(samples) == 0 {
		return "", 0, fmt.Errorf("chromaprint: no samples provided")
	}
	if sampleRate <= 0 {
		return "", 0, fmt.Errorf("chromaprint: invalid sample rate %d", sampleRate)
	}

	// Convert float32 [-1.0, 1.0] to int16 [-32768, 32767].
	pcm := make([]C.int16_t, len(samples))
	for i, s := range samples {
		// Clamp to [-1.0, 1.0] before scaling to avoid overflow.
		if s > 1.0 {
			s = 1.0
		} else if s < -1.0 {
			s = -1.0
		}
		pcm[i] = C.int16_t(s * math.MaxInt16)
	}

	numChannels := 1 // Our pipeline always produces mono.

	if rc := C.chromaprint_start(f.ctx, C.int(sampleRate), C.int(numChannels)); rc == 0 {
		return "", 0, fmt.Errorf("chromaprint: start failed")
	}

	if rc := C.chromaprint_feed(f.ctx, &pcm[0], C.int(len(pcm))); rc == 0 {
		return "", 0, fmt.Errorf("chromaprint: feed failed")
	}

	if rc := C.chromaprint_finish(f.ctx); rc == 0 {
		return "", 0, fmt.Errorf("chromaprint: finish failed")
	}

	var cfp *C.char
	if rc := C.chromaprint_get_fingerprint(f.ctx, &cfp); rc == 0 {
		return "", 0, fmt.Errorf("chromaprint: get fingerprint failed")
	}
	defer C.chromaprint_dealloc(unsafe.Pointer(cfp))

	fp := C.GoString(cfp)
	duration := len(samples) / sampleRate

	return fp, duration, nil
}

// FingerprintInt16 generates a fingerprint from int16 PCM samples (the native
// format Chromaprint expects). Supports mono or stereo.
//
// WHY this method: The original Fingerprint() converts float32->int16 and only
// handles mono at 16kHz. But Chromaprint produces much better fingerprints from
// the original pre-resampled audio (e.g., 48kHz stereo). AcoustID was rejecting
// fingerprints from 16kHz audio as "invalid fingerprint".
func (f *Fingerprinter) FingerprintInt16(samples []int16, sampleRate, channels int) (string, int, error) {
	if f.ctx == nil {
		return "", 0, fmt.Errorf("chromaprint: fingerprinter is closed")
	}
	if len(samples) == 0 {
		return "", 0, fmt.Errorf("chromaprint: no samples provided")
	}
	if sampleRate <= 0 {
		return "", 0, fmt.Errorf("chromaprint: invalid sample rate %d", sampleRate)
	}
	if channels <= 0 {
		return "", 0, fmt.Errorf("chromaprint: invalid channel count %d", channels)
	}

	// Cast Go []int16 to C []int16_t (same underlying type).
	pcm := make([]C.int16_t, len(samples))
	for i, s := range samples {
		pcm[i] = C.int16_t(s)
	}

	if rc := C.chromaprint_start(f.ctx, C.int(sampleRate), C.int(channels)); rc == 0 {
		return "", 0, fmt.Errorf("chromaprint: start failed")
	}

	if rc := C.chromaprint_feed(f.ctx, &pcm[0], C.int(len(pcm))); rc == 0 {
		return "", 0, fmt.Errorf("chromaprint: feed failed")
	}

	if rc := C.chromaprint_finish(f.ctx); rc == 0 {
		return "", 0, fmt.Errorf("chromaprint: finish failed")
	}

	var cfp *C.char
	if rc := C.chromaprint_get_fingerprint(f.ctx, &cfp); rc == 0 {
		return "", 0, fmt.Errorf("chromaprint: get fingerprint failed")
	}
	defer C.chromaprint_dealloc(unsafe.Pointer(cfp))

	fp := C.GoString(cfp)
	// Total samples / channels / sampleRate = duration in seconds.
	duration := len(samples) / channels / sampleRate

	return fp, duration, nil
}

// Close frees the underlying Chromaprint context. The Fingerprinter must not be
// used after Close is called.
func (f *Fingerprinter) Close() {
	if f.ctx != nil {
		C.chromaprint_free(f.ctx)
		f.ctx = nil
	}
}
