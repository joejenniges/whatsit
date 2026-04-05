package classifier

import (
	"math"
	"testing"
)

func TestNextPow2(t *testing.T) {
	tests := []struct{ in, want int }{
		{1, 1},
		{2, 2},
		{3, 4},
		{5, 8},
		{1023, 1024},
		{1024, 1024},
		{1025, 2048},
	}
	for _, tt := range tests {
		got := nextPow2(tt.in)
		if got != tt.want {
			t.Errorf("nextPow2(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestMagnitudeSpectrum_DC(t *testing.T) {
	// Constant signal with Hann window applied: the DC bin should contain
	// the bulk of the energy. With Hann windowing a constant signal, DC
	// magnitude = sum of window coefficients = N/2 for large N.
	n := 64
	samples := make([]float32, n)
	for i := range samples {
		samples[i] = 1.0
	}

	mag := MagnitudeSpectrum(samples)

	// DC bin should be the dominant bin.
	for i := 2; i < len(mag); i++ {
		if mag[i] > mag[0] {
			t.Errorf("bin %d (%f) exceeds DC bin (%f)", i, mag[i], mag[0])
		}
	}

	// DC magnitude with Hann window on constant signal ≈ N/2 = 32.
	// Allow generous tolerance since it's a short window.
	expectedDC := float64(n) / 2.0
	if math.Abs(mag[0]-expectedDC) > 2.0 {
		t.Errorf("expected DC bin near %f, got %f", expectedDC, mag[0])
	}
}

func TestSpectralCentroid_PureTone(t *testing.T) {
	// Generate a pure sine wave at 1000 Hz, sample rate 16000.
	// The spectral centroid should be close to 1000 Hz.
	sampleRate := 16000
	duration := 1.0 // 1 second
	freq := 1000.0
	n := int(duration * float64(sampleRate))

	samples := make([]float32, n)
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / float64(sampleRate)))
	}

	centroid := SpectralCentroid(samples, sampleRate)

	// Allow some tolerance -- windowing effects and spectral leakage
	// shift the centroid slightly.
	tolerance := 50.0 // Hz
	if math.Abs(centroid-freq) > tolerance {
		t.Errorf("expected centroid near %f Hz, got %f Hz", freq, centroid)
	}
}

func TestSpectralCentroid_Silence(t *testing.T) {
	samples := make([]float32, 256)
	centroid := SpectralCentroid(samples, 16000)
	if centroid != 0 {
		t.Errorf("expected centroid 0 for silence, got %f", centroid)
	}
}

func TestSpectralFlux_Identical(t *testing.T) {
	a := []float64{1, 2, 3, 4, 5}
	flux := SpectralFlux(a, a)
	if flux != 0 {
		t.Errorf("expected 0 flux for identical spectra, got %f", flux)
	}
}

func TestSpectralFlux_Increase(t *testing.T) {
	prev := []float64{0, 0, 0}
	curr := []float64{1, 2, 3}
	// Half-wave rectified: all diffs are positive.
	// flux = 1 + 4 + 9 = 14
	flux := SpectralFlux(curr, prev)
	if math.Abs(flux-14.0) > 1e-9 {
		t.Errorf("expected flux 14, got %f", flux)
	}
}

func TestSpectralFlux_Decrease(t *testing.T) {
	prev := []float64{5, 5, 5}
	curr := []float64{1, 1, 1}
	// All differences are negative -- half-wave rectified to 0.
	flux := SpectralFlux(curr, prev)
	if flux != 0 {
		t.Errorf("expected 0 flux for decreasing spectrum, got %f", flux)
	}
}

func TestSpectralFlux_DifferentLengths(t *testing.T) {
	prev := []float64{0, 0}
	curr := []float64{3, 4, 5}
	// Should use the shorter length (2 bins).
	// flux = 9 + 16 = 25
	flux := SpectralFlux(curr, prev)
	if math.Abs(flux-25.0) > 1e-9 {
		t.Errorf("expected flux 25, got %f", flux)
	}
}

func TestSpectralFlux_Empty(t *testing.T) {
	flux := SpectralFlux(nil, nil)
	if flux != 0 {
		t.Errorf("expected 0 for empty input, got %f", flux)
	}
}
