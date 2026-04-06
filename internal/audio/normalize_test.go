package audio

import (
	"math"
	"testing"
)

func rms(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}
	var sumSq float64
	for _, s := range samples {
		sumSq += float64(s) * float64(s)
	}
	return math.Sqrt(sumSq / float64(len(samples)))
}

func TestNormalizeRMS_QuietSignalAmplified(t *testing.T) {
	// Quiet signal: RMS ~0.01
	samples := make([]float32, 1000)
	for i := range samples {
		samples[i] = 0.01 * float32(math.Sin(2*math.Pi*float64(i)/100))
	}
	before := rms(samples)
	target := float32(0.1)
	out := NormalizeRMS(samples, target)

	after := rms(out)
	if after <= before {
		t.Errorf("expected amplification: before=%.6f after=%.6f", before, after)
	}
	if math.Abs(after-float64(target)) > 0.005 {
		t.Errorf("output RMS %.6f should be close to target %.3f", after, target)
	}
}

func TestNormalizeRMS_LoudSignalAttenuated(t *testing.T) {
	// Loud signal: RMS ~0.5
	samples := make([]float32, 1000)
	for i := range samples {
		samples[i] = 0.5 * float32(math.Sin(2*math.Pi*float64(i)/100))
	}
	before := rms(samples)
	target := float32(0.1)
	out := NormalizeRMS(samples, target)

	after := rms(out)
	if after >= before {
		t.Errorf("expected attenuation: before=%.6f after=%.6f", before, after)
	}
	if math.Abs(after-float64(target)) > 0.005 {
		t.Errorf("output RMS %.6f should be close to target %.3f", after, target)
	}
}

func TestNormalizeRMS_SilenceUnchanged(t *testing.T) {
	// Near-silent signal: RMS well below epsilon (0.0001)
	samples := make([]float32, 1000)
	for i := range samples {
		samples[i] = 0.00001 * float32(math.Sin(2*math.Pi*float64(i)/100))
	}
	out := NormalizeRMS(samples, 0.1)

	// Should return input unchanged (same pointer is fine, same values required).
	for i := range samples {
		if out[i] != samples[i] {
			t.Fatalf("silence: sample %d changed from %v to %v", i, samples[i], out[i])
		}
	}
}

func TestNormalizeRMS_ClampsToUnitRange(t *testing.T) {
	// Signal with peaks near 1.0 and a high target that would push past 1.0.
	samples := []float32{0.9, -0.9, 0.9, -0.9, 0.9, -0.9}
	// RMS of this is ~0.9. Target 0.95 means scale ~1.056, pushing 0.9 to ~0.95.
	// But let's use a target that definitely clips.
	out := NormalizeRMS(samples, 1.0)

	for i, v := range out {
		if v > 1.0 || v < -1.0 {
			t.Errorf("sample %d out of range: %v", i, v)
		}
	}
}

func TestNormalizeRMS_OutputMatchesTarget(t *testing.T) {
	// Signal at known RMS, check output RMS matches target within tolerance.
	// Use a pure sine (no clipping risk at target 0.1).
	samples := make([]float32, 4000)
	for i := range samples {
		samples[i] = 0.3 * float32(math.Sin(2*math.Pi*float64(i)/200))
	}

	target := float32(0.1)
	out := NormalizeRMS(samples, target)
	outRMS := rms(out)

	if math.Abs(outRMS-float64(target)) > 0.002 {
		t.Errorf("output RMS %.6f not close enough to target %.3f", outRMS, target)
	}
}

func TestNormalizeRMS_DoesNotModifyInput(t *testing.T) {
	samples := []float32{0.5, -0.5, 0.3, -0.3}
	orig := make([]float32, len(samples))
	copy(orig, samples)

	_ = NormalizeRMS(samples, 0.1)

	for i := range samples {
		if samples[i] != orig[i] {
			t.Errorf("input modified at index %d: %v -> %v", i, orig[i], samples[i])
		}
	}
}

func TestNormalizeRMS_EmptySlice(t *testing.T) {
	out := NormalizeRMS(nil, 0.1)
	if out != nil {
		t.Errorf("expected nil for nil input, got len %d", len(out))
	}

	out = NormalizeRMS([]float32{}, 0.1)
	if len(out) != 0 {
		t.Errorf("expected empty for empty input, got len %d", len(out))
	}
}
