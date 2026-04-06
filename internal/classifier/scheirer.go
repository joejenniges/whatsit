package classifier

import "math"

// ScheirerFeatures holds the four features from Scheirer & Slaney (1997)
// "Construction and Evaluation of a Robust Multifeature Speech/Music Discriminator".
// Designed specifically for broadcast radio classification.
type ScheirerFeatures struct {
	SpectralCentroidVariance float64
	SpectralFluxMean         float64
	ZCRMean                  float64
	LowEnergyPercent         float64
}

// ScheirerThresholds controls the decision boundaries for each feature.
// Values above the threshold contribute speech points to the score.
type ScheirerThresholds struct {
	CentroidVarSpeechMin      float64 // Centroid variance above this -> speech
	FluxMeanSpeechMin         float64 // Flux mean above this -> speech
	LowEnergyPercentSpeechMin float64 // Low-energy frame % above this -> speech
	ZCRMeanSpeechMin          float64 // ZCR mean above this -> speech
}

// DefaultScheirerThresholds returns thresholds tuned for 16 kHz mono audio.
//
// WHY these defaults: The paper doesn't publish exact thresholds (they used
// trained classifiers), so these are empirical starting points. Low-energy %
// is the most stable feature -- speech has 60-80% low-energy frames due to
// pauses, music sits at 40-55% with sustained energy. The others need tuning
// against real radio, same story as the legacy classifier.
func DefaultScheirerThresholds() ScheirerThresholds {
	return ScheirerThresholds{
		CentroidVarSpeechMin:      50000,
		FluxMeanSpeechMin:         0.5,
		LowEnergyPercentSpeechMin: 0.55,
		ZCRMeanSpeechMin:          0.04,
	}
}

// ScheirerClassifier implements the Scheirer & Slaney (1997) 4-feature
// speech/music discriminator with debounce for temporal stability.
type ScheirerClassifier struct {
	sampleRate int
	frameLen   int // 25ms in samples
	hopLen     int // 10ms in samples

	Thresholds ScheirerThresholds
	Debug      bool

	// Debounce state
	lastClass       Classification
	consistentCount int
	debounceN       int

	// Optional: carry spectrum across chunks for flux continuity.
	prevSpectrum []float64
}

// NewScheirerClassifier creates a classifier using Scheirer & Slaney features.
// sampleRate is typically 16000 for this pipeline.
func NewScheirerClassifier(sampleRate int) *ScheirerClassifier {
	return &ScheirerClassifier{
		sampleRate:   sampleRate,
		frameLen:     sampleRate * 25 / 1000, // 400 @ 16kHz
		hopLen:       sampleRate * 10 / 1000, // 160 @ 16kHz
		Thresholds:   DefaultScheirerThresholds(),
		lastClass:    ClassSilence,
		debounceN:    5,
	}
}

// Name returns the classifier identifier.
func (c *ScheirerClassifier) Name() string { return "scheirer" }

// Classify analyses a chunk of PCM audio using the four Scheirer features
// and returns a debounced classification.
func (c *ScheirerClassifier) Classify(samples []float32) ClassifyResult {
	if len(samples) < c.frameLen {
		return c.debounce(ClassSilence)
	}

	// Silence gate: if the whole chunk is quiet, skip feature extraction.
	rms := RMSEnergy(samples)
	if rms < 0.005 {
		return c.debounce(ClassSilence)
	}

	features := ComputeScheirerFeatures(samples, c.sampleRate)

	// Scoring: positive = speech, compare each feature against threshold.
	// Total possible: 6.0 (all features indicate speech).
	var score float64

	// Centroid variance: speech phonemes shift the spectral center of gravity.
	// Weight: 2.0 -- strong discriminator in the paper.
	if features.SpectralCentroidVariance > c.Thresholds.CentroidVarSpeechMin {
		score += 2.0
	}

	// Spectral flux mean: speech has more rapid spectral changes.
	// Weight: 1.5
	if features.SpectralFluxMean > c.Thresholds.FluxMeanSpeechMin {
		score += 1.5
	}

	// Low-energy %: the key insight from the paper. Speech has many
	// low-energy frames (pauses between words/syllables).
	// Weight: 1.5 -- strongest single feature for radio.
	if features.LowEnergyPercent > c.Thresholds.LowEnergyPercentSpeechMin {
		score += 1.5
	}

	// ZCR mean: speech tends to have higher zero-crossing rate.
	// Weight: 1.0 -- weakest individual feature.
	if features.ZCRMean > c.Thresholds.ZCRMeanSpeechMin {
		score += 1.0
	}

	raw := ClassMusic
	if score >= 3.5 {
		raw = ClassSpeech
	}

	if c.Debug {
		debugLog("scheirer", "centroid_var=%.1f flux=%.3f low_energy=%.3f zcr=%.4f -> %s",
			features.SpectralCentroidVariance, features.SpectralFluxMean,
			features.LowEnergyPercent, features.ZCRMean, raw)
	}

	return c.debounce(raw)
}

