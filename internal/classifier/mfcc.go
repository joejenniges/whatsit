package classifier

import (
	"math"
	"math/cmplx"
)

// MFCCConfig holds parameters for MFCC extraction.
type MFCCConfig struct {
	SampleRate int     // audio sample rate in Hz
	FrameLenMs float64 // analysis frame length in milliseconds
	FrameHopMs float64 // hop between frames in milliseconds
	FFTSize    int     // FFT length (must be power of 2)
	NumFilters int     // number of Mel filterbank channels
	NumCoeffs  int     // number of cepstral coefficients to keep
}

// DefaultMFCCConfig returns standard MFCC parameters for 16 kHz audio.
func DefaultMFCCConfig() MFCCConfig {
	return MFCCConfig{
		SampleRate: 16000,
		FrameLenMs: 25.0,
		FrameHopMs: 10.0,
		FFTSize:    512,
		NumFilters: 26,
		NumCoeffs:  13,
	}
}

// MFCCExtractor computes MFCCs from raw audio samples.
type MFCCExtractor struct {
	config     MFCCConfig
	filterbank *MelFilterbank
	dctMatrix  [][]float64 // precomputed DCT-II matrix [NumCoeffs][NumFilters]
}

// NewMFCCExtractor creates an extractor with a precomputed filterbank and DCT
// matrix for the given config.
func NewMFCCExtractor(config MFCCConfig) *MFCCExtractor {
	e := &MFCCExtractor{
		config:     config,
		filterbank: NewMelFilterbank(config.NumFilters, config.FFTSize, config.SampleRate),
	}

	// Precompute DCT-II matrix: dct[i][j] = cos(pi * i * (j + 0.5) / N)
	e.dctMatrix = make([][]float64, config.NumCoeffs)
	for i := 0; i < config.NumCoeffs; i++ {
		e.dctMatrix[i] = make([]float64, config.NumFilters)
		for j := 0; j < config.NumFilters; j++ {
			e.dctMatrix[i][j] = math.Cos(math.Pi * float64(i) * (float64(j) + 0.5) / float64(config.NumFilters))
		}
	}

	return e
}

// ComputeFrame extracts MFCCs from a single frame of float64 samples.
//
// Pipeline: Hann window -> zero-pad to FFTSize -> FFT -> power spectrum ->
// Mel filterbank -> log -> DCT-II -> first NumCoeffs coefficients.
func (e *MFCCExtractor) ComputeFrame(frame []float64) []float64 {
	n := e.config.FFTSize

	// Apply Hann window and pack into complex buffer, zero-padded.
	buf := make([]complex128, n)
	frameLen := len(frame)
	for i := 0; i < frameLen && i < n; i++ {
		w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(frameLen-1)))
		buf[i] = complex(frame[i]*w, 0)
	}

	fft(buf)

	// Power spectrum: |X[k]|^2, only positive frequencies.
	bins := n/2 + 1
	power := make([]float64, bins)
	for i := 0; i < bins; i++ {
		mag := cmplx.Abs(buf[i])
		power[i] = mag * mag
	}

	// Mel filterbank energies.
	melEnergies := e.filterbank.Apply(power)

	// Log compression. Floor at a small value to avoid log(0).
	for i := range melEnergies {
		if melEnergies[i] < 1e-22 {
			melEnergies[i] = 1e-22
		}
		melEnergies[i] = math.Log(melEnergies[i])
	}

	// DCT-II to get cepstral coefficients.
	coeffs := make([]float64, e.config.NumCoeffs)
	for i := 0; i < e.config.NumCoeffs; i++ {
		var sum float64
		for j := 0; j < e.config.NumFilters; j++ {
			sum += e.dctMatrix[i][j] * melEnergies[j]
		}
		coeffs[i] = sum
	}

	return coeffs
}

// ComputeChunk splits a chunk of audio into overlapping frames and computes
// MFCCs for each frame. Returns [numFrames][NumCoeffs].
//
// For 2 seconds of 16 kHz audio with 25ms frames and 10ms hop, this produces
// approximately 198 frames.
func (e *MFCCExtractor) ComputeChunk(samples []float32) [][]float64 {
	frameLen := int(e.config.FrameLenMs * float64(e.config.SampleRate) / 1000.0)
	hopLen := int(e.config.FrameHopMs * float64(e.config.SampleRate) / 1000.0)

	if len(samples) < frameLen {
		return nil
	}

	numFrames := (len(samples) - frameLen) / hopLen + 1
	result := make([][]float64, numFrames)

	for i := 0; i < numFrames; i++ {
		start := i * hopLen
		end := start + frameLen

		// Convert float32 frame to float64 for processing.
		frame := make([]float64, frameLen)
		for j := 0; j < frameLen; j++ {
			frame[j] = float64(samples[start+j])
		}
		_ = end // clarity: we index manually above

		result[i] = e.ComputeFrame(frame)
	}

	return result
}
