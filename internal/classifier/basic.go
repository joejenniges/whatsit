package classifier

// BasicClassifier is the simplest (tier 1) classifier. It uses ZCR, RMS
// energy, and spectral flatness to distinguish speech from music. Fast and
// cheap but lower accuracy than higher tiers.
type BasicClassifier struct {
	SilenceThreshold float64
	Debug            bool

	lastClass       Classification
	rawClass        Classification
	consistentCount int
	debounceN       int
}

// NewBasicClassifier returns a BasicClassifier with tuned defaults.
func NewBasicClassifier() *BasicClassifier {
	return &BasicClassifier{
		SilenceThreshold: 0.005,
		debounceN:        5,
		lastClass:        ClassSilence,
	}
}

// Name returns "basic".
func (c *BasicClassifier) Name() string { return "basic" }

// Classify analyses a chunk of PCM audio and returns the classification.
//
// Scoring logic:
//   - High ZCR (> 0.04): +1 speech, (> 0.07): +1 more
//   - High spectral flatness (> 0.2): +1.5 speech, (> 0.4): +1.5 more
//   - Speech if score >= 2.5, else music
//
// WHY these features: ZCR and spectral flatness are the cheapest features
// that still separate speech (noise-like, high flatness, moderate-high ZCR)
// from music (tonal peaks, low flatness). This is the "good enough" tier
// for when CPU matters more than accuracy.
func (c *BasicClassifier) Classify(samples []float32) Classification {
	if len(samples) == 0 {
		return c.debounce(ClassSilence)
	}

	// Silence gate.
	rms := RMSEnergy(samples)
	if rms < c.SilenceThreshold {
		return c.debounce(ClassSilence)
	}

	// Feature extraction.
	zcr := ZeroCrossingRate(samples)
	flatness := SpectralFlatness(samples)

	// Scoring.
	var score float64

	if zcr > 0.04 {
		score += 1.0
	}
	if zcr > 0.07 {
		score += 1.0
	}

	if flatness > 0.2 {
		score += 1.5
	}
	if flatness > 0.4 {
		score += 1.5
	}

	raw := ClassMusic
	if score >= 2.5 {
		raw = ClassSpeech
	}

	if c.Debug {
		debugLog("basic", "zcr=%.4f flatness=%.3f -> %s", zcr, flatness, raw)
	}

	return c.debounce(raw)
}

// debounce requires debounceN consecutive identical raw classifications
// before the output switches. Same logic as LegacyClassifier.debounce.
func (c *BasicClassifier) debounce(raw Classification) Classification {
	if raw == c.rawClass {
		c.consistentCount++
	} else {
		c.rawClass = raw
		c.consistentCount = 1
	}

	if c.consistentCount >= c.debounceN {
		c.lastClass = c.rawClass
	}
	return c.lastClass
}
