package classifier

import "math"

// FrameParams controls how audio is split into overlapping analysis frames.
type FrameParams struct {
	SampleRate int
	FrameLenMs float64 // frame duration in milliseconds (default 25.0)
	FrameHopMs float64 // hop between frames in milliseconds (default 10.0)
}

// DefaultFrameParams returns standard speech-analysis frame parameters:
// 25 ms frames with 10 ms hop at 16 kHz.
func DefaultFrameParams() FrameParams {
	return FrameParams{
		SampleRate: 16000,
		FrameLenMs: 25.0,
		FrameHopMs: 10.0,
	}
}

// SplitIntoFrames divides samples into overlapping Hann-windowed frames.
//
// Each frame has length ceil(SampleRate * FrameLenMs / 1000) samples.
// Frames advance by ceil(SampleRate * FrameHopMs / 1000) samples.
// A Hann window is applied to each frame to reduce spectral leakage.
//
// Example: 2 seconds at 16 kHz with 25 ms / 10 ms produces ~198 frames.
//
// Returns nil if samples is shorter than one frame.
func SplitIntoFrames(samples []float32, params FrameParams) [][]float32 {
	frameLen := int(math.Ceil(float64(params.SampleRate) * params.FrameLenMs / 1000.0))
	frameHop := int(math.Ceil(float64(params.SampleRate) * params.FrameHopMs / 1000.0))

	if frameLen <= 0 || frameHop <= 0 || len(samples) < frameLen {
		return nil
	}

	// Precompute the Hann window coefficients.
	window := make([]float64, frameLen)
	for i := range window {
		window[i] = 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(i)/float64(frameLen-1)))
	}

	numFrames := 1 + (len(samples)-frameLen)/frameHop
	frames := make([][]float32, numFrames)

	for f := 0; f < numFrames; f++ {
		start := f * frameHop
		frame := make([]float32, frameLen)
		for i := 0; i < frameLen; i++ {
			frame[i] = samples[start+i] * float32(window[i])
		}
		frames[f] = frame
	}

	return frames
}
