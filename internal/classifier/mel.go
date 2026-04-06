package classifier

import "math"

// MelFilterbank maps FFT power spectrum bins to Mel-scale frequency bands
// using overlapping triangular filters. This is the standard front-end for
// MFCC extraction.
type MelFilterbank struct {
	NumFilters int         // number of Mel bands (typically 26)
	FFTSize    int         // FFT length (e.g. 512)
	SampleRate int         // audio sample rate in Hz
	Filters    [][]float64 // [NumFilters][FFTSize/2+1] triangular filter weights
}

// HzToMel converts a frequency in Hz to the Mel scale.
// Uses the O'Shaughnessy formula: 2595 * log10(1 + hz/700).
func HzToMel(hz float64) float64 {
	return 2595.0 * math.Log10(1.0+hz/700.0)
}

// MelToHz converts a Mel-scale value back to Hz.
func MelToHz(mel float64) float64 {
	return 700.0 * (math.Pow(10.0, mel/2595.0) - 1.0)
}

// NewMelFilterbank builds a bank of triangular filters spaced evenly on the
// Mel scale. Each filter spans three adjacent Mel-spaced points and has unit
// peak height.
func NewMelFilterbank(numFilters, fftSize, sampleRate int) *MelFilterbank {
	fb := &MelFilterbank{
		NumFilters: numFilters,
		FFTSize:    fftSize,
		SampleRate: sampleRate,
	}

	bins := fftSize/2 + 1
	melMin := HzToMel(0)
	melMax := HzToMel(float64(sampleRate) / 2.0)

	// numFilters+2 equally spaced points on the Mel scale.
	// The extra 2 are the left edge of the first filter and the right edge
	// of the last filter.
	nPoints := numFilters + 2
	melPoints := make([]float64, nPoints)
	for i := 0; i < nPoints; i++ {
		melPoints[i] = melMin + float64(i)*(melMax-melMin)/float64(nPoints-1)
	}

	// Convert Mel points to FFT bin indices.
	binIndices := make([]int, nPoints)
	for i, m := range melPoints {
		hz := MelToHz(m)
		binIndices[i] = int(math.Floor((float64(fftSize) + 1.0) * hz / float64(sampleRate)))
	}

	// Build triangular filters.
	fb.Filters = make([][]float64, numFilters)
	for i := 0; i < numFilters; i++ {
		fb.Filters[i] = make([]float64, bins)
		left := binIndices[i]
		center := binIndices[i+1]
		right := binIndices[i+2]

		// Rising slope: left -> center
		for j := left; j <= center && j < bins; j++ {
			if j < 0 {
				continue
			}
			if center != left {
				fb.Filters[i][j] = float64(j-left) / float64(center-left)
			}
		}
		// Falling slope: center -> right
		for j := center; j <= right && j < bins; j++ {
			if j < 0 {
				continue
			}
			if right != center {
				fb.Filters[i][j] = float64(right-j) / float64(right-center)
			}
		}
	}

	return fb
}

// Apply multiplies the power spectrum by each filter and sums the result,
// producing one energy value per Mel band.
func (fb *MelFilterbank) Apply(powerSpectrum []float64) []float64 {
	result := make([]float64, fb.NumFilters)
	bins := fb.FFTSize/2 + 1

	n := len(powerSpectrum)
	if n > bins {
		n = bins
	}

	for i := 0; i < fb.NumFilters; i++ {
		var sum float64
		for j := 0; j < n; j++ {
			sum += fb.Filters[i][j] * powerSpectrum[j]
		}
		result[i] = sum
	}
	return result
}
