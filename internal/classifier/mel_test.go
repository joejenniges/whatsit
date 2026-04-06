package classifier

import (
	"math"
	"testing"
)

func TestHzToMelRoundTrip(t *testing.T) {
	// Converting Hz -> Mel -> Hz should return the original value.
	freqs := []float64{0, 100, 500, 1000, 4000, 8000}
	for _, hz := range freqs {
		mel := HzToMel(hz)
		got := MelToHz(mel)
		if math.Abs(got-hz) > 1e-6 {
			t.Errorf("round-trip failed for %.1f Hz: got %.6f", hz, got)
		}
	}
}

func TestHzToMelKnownValues(t *testing.T) {
	// 1000 Hz should be approximately 1000 Mel (the scales are designed to
	// roughly align there). Check within a reasonable tolerance.
	mel1000 := HzToMel(1000)
	if math.Abs(mel1000-1000) > 200 {
		t.Errorf("HzToMel(1000) = %.1f, expected ~1000", mel1000)
	}

	// 0 Hz should be 0 Mel.
	if HzToMel(0) != 0 {
		t.Errorf("HzToMel(0) = %f, expected 0", HzToMel(0))
	}
}

func TestMelFilterbankTriangularShape(t *testing.T) {
	fb := NewMelFilterbank(26, 512, 16000)

	if len(fb.Filters) != 26 {
		t.Fatalf("expected 26 filters, got %d", len(fb.Filters))
	}

	bins := 512/2 + 1
	for i, f := range fb.Filters {
		if len(f) != bins {
			t.Errorf("filter %d: expected %d bins, got %d", i, bins, len(f))
			continue
		}

		// Each filter should have a single peak (value 1.0 at center) and
		// all values should be in [0, 1].
		maxVal := 0.0
		for _, v := range f {
			if v < -1e-10 {
				t.Errorf("filter %d: negative weight %f", i, v)
			}
			if v > maxVal {
				maxVal = v
			}
		}
		// Peak should be close to 1.0 (it equals 1.0 when center bin is exact).
		if maxVal < 0.5 {
			t.Errorf("filter %d: peak too low: %f", i, maxVal)
		}
	}
}

func TestMelFilterbankApply(t *testing.T) {
	fb := NewMelFilterbank(26, 512, 16000)
	bins := 512/2 + 1

	// Flat power spectrum: all bins = 1.0.
	flat := make([]float64, bins)
	for i := range flat {
		flat[i] = 1.0
	}

	result := fb.Apply(flat)
	if len(result) != 26 {
		t.Fatalf("expected 26 energies, got %d", len(result))
	}

	// Each filter applied to a flat spectrum should give a positive value
	// (the sum of the triangular weights).
	for i, v := range result {
		if v <= 0 {
			t.Errorf("filter %d: energy %.6f on flat spectrum, expected > 0", i, v)
		}
	}

	// Zero spectrum should give zero energies.
	zeros := make([]float64, bins)
	result = fb.Apply(zeros)
	for i, v := range result {
		if v != 0 {
			t.Errorf("filter %d: energy %f on zero spectrum, expected 0", i, v)
		}
	}
}
