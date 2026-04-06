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

func TestSpectralFlatness_PureTone(t *testing.T) {
	// A pure sine wave has energy concentrated in one bin -- flatness
	// should be low (close to 0).
	sampleRate := 16000
	freq := 1000.0
	n := sampleRate // 1 second

	samples := make([]float32, n)
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * freq * float64(i) / float64(sampleRate)))
	}

	flatness := SpectralFlatness(samples)
	if flatness > 0.15 {
		t.Errorf("expected low flatness for pure tone, got %f", flatness)
	}
}

func TestSpectralFlatness_WhiteNoise(t *testing.T) {
	// White noise has roughly equal energy across all bins -- flatness
	// should be high (close to 1.0).
	//
	// WHY deterministic PRNG: using a simple LCG instead of math/rand
	// to keep the test fully reproducible without seed management.
	n := 16000
	samples := make([]float32, n)
	state := uint32(42)
	for i := range samples {
		state = state*1664525 + 1013904223
		// Map to [-1, 1].
		samples[i] = float32(state)/float32(math.MaxUint32)*2.0 - 1.0
	}

	flatness := SpectralFlatness(samples)
	if flatness < 0.5 {
		t.Errorf("expected high flatness for white noise, got %f", flatness)
	}
}

func TestSpectralFlatness_Silence(t *testing.T) {
	samples := make([]float32, 256)
	flatness := SpectralFlatness(samples)
	// Should be 0 or very small, not NaN or Inf.
	if math.IsNaN(flatness) || math.IsInf(flatness, 0) {
		t.Errorf("expected finite flatness for silence, got %f", flatness)
	}
}

func TestSpectralRolloff_PureTone(t *testing.T) {
	// Pure 1000 Hz tone: 85% rolloff should be near 1000 Hz.
	sampleRate := 16000
	freq := 1000.0
	n := sampleRate

	f64 := make([]float64, n)
	for i := range f64 {
		f64[i] = math.Sin(2 * math.Pi * freq * float64(i) / float64(sampleRate))
	}

	ps := PowerSpectrum(f64)
	rolloff := SpectralRolloff(ps, sampleRate, 0.85)

	// Should be near 1000 Hz. Allow generous tolerance for windowing effects.
	if math.Abs(rolloff-freq) > 200 {
		t.Errorf("expected rolloff near %f Hz, got %f Hz", freq, rolloff)
	}
}

func TestSpectralRolloff_Empty(t *testing.T) {
	rolloff := SpectralRolloff(nil, 16000, 0.85)
	if rolloff != 0 {
		t.Errorf("expected 0 for empty input, got %f", rolloff)
	}
}

func TestPowerSpectrum_Length(t *testing.T) {
	// 1024 samples -> next power of 2 is 1024 -> 513 bins.
	samples := make([]float64, 1024)
	for i := range samples {
		samples[i] = float64(i) / 1024.0
	}
	ps := PowerSpectrum(samples)
	expected := 513
	if len(ps) != expected {
		t.Errorf("expected %d bins, got %d", expected, len(ps))
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
