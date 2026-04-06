package classifier

const defaultSampleRate = 16000

// LegacyClassifier combines ZCR, RMS energy, spectral centroid, and spectral flux
// to distinguish speech from music in 2-second PCM chunks (16 kHz, mono,
// float32). No AI -- pure DSP heuristics.
//
// Deprecated: Use NewClassifier(tier, sampleRate) via the AudioClassifier interface instead.
// This will be removed once all tiers are implemented and validated.
type LegacyClassifier struct {
	prevSpectrum []float64

	// Configurable thresholds -- sensible defaults set by NewLegacyClassifier.
	SilenceThreshold    float64 // RMS below this = silence
	SpeechZCRMin        float64 // ZCR above this contributes to speech score
	MusicFluxMax        float64 // spectral flux below this contributes to music score
	SpeechCentroidMin   float64 // centroid above this contributes to speech score (Hz)
	SpeechCentroidMax   float64 // centroid above this is unlikely speech
	EnergyVarThreshold  float64 // energy variance above this contributes to speech score
	EnergyVarFrameSize  int     // sub-frame size for energy variance calculation

	// Debounce state -- require debounceN consecutive identical raw
	// classifications before the output actually switches.
	lastClass  Classification
	rawClass   Classification
	classCount int
	debounceN  int
}

// NewLegacyClassifier returns a LegacyClassifier with tuned defaults for 16 kHz audio.
//
// WHY these thresholds: Tuned against real 48kHz 320kbps MP3 radio resampled
// to 16kHz mono. Compressed radio audio has universally high ZCR (0.07-0.15)
// and enormous spectral flux (48K-400K) even for music, so those features
// cannot discriminate well. Energy variance is the strongest discriminator:
// music sustains energy (variance < 0.0005), speech is bursty (variance > 0.001).
func NewLegacyClassifier() *LegacyClassifier {
	return &LegacyClassifier{
		SilenceThreshold:   0.005,
		SpeechZCRMin:       0.15,   // WHY: compressed audio has high ZCR even for music (0.07-0.15). Only extreme ZCR suggests speech.
		MusicFluxMax:       150000, // WHY: real radio music flux ranges 48K-400K. Values below 150K suggest stable content (music-like).
		SpeechCentroidMin:  500.0,
		SpeechCentroidMax:  3500.0, // WHY: tightened from 4000. Music centroid clusters 1500-2400 on radio.
		EnergyVarThreshold: 0.0005, // WHY: music energy variance is 0.000005-0.000070 on radio. Speech pauses push this above 0.001.
		EnergyVarFrameSize: 1600,   // 100 ms frames at 16 kHz
		debounceN:          5,      // WHY: increased from 3. Radio transitions are gradual (DJ talk -> music fade).
		lastClass:          ClassSilence,
	}
}

// SetDebounce configures the number of consecutive identical raw
// classifications required before the output switches. Must be >= 1.
func (c *LegacyClassifier) SetDebounce(n int) {
	if n < 1 {
		n = 1
	}
	c.debounceN = n
}

// Classify analyses a chunk of PCM audio and returns the classification.
//
// Expected input: ~2 seconds of 16 kHz mono float32 (32000 samples).
// Shorter or longer buffers work but may affect threshold accuracy.
func (c *LegacyClassifier) Classify(samples []float32) ClassifyResult {
	if len(samples) == 0 {
		return c.debounce(ClassSilence)
	}

	// Step 1: silence gate
	rms := RMSEnergy(samples)
	if rms < c.SilenceThreshold {
		return c.debounce(ClassSilence)
	}

	// Step 2: feature extraction
	zcr := ZeroCrossingRate(samples)
	centroid := SpectralCentroid(samples, defaultSampleRate)
	spectrum := MagnitudeSpectrum(samples)

	var flux float64
	if c.prevSpectrum != nil {
		flux = SpectralFlux(spectrum, c.prevSpectrum)
	}

	energyVar := EnergyVariance(samples, c.EnergyVarFrameSize)

	// Store spectrum for next call's flux calculation.
	c.prevSpectrum = spectrum

	// Step 3: weighted scoring
	// Positive score = speech, negative = music.
	//
	// WHY these weights: On real compressed radio audio, ZCR and spectral
	// flux are poor discriminators (both are high for music and speech).
	// Energy variance is by far the strongest signal: speech has pauses
	// that create high variance, music sustains energy. Centroid and flux
	// provide secondary signal with reduced weight.
	var score float64

	// ZCR: weak discriminator on compressed audio -- low weight.
	// WHY reduced: compressed MP3 music has ZCR 0.07-0.15, overlapping
	// heavily with speech. Only very high ZCR is meaningful.
	if zcr >= c.SpeechZCRMin {
		score += 0.5
	} else {
		score -= 0.25
	}

	// Spectral centroid: speech lives in a characteristic band
	if centroid >= c.SpeechCentroidMin && centroid <= c.SpeechCentroidMax {
		score += 0.5
	} else if centroid < c.SpeechCentroidMin {
		// Very low centroid -- probably bass-heavy music
		score -= 0.5
	} else {
		// High centroid (>3500 Hz) -- more likely music with bright content
		score -= 0.5
	}

	// Spectral flux: moderate discriminator on real audio.
	// WHY: real music flux is 48K-400K. Low flux (<150K) suggests stable
	// tonal content. But high flux alone doesn't mean speech.
	if flux > c.MusicFluxMax {
		score += 0.5
	} else if c.prevSpectrum != nil {
		score -= 1.0
	}

	// Energy variance: strongest discriminator for radio audio.
	// WHY heavily weighted: music energy variance is 0.000005-0.000070,
	// speech with pauses is typically > 0.001. This is the most reliable
	// feature for distinguishing radio speech from radio music.
	if energyVar > c.EnergyVarThreshold {
		score += 3.0
	} else {
		score -= 2.0
	}

	raw := ClassMusic
	if score > 0 {
		raw = ClassSpeech
	}

	return c.debounce(raw)
}

// debounce applies hysteresis: the output only changes after debounceN
// consecutive identical raw classifications.
func (c *LegacyClassifier) debounce(raw Classification) ClassifyResult {
	if raw == c.rawClass {
		c.classCount++
	} else {
		c.rawClass = raw
		c.classCount = 1
	}

	if c.classCount >= c.debounceN {
		c.lastClass = c.rawClass
	}
	return ClassifyResult{Raw: raw, Debounced: c.lastClass}
}
