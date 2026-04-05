package classifier

import (
	"math"
	"testing"
)

func TestZeroCrossingRate_MaxCrossings(t *testing.T) {
	// Alternating +1 / -1 crosses zero every sample pair.
	samples := make([]float32, 100)
	for i := range samples {
		if i%2 == 0 {
			samples[i] = 1.0
		} else {
			samples[i] = -1.0
		}
	}

	zcr := ZeroCrossingRate(samples)
	if math.Abs(zcr-1.0) > 1e-9 {
		t.Errorf("expected ZCR ~1.0 for alternating signal, got %f", zcr)
	}
}

func TestZeroCrossingRate_NoCrossings(t *testing.T) {
	// All positive -- zero crossings.
	samples := make([]float32, 100)
	for i := range samples {
		samples[i] = float32(i + 1)
	}

	zcr := ZeroCrossingRate(samples)
	if zcr != 0 {
		t.Errorf("expected ZCR 0 for monotone positive signal, got %f", zcr)
	}
}

func TestZeroCrossingRate_Empty(t *testing.T) {
	if zcr := ZeroCrossingRate(nil); zcr != 0 {
		t.Errorf("expected 0 for nil input, got %f", zcr)
	}
	if zcr := ZeroCrossingRate([]float32{1.0}); zcr != 0 {
		t.Errorf("expected 0 for single sample, got %f", zcr)
	}
}

func TestZeroCrossingRate_SingleCrossing(t *testing.T) {
	// [1, 1, 1, -1, -1, -1] -> 1 crossing out of 5 pairs = 0.2
	samples := []float32{1, 1, 1, -1, -1, -1}
	zcr := ZeroCrossingRate(samples)
	if math.Abs(zcr-0.2) > 1e-9 {
		t.Errorf("expected ZCR 0.2, got %f", zcr)
	}
}
