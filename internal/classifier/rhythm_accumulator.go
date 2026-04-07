package classifier

// RhythmAccumulator maintains a longer history of onset envelopes
// across multiple 2-second chunks for more reliable beat detection.
//
// WHY: A single 2-second chunk at 60 BPM contains only 2 beats, which
// isn't enough for strong autocorrelation. The 6-second sliding window
// gives 6-12 beats depending on tempo, producing reliable results.
type RhythmAccumulator struct {
	detector      *RhythmDetector
	onsetHistory  []float64 // Rolling buffer of onset values
	maxHistoryLen int       // Keep ~6 seconds of onset data (600 frames at 10ms hop)
}

// NewRhythmAccumulator creates an accumulator for the given sample rate.
func NewRhythmAccumulator(sampleRate int) *RhythmAccumulator {
	return &RhythmAccumulator{
		detector:      NewRhythmDetector(sampleRate),
		onsetHistory:  make([]float64, 0, 600),
		maxHistoryLen: 600, // 6 seconds at 10ms hop
	}
}

// AddChunkAndAnalyze appends new onset data from the chunk
// and runs rhythm analysis on the full accumulated history.
//
// Returns 0.0 until at least 4 seconds of data have been accumulated,
// then returns the rhythm strength of the full history window.
func (a *RhythmAccumulator) AddChunkAndAnalyze(samples []float32) float64 {
	// Compute onset envelope for this chunk
	newOnsets := a.detector.computeOnsetEnvelope(samples)

	// Append to history
	a.onsetHistory = append(a.onsetHistory, newOnsets...)

	// Trim to max length (sliding window)
	if len(a.onsetHistory) > a.maxHistoryLen {
		a.onsetHistory = a.onsetHistory[len(a.onsetHistory)-a.maxHistoryLen:]
	}

	// Need at least 4 seconds of data for reliable analysis
	minFrames := 400 // ~4 seconds at 10ms hop
	if len(a.onsetHistory) < minFrames {
		return 0.0
	}

	// Autocorrelate the full history and find tempo strength
	acf := a.detector.autocorrelate(a.onsetHistory)
	return a.detector.findTempoStrength(acf)
}

// Reset clears all accumulated history.
// Call when the stream reconnects or classification changes dramatically.
func (a *RhythmAccumulator) Reset() {
	a.onsetHistory = a.onsetHistory[:0]
	a.detector.Reset()
}
