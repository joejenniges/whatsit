package classifier

import (
	"math"
	"testing"
)

func TestRMSEnergy_ConstantSignal(t *testing.T) {
	// RMS of a constant value k is |k|.
	samples := make([]float32, 200)
	for i := range samples {
		samples[i] = 0.5
	}

	rms := RMSEnergy(samples)
	if math.Abs(rms-0.5) > 1e-6 {
		t.Errorf("expected RMS 0.5 for constant 0.5 signal, got %f", rms)
	}
}

func TestRMSEnergy_Silence(t *testing.T) {
	samples := make([]float32, 100)
	rms := RMSEnergy(samples)
	if rms != 0 {
		t.Errorf("expected RMS 0 for silence, got %f", rms)
	}
}

func TestRMSEnergy_Empty(t *testing.T) {
	if rms := RMSEnergy(nil); rms != 0 {
		t.Errorf("expected 0 for nil input, got %f", rms)
	}
}

func TestRMSEnergy_KnownValue(t *testing.T) {
	// [3, 4] -> sqrt((9+16)/2) = sqrt(12.5) ≈ 3.5355
	samples := []float32{3, 4}
	rms := RMSEnergy(samples)
	expected := math.Sqrt(12.5)
	if math.Abs(rms-expected) > 1e-6 {
		t.Errorf("expected RMS %f, got %f", expected, rms)
	}
}

func TestEnergyVariance_Constant(t *testing.T) {
	// Constant signal -> all frames have the same energy -> variance = 0.
	samples := make([]float32, 200)
	for i := range samples {
		samples[i] = 0.5
	}

	v := EnergyVariance(samples, 50)
	if v > 1e-12 {
		t.Errorf("expected near-zero variance for constant signal, got %e", v)
	}
}

func TestEnergyVariance_Bursty(t *testing.T) {
	// Alternating loud/silent frames should have high variance.
	samples := make([]float32, 400)
	for i := 0; i < 100; i++ {
		samples[i] = 0.8 // loud frame 1
	}
	// 100..199 silent
	for i := 200; i < 300; i++ {
		samples[i] = 0.8 // loud frame 3
	}
	// 300..399 silent

	v := EnergyVariance(samples, 100)
	if v < 0.01 {
		t.Errorf("expected high variance for bursty signal, got %e", v)
	}
}
