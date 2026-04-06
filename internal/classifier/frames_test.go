package classifier

import (
	"math"
	"testing"
)

func TestSplitIntoFrames_Count(t *testing.T) {
	// 2 seconds at 16 kHz = 32000 samples.
	// 25 ms frame = 400 samples, 10 ms hop = 160 samples.
	// numFrames = 1 + (32000 - 400) / 160 = 1 + 197 = 198
	samples := make([]float32, 32000)
	for i := range samples {
		samples[i] = 0.5
	}

	params := DefaultFrameParams()
	frames := SplitIntoFrames(samples, params)

	expected := 198
	if len(frames) != expected {
		t.Errorf("expected %d frames, got %d", expected, len(frames))
	}
}

func TestSplitIntoFrames_FrameLength(t *testing.T) {
	samples := make([]float32, 32000)
	for i := range samples {
		samples[i] = 1.0
	}

	params := DefaultFrameParams()
	frames := SplitIntoFrames(samples, params)

	expectedLen := int(math.Ceil(float64(params.SampleRate) * params.FrameLenMs / 1000.0))
	for i, f := range frames {
		if len(f) != expectedLen {
			t.Errorf("frame %d: expected length %d, got %d", i, expectedLen, len(f))
			break
		}
	}
}

func TestSplitIntoFrames_HannWindowApplied(t *testing.T) {
	// Feed a constant signal. After Hann windowing, the first and last
	// samples of each frame should be near zero (Hann window is zero at
	// endpoints).
	samples := make([]float32, 32000)
	for i := range samples {
		samples[i] = 1.0
	}

	params := DefaultFrameParams()
	frames := SplitIntoFrames(samples, params)

	if len(frames) == 0 {
		t.Fatal("expected frames, got none")
	}

	frame := frames[0]

	// First sample: Hann(0) = 0, so windowed value should be ~0.
	if math.Abs(float64(frame[0])) > 1e-6 {
		t.Errorf("expected first sample near 0 (Hann window), got %f", frame[0])
	}

	// Last sample: Hann(N-1) = 0, so windowed value should be ~0.
	last := frame[len(frame)-1]
	if math.Abs(float64(last)) > 1e-6 {
		t.Errorf("expected last sample near 0 (Hann window), got %f", last)
	}

	// Middle sample should be near 1.0 (Hann peaks at ~1.0 in the center).
	mid := frame[len(frame)/2]
	if math.Abs(float64(mid)-1.0) > 0.01 {
		t.Errorf("expected middle sample near 1.0 (Hann peak), got %f", mid)
	}
}

func TestSplitIntoFrames_TooShort(t *testing.T) {
	// Input shorter than one frame should return nil.
	samples := make([]float32, 100) // 100 samples < 400 (25ms at 16kHz)
	params := DefaultFrameParams()
	frames := SplitIntoFrames(samples, params)
	if frames != nil {
		t.Errorf("expected nil for input shorter than one frame, got %d frames", len(frames))
	}
}

func TestSplitIntoFrames_Empty(t *testing.T) {
	params := DefaultFrameParams()
	frames := SplitIntoFrames(nil, params)
	if frames != nil {
		t.Errorf("expected nil for nil input, got %d frames", len(frames))
	}
}
