package classifier

import "math"

// RMSEnergy computes the root mean square of the sample amplitudes.
// Returns 0 for empty input.
//
// Typical values for float32 audio normalised to [-1, 1]:
//   - Silence: < 0.001
//   - Speech:  variable, bursty (0.01 - 0.15)
//   - Music:   sustained (0.03 - 0.20)
func RMSEnergy(samples []float32) float64 {
	if len(samples) == 0 {
		return 0
	}

	var sumSq float64
	for _, s := range samples {
		sumSq += float64(s) * float64(s)
	}
	return math.Sqrt(sumSq / float64(len(samples)))
}

// EnergyVariance computes the variance of per-frame RMS energy across
// sub-frames of the given size. High variance indicates bursty energy
// (speech); low variance indicates sustained energy (music).
func EnergyVariance(samples []float32, frameSize int) float64 {
	if frameSize <= 0 || len(samples) < frameSize {
		return 0
	}

	numFrames := len(samples) / frameSize
	if numFrames < 2 {
		return 0
	}

	energies := make([]float64, numFrames)
	var sum float64
	for i := 0; i < numFrames; i++ {
		e := RMSEnergy(samples[i*frameSize : (i+1)*frameSize])
		energies[i] = e
		sum += e
	}

	mean := sum / float64(numFrames)
	var varSum float64
	for _, e := range energies {
		d := e - mean
		varSum += d * d
	}
	return varSum / float64(numFrames)
}