// debounce requires debounceN consecutive identical raw classifications
// before switching the output. Prevents rapid toggling on transitions.
// Returns both the raw and debounced classification so the orchestrator
// can buffer audio during transitions.
func (c *ScheirerClassifier) debounce(raw Classification) ClassifyResult {
	if raw == c.lastClass {
		// Already outputting this class, keep the counter saturated.
		c.consistentCount = c.debounceN
		return ClassifyResult{Raw: raw, Debounced: c.lastClass}
	}

	// Raw disagrees with current output.
	// WHY: We track consecutive disagreements, not consecutive agreements
	// with the new class. This is simpler and equivalent when there are
	// only two non-silence classes competing. For silence, it transitions
	// immediately since silence is unambiguous (gated by RMS).
	if raw == ClassSilence {
		// Silence transitions are instant -- the RMS gate already filtered.
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

// ComputeScheirerFeatures extracts the four Scheirer & Slaney features
// from a chunk of audio samples.
//
// The chunk is split into 25ms Hann-windowed frames with 10ms hop.
// Per-frame: spectral centroid, spectral flux, ZCR, RMS energy.
// Aggregate: variance of centroids, mean of flux, mean of ZCR,
// percentage of frames with energy below the chunk mean.
func ComputeScheirerFeatures(samples []float32, sampleRate int) ScheirerFeatures {
	frameLen := sampleRate * 25 / 1000
	hopLen := sampleRate * 10 / 1000

	if len(samples) < frameLen || frameLen <= 0 || hopLen <= 0 {
		return ScheirerFeatures{}
	}

	// Count frames
	numFrames := 1 + (len(samples)-frameLen)/hopLen
	if numFrames < 1 {
		return ScheirerFeatures{}
	}

	centroids := make([]float64, 0, numFrames)
	fluxes := make([]float64, 0, numFrames)
	zcrs := make([]float64, 0, numFrames)
	energies := make([]float64, 0, numFrames)

	var prevMag []float64

	for i := 0; i < numFrames; i++ {
		start := i * hopLen
		end := start + frameLen
		if end > len(samples) {
			break
		}

		frame := samples[start:end]

		// Spectral centroid (MagnitudeSpectrum already applies Hann window)
		centroid := SpectralCentroid(frame, sampleRate)
		centroids = append(centroids, centroid)

		// Spectral flux: L2-norm between adjacent frames.
		// WHY: Using full flux (not half-wave rectified) here because the
		// paper computes total spectral distance, not just onset detection.
		mag := MagnitudeSpectrum(frame)
		if prevMag != nil {
			flux := spectralFluxL2(mag, prevMag)
			fluxes = append(fluxes, flux)
		}
		prevMag = mag

		// ZCR per frame
		zcrs = append(zcrs, ZeroCrossingRate(frame))

		// RMS energy per frame
		energies = append(energies, RMSEnergy(frame))
	}

	var features ScheirerFeatures

	// Spectral centroid variance
	features.SpectralCentroidVariance = variance(centroids)

	// Spectral flux mean
	features.SpectralFluxMean = mean(fluxes)

	// ZCR mean
	features.ZCRMean = mean(zcrs)

	// Low-energy frame percentage
	features.LowEnergyPercent = LowEnergyPercent(energies)

	return features
}

// LowEnergyPercent returns the fraction of frame energies that fall below
// the mean energy of all frames. This is the key Scheirer insight: speech
// has many low-energy frames (60-80%) due to pauses between words, while
// music sustains energy more evenly (40-55%).
func LowEnergyPercent(frameEnergies []float64) float64 {
	if len(frameEnergies) == 0 {
		return 0
	}

	m := mean(frameEnergies)
	count := 0
	for _, e := range frameEnergies {
		if e < m {
			count++
		}
	}
	return float64(count) / float64(len(frameEnergies))
}

// spectralFluxL2 computes the L2-norm (Euclidean distance) between two
// magnitude spectra. This measures total spectral change, not just onsets.
func spectralFluxL2(current, previous []float64) float64 {
	n := len(current)
	if len(previous) < n {
		n = len(previous)
	}
	if n == 0 {
		return 0
	}

	var sum float64
	for i := 0; i < n; i++ {
		diff := current[i] - previous[i]
		sum += diff * diff
	}
	return math.Sqrt(sum)
}

// variance computes the population variance of a slice.
func variance(vals []float64) float64 {
	if len(vals) < 2 {
		return 0
	}
	m := mean(vals)
	var sum float64
	for _, v := range vals {
		d := v - m
		sum += d * d
	}
	return sum / float64(len(vals))
}

// mean computes the arithmetic mean of a slice.
func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}
