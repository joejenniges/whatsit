package classifier

// ZeroCrossingRate calculates the fraction of consecutive sample pairs where
// the signal crosses zero. A crossing is counted when two adjacent samples
// have different signs. The result is in the range [0, 1].
//
// Typical values at 16 kHz:
//   - Speech: 0.02 - 0.10
//   - Music:  0.01 - 0.05
func ZeroCrossingRate(samples []float32) float64 {
	if len(samples) < 2 {
		return 0
	}

	crossings := 0
	for i := 1; i < len(samples); i++ {
		// WHY: using strict sign comparison rather than threshold crossing.
		// Near-zero samples could be noise, but for 16-bit-origin audio the
		// quantisation floor is low enough that this doesn't matter in practice.
		if (samples[i] >= 0) != (samples[i-1] >= 0) {
			crossings++
		}
	}

	return float64(crossings) / float64(len(samples)-1)
}
