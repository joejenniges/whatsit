package classifier

import (
	"math"
	"testing"
)

func TestComputeFrameOutputLength(t *testing.T) {
	cfg := DefaultMFCCConfig()
	ext := NewMFCCExtractor(cfg)

	// A frame of 400 samples (25ms at 16kHz).
	frame := make([]float64, 400)
	for i := range frame {
		frame[i] = math.Sin(2 * math.Pi * 440 * float64(i) / 16000)
	}

	coeffs := ext.ComputeFrame(frame)
	if len(coeffs) != cfg.NumCoeffs {
		t.Errorf("expected %d coefficients, got %d", cfg.NumCoeffs, len(coeffs))
	}
}

func TestComputeFrameNotAllZero(t *testing.T) {
	cfg := DefaultMFCCConfig()
	ext := NewMFCCExtractor(cfg)

	// Non-trivial signal should produce non-zero MFCCs.
	frame := make([]float64, 400)
	for i := range frame {
		frame[i] = 0.5 * math.Sin(2*math.Pi*300*float64(i)/16000)
	}

	coeffs := ext.ComputeFrame(frame)
	allZero := true
	for _, c := range coeffs {
		if math.Abs(c) > 1e-10 {
			allZero = false
			break
		}
	}
	if allZero {
		t.Error("expected non-zero MFCCs for a tonal signal")
	}
}

func TestComputeChunkFrameCount(t *testing.T) {
	cfg := DefaultMFCCConfig()
	ext := NewMFCCExtractor(cfg)

	// 2 seconds at 16kHz = 32000 samples.
	// frameLen = 25ms * 16000 / 1000 = 400
	// hopLen = 10ms * 16000 / 1000 = 160
	// numFrames = (32000 - 400) / 160 + 1 = 198
	samples := make([]float32, 32000)
	for i := range samples {
		samples[i] = 0.3 * float32(math.Sin(2*math.Pi*440*float64(i)/16000))
	}

	mfccs := ext.ComputeChunk(samples)
	expected := 198
	if len(mfccs) != expected {
		t.Errorf("expected %d frames, got %d", expected, len(mfccs))
	}

	// Each frame should have NumCoeffs coefficients.
	for i, frame := range mfccs {
		if len(frame) != cfg.NumCoeffs {
			t.Errorf("frame %d: expected %d coefficients, got %d", i, cfg.NumCoeffs, len(frame))
		}
	}
}

func TestComputeChunkTooShort(t *testing.T) {
	cfg := DefaultMFCCConfig()
	ext := NewMFCCExtractor(cfg)

	// Fewer samples than one frame should return nil.
	samples := make([]float32, 100)
	mfccs := ext.ComputeChunk(samples)
	if mfccs != nil {
		t.Errorf("expected nil for too-short input, got %d frames", len(mfccs))
	}
}

func TestMFCCVariance(t *testing.T) {
	// 3 frames, 2 coefficients each.
	mfccs := [][]float64{
		{1.0, 10.0},
		{2.0, 20.0},
		{3.0, 30.0},
	}

	variances := MFCCVariance(mfccs)
	if len(variances) != 2 {
		t.Fatalf("expected 2 variances, got %d", len(variances))
	}

	// Coeff 0: mean=2, var = ((1-2)^2 + (2-2)^2 + (3-2)^2) / 3 = 2/3
	expectedVar0 := 2.0 / 3.0
	if math.Abs(variances[0]-expectedVar0) > 1e-10 {
		t.Errorf("variance[0] = %f, expected %f", variances[0], expectedVar0)
	}

	// Coeff 1: mean=20, var = ((10-20)^2 + (20-20)^2 + (30-20)^2) / 3 = 200/3
	expectedVar1 := 200.0 / 3.0
	if math.Abs(variances[1]-expectedVar1) > 1e-10 {
		t.Errorf("variance[1] = %f, expected %f", variances[1], expectedVar1)
	}
}

func TestMFCCVarianceEmpty(t *testing.T) {
	result := MFCCVariance(nil)
	if result != nil {
		t.Errorf("expected nil for empty input, got %v", result)
	}
}

func TestDeltaMFCC(t *testing.T) {
	mfccs := [][]float64{
		{1.0, 5.0},
		{3.0, 2.0},
		{6.0, 1.0},
	}

	deltas := DeltaMFCC(mfccs)
	if len(deltas) != 2 {
		t.Fatalf("expected 2 delta frames, got %d", len(deltas))
	}

	// delta[0] = mfcc[1] - mfcc[0] = {2, -3}
	if deltas[0][0] != 2.0 || deltas[0][1] != -3.0 {
		t.Errorf("delta[0] = %v, expected [2, -3]", deltas[0])
	}

	// delta[1] = mfcc[2] - mfcc[1] = {3, -1}
	if deltas[1][0] != 3.0 || deltas[1][1] != -1.0 {
		t.Errorf("delta[1] = %v, expected [3, -1]", deltas[1])
	}
}

func TestDeltaMFCCTooFew(t *testing.T) {
	// Fewer than 2 frames -> nil.
	result := DeltaMFCC([][]float64{{1.0, 2.0}})
	if result != nil {
		t.Errorf("expected nil for single frame, got %v", result)
	}
}
