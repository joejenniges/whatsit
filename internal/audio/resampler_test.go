package audio

import (
	"context"
	"math"
	"testing"
)

func TestStereoToMonoFloat32(t *testing.T) {
	// Interleaved stereo: [L, R, L, R, ...]
	stereo := []int16{1000, 2000, -1000, -2000, 0, 0, 32767, 32767}
	mono := stereoToMonoFloat32(stereo)

	if len(mono) != 4 {
		t.Fatalf("expected 4 mono frames, got %d", len(mono))
	}

	// Frame 0: (1000 + 2000) / 2 / 32768 = 1500 / 32768 ~ 0.04577
	expected0 := float32(1500.0 / 32768.0)
	if !approxEqual32(mono[0], expected0, 1e-5) {
		t.Errorf("frame 0: expected %f, got %f", expected0, mono[0])
	}

	// Frame 1: (-1000 + -2000) / 2 / 32768 = -1500 / 32768 ~ -0.04577
	expected1 := float32(-1500.0 / 32768.0)
	if !approxEqual32(mono[1], expected1, 1e-5) {
		t.Errorf("frame 1: expected %f, got %f", expected1, mono[1])
	}

	// Frame 2: (0 + 0) / 2 / 32768 = 0
	if mono[2] != 0 {
		t.Errorf("frame 2: expected 0, got %f", mono[2])
	}

	// Frame 3: (32767 + 32767) / 2 / 32768 ~ 0.99997
	expected3 := float32(32767.0 / 32768.0)
	if !approxEqual32(mono[3], expected3, 1e-4) {
		t.Errorf("frame 3: expected ~%f, got %f", expected3, mono[3])
	}
}

func TestStereoToMonoFloat32_Empty(t *testing.T) {
	mono := stereoToMonoFloat32(nil)
	if mono != nil {
		t.Errorf("expected nil for empty input, got %v", mono)
	}

	mono = stereoToMonoFloat32([]int16{})
	if mono != nil {
		t.Errorf("expected nil for zero-length input, got %v", mono)
	}
}

func TestStereoToMonoFloat32_Range(t *testing.T) {
	// Verify all outputs are within [-1.0, 1.0].
	stereo := []int16{
		-32768, -32768, // min possible
		32767, 32767, // max possible
		-32768, 32767, // opposing extremes
		0, 0, // silence
	}
	mono := stereoToMonoFloat32(stereo)

	for i, v := range mono {
		if v < -1.0 || v > 1.0 {
			t.Errorf("sample[%d] = %f is outside [-1.0, 1.0]", i, v)
		}
	}
}

func TestResampleLinear_SameRate(t *testing.T) {
	input := []float32{0.1, 0.2, 0.3, 0.4, 0.5}
	output := resampleLinear(input, 44100, 44100)

	if len(output) != len(input) {
		t.Fatalf("same-rate resample should preserve length: got %d, want %d", len(output), len(input))
	}
	for i := range input {
		if output[i] != input[i] {
			t.Errorf("sample[%d]: expected %f, got %f", i, input[i], output[i])
		}
	}
}

func TestResampleLinear_Ratio(t *testing.T) {
	// Generate 1 second of mono audio at 44100 Hz.
	inputRate := 44100
	outputRate := 16000
	input := make([]float32, inputRate)
	for i := range input {
		input[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / float64(inputRate)))
	}

	output := resampleLinear(input, inputRate, outputRate)

	// Output length should be approximately (inputLen * outputRate / inputRate).
	expectedLen := int(float64(len(input)) / (float64(inputRate) / float64(outputRate)))
	tolerance := 2 // allow +/- 2 samples for rounding
	if abs(len(output)-expectedLen) > tolerance {
		t.Errorf("output length %d not within tolerance of expected %d", len(output), expectedLen)
	}

	// Verify ratio is approximately correct.
	ratio := float64(len(output)) / float64(len(input))
	expectedRatio := float64(outputRate) / float64(inputRate)
	if math.Abs(ratio-expectedRatio) > 0.001 {
		t.Errorf("resample ratio %f differs from expected %f", ratio, expectedRatio)
	}
}

func TestResampleLinear_OutputRange(t *testing.T) {
	// Input in [-1, 1] range, output should also be in [-1, 1].
	input := make([]float32, 44100)
	for i := range input {
		input[i] = float32(math.Sin(2 * math.Pi * 1000 * float64(i) / 44100.0))
	}

	output := resampleLinear(input, 44100, 16000)

	for i, v := range output {
		if v < -1.0 || v > 1.0 {
			t.Errorf("output[%d] = %f is outside [-1.0, 1.0]", i, v)
		}
	}
}

func TestResampleLinear_Empty(t *testing.T) {
	output := resampleLinear(nil, 44100, 16000)
	if output != nil {
		t.Errorf("expected nil for nil input, got %v", output)
	}

	output = resampleLinear([]float32{}, 44100, 16000)
	if output != nil {
		t.Errorf("expected nil for empty input, got %v", output)
	}
}

func TestResampler_Integration(t *testing.T) {
	// Feed stereo int16 samples through the full Resampler pipeline.
	inputRate := 44100
	outputRate := 16000

	// Generate 0.1 seconds of stereo silence (to keep the test fast).
	numFrames := inputRate / 10
	stereo := make([]int16, numFrames*2) // interleaved L,R
	for i := 0; i < numFrames; i++ {
		val := int16(float64(10000) * math.Sin(2*math.Pi*440*float64(i)/float64(inputRate)))
		stereo[i*2] = val   // left
		stereo[i*2+1] = val // right
	}

	input := make(chan []int16, 1)
	input <- stereo
	close(input)

	r := NewResampler(input, inputRate, outputRate)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx)

	var allSamples []float32
	for chunk := range r.Output() {
		allSamples = append(allSamples, chunk...)
	}

	// Expected output length: numFrames * outputRate / inputRate
	expectedLen := int(float64(numFrames) / (float64(inputRate) / float64(outputRate)))
	tolerance := 2
	if abs(len(allSamples)-expectedLen) > tolerance {
		t.Errorf("output length %d not within tolerance of expected %d", len(allSamples), expectedLen)
	}

	// All output values should be in [-1.0, 1.0].
	for i, v := range allSamples {
		if v < -1.0 || v > 1.0 {
			t.Errorf("output[%d] = %f is outside [-1.0, 1.0]", i, v)
			break
		}
	}
}

func approxEqual32(a, b, epsilon float32) bool {
	return float32(math.Abs(float64(a-b))) < epsilon
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
