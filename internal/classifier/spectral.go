package classifier

import (
	"math"
	"math/cmplx"
)

// nextPow2 returns the smallest power of 2 >= n.
func nextPow2(n int) int {
	p := 1
	for p < n {
		p <<= 1
	}
	return p
}

// fft computes the radix-2 Cooley-Tukey FFT in-place.
// len(x) must be a power of 2.
func fft(x []complex128) {
	n := len(x)
	if n <= 1 {
		return
	}

	// Bit-reversal permutation
	j := 0
	for i := 1; i < n; i++ {
		bit := n >> 1
		for j&bit != 0 {
			j ^= bit
			bit >>= 1
		}
		j ^= bit
		if i < j {
			x[i], x[j] = x[j], x[i]
		}
	}

	// Butterfly stages
	for size := 2; size <= n; size <<= 1 {
		half := size >> 1
		// WHY: precompute the twiddle factor step per stage rather than per
		// butterfly -- saves a multiply inside the inner loop.
		wStep := -2.0 * math.Pi / float64(size)
		for start := 0; start < n; start += size {
			w := complex(1, 0)
			wn := cmplx.Exp(complex(0, wStep))
			for k := 0; k < half; k++ {
				u := x[start+k]
				t := w * x[start+k+half]
				x[start+k] = u + t
				x[start+k+half] = u - t
				w *= wn
			}
		}
	}
}

// MagnitudeSpectrum computes the magnitude spectrum of the input samples
// using a radix-2 FFT. A Hann window is applied before the transform to
// reduce spectral leakage. The input is zero-padded to the next power of 2.
// Returns only the first N/2+1 bins (positive frequencies).
func MagnitudeSpectrum(samples []float32) []float64 {
	n := nextPow2(len(samples))
	buf := make([]complex128, n)
	// WHY: Hann window reduces spectral leakage so that SpectralCentroid
	// on a pure tone actually lands near the tone frequency rather than
	// being smeared across bins.
	nSamples := len(samples)
	for i, s := range samples {
		w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(nSamples-1)))
		buf[i] = complex(float64(s)*w, 0)
	}

	fft(buf)

	// Only positive frequencies
	bins := n/2 + 1
	mag := make([]float64, bins)
	for i := 0; i < bins; i++ {
		mag[i] = cmplx.Abs(buf[i])
	}
	return mag
}

// SpectralCentroid computes the weighted mean frequency of the magnitude
// spectrum. The result is in Hz.
//
// Typical values:
//   - Speech: 500 - 2000 Hz
//   - Music:  wider spread, often higher centroid
func SpectralCentroid(samples []float32, sampleRate int) float64 {
	mag := MagnitudeSpectrum(samples)
	n := (len(mag) - 1) * 2 // FFT size

	var weightedSum, totalMag float64
	freqPerBin := float64(sampleRate) / float64(n)

	for i, m := range mag {
		freq := float64(i) * freqPerBin
		weightedSum += freq * m
		totalMag += m
	}

	if totalMag == 0 {
		return 0
	}
	return weightedSum / totalMag
}

// SpectralFlux computes the sum of squared positive differences between
// the current and previous magnitude spectra. Only increases in energy
// are counted (half-wave rectified), which better captures onsets.
//
// If the spectra have different lengths, the shorter one is used.
//
// Typical behaviour:
//   - Speech: high flux (rapidly changing spectral content)
//   - Music:  lower flux (more stable tonal content)
func SpectralFlux(current, previous []float64) float64 {
	n := len(current)
	if len(previous) < n {
		n = len(previous)
	}
	if n == 0 {
		return 0
	}

	var flux float64
	for i := 0; i < n; i++ {
		diff := current[i] - previous[i]
		if diff > 0 {
			flux += diff * diff
		}
	}
	return flux
}
