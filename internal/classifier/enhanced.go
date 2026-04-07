package classifier

// EnhancedClassifier wraps any AudioClassifier and adds rhythm-based
// beat detection as an override signal. This solves the rock/pop lyrics
// problem where DSP classifiers see speech-like vocal features but miss
// the underlying beat structure.
//
// WHY wrapper instead of modifying existing classifiers: Each classifier
// (scheirer, mfcc, whisper) has its own scoring logic. Adding rhythm as
// a wrapper means it works with any inner classifier without touching
// their internals. Also allows A/B testing: "scheirer" vs "scheirer+rhythm".
type EnhancedClassifier struct {
	inner  AudioClassifier
	rhythm *RhythmAccumulator
	debug  bool

	// Rhythm thresholds
	musicRhythmMin  float64 // above this + base says speech -> override to music
	speechRhythmMax float64 // below this + base says music -> override to speech

	// Debounce state for the combined output
	lastClass       Classification
	consistentCount int
	debounceN       int
}

// NewEnhancedClassifier wraps an existing classifier with rhythm detection.
func NewEnhancedClassifier(inner AudioClassifier, sampleRate int, debug bool) *EnhancedClassifier {
	return &EnhancedClassifier{
		inner:           inner,
		rhythm:          NewRhythmAccumulator(sampleRate),
		debug:           debug,
		// WHY 0.40 not 0.30: Robotic/processed speech (station IDs, jingles)
		// with evenly-timed words produces rhythm strengths of 0.30-0.36.
		// Real rock/pop music hits 0.60-0.95. The 0.40 threshold avoids
		// false positives on rhythmic speech while still catching all music.
		musicRhythmMin:  0.40,
		speechRhythmMax: 0.15,
		lastClass:       ClassSilence,
		debounceN:       2,
	}
}

// Classify analyses audio using both the inner classifier and rhythm detection.
//
// Rhythm can OVERRIDE the base classification in clear cases:
//   - rhythmStrength >= 0.30 AND base says speech -> override to MUSIC
//     (catches sung lyrics over a beat)
//   - rhythmStrength <= 0.15 AND base says music -> override to SPEECH
//     (catches speech over ambient/pad music)
//   - 0.15-0.30 ambiguous zone -> trust base classifier
//     (handles edge cases like DJ over light music beds)
func (c *EnhancedClassifier) Classify(samples []float32) ClassifyResult {
	baseResult := c.inner.Classify(samples)
	baseClass := baseResult.Raw // use raw, not debounced, for override decisions

	rhythmStrength := c.rhythm.AddChunkAndAnalyze(samples)

	finalClass := baseClass

	if rhythmStrength >= c.musicRhythmMin && baseClass == ClassSpeech {
		// Strong beat detected but base thinks speech -> music with lyrics.
		finalClass = ClassMusic
		if c.debug {
			debugLog("rhythm", "OVERRIDE speech->music (strength=%.3f >= %.2f)",
				rhythmStrength, c.musicRhythmMin)
		}
	}

	if rhythmStrength <= c.speechRhythmMax && baseClass == ClassMusic {
		// No beat detected but base thinks music -> speech over ambient.
		finalClass = ClassSpeech
		if c.debug {
			debugLog("rhythm", "OVERRIDE music->speech (strength=%.3f <= %.2f)",
				rhythmStrength, c.speechRhythmMax)
		}
	}

	if c.debug {
		debugLog("rhythm", "strength=%.3f base=%s final=%s",
			rhythmStrength, baseClass, finalClass)
	}

	// Silence passes through directly (no rhythm analysis needed).
	if baseClass == ClassSilence {
		finalClass = ClassSilence
	}

	return c.debounce(finalClass)
}

// debounce requires debounceN consecutive identical classifications before
// switching output. Asymmetric: silence transitions are instant.
func (c *EnhancedClassifier) debounce(raw Classification) ClassifyResult {
	if raw == c.lastClass {
		c.consistentCount = c.debounceN
		return ClassifyResult{Raw: raw, Debounced: c.lastClass}
	}

	if raw == ClassSilence {
		c.lastClass = ClassSilence
		c.consistentCount = c.debounceN
		return ClassifyResult{Raw: raw, Debounced: c.lastClass}
	}

	c.consistentCount++
	if c.consistentCount >= c.debounceN {
		c.lastClass = raw
		c.consistentCount = c.debounceN
	}
	return ClassifyResult{Raw: raw, Debounced: c.lastClass}
}

// Name returns the combined classifier name.
func (c *EnhancedClassifier) Name() string {
	return c.inner.Name() + "+rhythm"
}
