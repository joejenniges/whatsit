package audio

import (
	"context"
)

// Resampler converts stereo int16 PCM at one sample rate to mono float32 PCM
// at another sample rate. Designed for the 44100 Hz stereo -> 16000 Hz mono
// conversion that Whisper requires, but works for arbitrary rate pairs.
//
// Conversion steps:
//  1. Downmix stereo to mono: mono[i] = (left[i] + right[i]) / 2
//  2. Convert int16 to float32: float32(sample) / 32768.0
//  3. Resample using linear interpolation
type Resampler struct {
	inputRate  int
	outputRate int
	input      <-chan []int16
	output     chan []float32
}

// NewResampler creates a Resampler that reads stereo int16 samples from the
// input channel and outputs mono float32 samples resampled to outputRate.
func NewResampler(input <-chan []int16, inputRate, outputRate int) *Resampler {
	return &Resampler{
		inputRate:  inputRate,
		outputRate: outputRate,
		input:      input,
		output:     make(chan []float32, 32),
	}
}

// Start begins resampling in a background goroutine.
func (r *Resampler) Start(ctx context.Context) {
	go r.resampleLoop(ctx)
}

// Output returns the channel of resampled mono float32 sample slices.
func (r *Resampler) Output() <-chan []float32 {
	return r.output
}

func (r *Resampler) resampleLoop(ctx context.Context) {
	defer close(r.output)

	for {
		select {
		case <-ctx.Done():
			return
		case stereoSamples, ok := <-r.input:
			if !ok {
				return
			}

			// Step 1: Downmix stereo interleaved int16 to mono float32.
			mono := stereoToMonoFloat32(stereoSamples)

			// Step 2: Resample from inputRate to outputRate using linear interpolation.
			resampled := resampleLinear(mono, r.inputRate, r.outputRate)

			select {
			case r.output <- resampled:
			case <-ctx.Done():
				return
			}
		}
	}
}

// stereoToMonoFloat32 takes interleaved stereo int16 samples [L, R, L, R, ...]
// and returns mono float32 samples in the range [-1.0, 1.0].
func stereoToMonoFloat32(stereo []int16) []float32 {
	// Stereo samples come in pairs: [left, right, left, right, ...]
	numFrames := len(stereo) / 2
	if numFrames == 0 {
		return nil
	}
	mono := make([]float32, numFrames)
	for i := 0; i < numFrames; i++ {
		left := float32(stereo[i*2])
		right := float32(stereo[i*2+1])
		// Average the two channels, then normalize to [-1.0, 1.0].
		mono[i] = (left + right) / 2.0 / 32768.0
	}
	return mono
}

// resampleLinear resamples mono float32 audio from srcRate to dstRate using
// linear interpolation. This is sufficient quality for speech recognition.
func resampleLinear(samples []float32, srcRate, dstRate int) []float32 {
	if len(samples) == 0 {
		return nil
	}
	if srcRate == dstRate {
		out := make([]float32, len(samples))
		copy(out, samples)
		return out
	}

	ratio := float64(srcRate) / float64(dstRate)
	outLen := int(float64(len(samples)) / ratio)
	if outLen == 0 {
		return nil
	}
	out := make([]float32, outLen)

	for i := 0; i < outLen; i++ {
		srcPos := float64(i) * ratio
		srcIdx := int(srcPos)
		frac := float32(srcPos - float64(srcIdx))

		if srcIdx+1 < len(samples) {
			// Linear interpolation between two adjacent samples.
			out[i] = samples[srcIdx]*(1.0-frac) + samples[srcIdx+1]*frac
		} else if srcIdx < len(samples) {
			out[i] = samples[srcIdx]
		}
	}
	return out
}
