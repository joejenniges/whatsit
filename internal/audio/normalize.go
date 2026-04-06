package audio

import "math"

// NormalizeRMS scales samples so their RMS energy matches targetRMS.
// This makes classifier thresholds station-independent -- a quiet stream
// and a loud stream produce the same feature magnitudes after normalization.
//
// If the chunk is near-silent (RMS < 0.0001), returns it unchanged to avoid
// amplifying noise to full scale.
//
// Returns a new slice; the input is not modified.
func NormalizeRMS(samples []float32, targetRMS float32) []float32 {
	if len(samples) == 0 {
		return samples
	}

	// Compute current RMS.
	var sumSq float64
	for _, s := range samples {
		sumSq += float64(s) * float64(s)
	}
	currentRMS := float32(math.Sqrt(sumSq / float64(len(samples))))

	// Near silence -- don't amplify noise.
	if currentRMS < 0.0001 {
		return samples
	}

	scale := targetRMS / currentRMS

	out := make([]float32, len(samples))
	for i, s := range samples {
		v := s * scale
		// Clamp to [-1.0, 1.0] to prevent clipping.
		if v > 1.0 {
			v = 1.0
		} else if v < -1.0 {
			v = -1.0
		}
		out[i] = v
	}
	return out
}
