package classifier

import "math"

// MFCCThresholds holds decision boundaries for the MFCC-based classifier.
type MFCCThresholds struct {
	SilenceRMS               float64 // RMS below this is silence
	MFCCVarianceSpeechMin    float64 // mean MFCC variance above this suggests speech
	SpectralRolloffSpeechMax float64 // rolloff below this (Hz) suggests speech
	DeltaMFCCMeanSpeechMin   float64 // mean absolute delta-MFCC above this suggests speech
	SpectralFlatnessMusicMax float64 // flatness below this suggests music (tonal content)
	LowEnergyPercentMin      float64 // % low-energy frames above this suggests speech
}

// DefaultMFCCThresholds returns thresholds tuned against real compressed
// radio audio (320kbps MP3, resampled to 16kHz mono).
//
// WHY these values: calibration against a live jazz/music radio stream showed
// music with mfcc_var 14-30, delta 2.1-2.9, rolloff 1555-3723. The original
// thresholds (mfcc_var 12, delta 1.0, rolloff 3500) gave false positives on
// dynamic music genres (jazz, orchestral). Raised thresholds and added flatness
// + low-energy % as counter-features to prevent this.
func DefaultMFCCThresholds() MFCCThresholds {
	return MFCCThresholds{
		SilenceRMS:               0.005,
		MFCCVarianceSpeechMin:    35.0,  // WHY: jazz music reaches 30, speech typically >40
		SpectralRolloffSpeechMax: 2500.0, // WHY: tightened from 3500; music rolloff is 1500-3700
		DeltaMFCCMeanSpeechMin:   3.5,   // WHY: raised from 1.0; music delta is 2.1-2.9
		SpectralFlatnessMusicMax: 0.15,  // WHY: music flatness 0.06-0.16, speech 0.2-0.6
		LowEnergyPercentMin:     0.55,   // WHY: speech has 60-80% low-energy frames, music 40-55%
	}
}

// MFCCClassifier is a Tier-3 music/speech classifier that uses Mel-frequency
// cepstral coefficients as the primary feature set. MFCCs capture the spectral
// envelope shape, which differs systematically between speech (formant
// structure) and music (harmonic/timbral structure).
type MFCCClassifier struct {
	extractor       *MFCCExtractor
	sampleRate      int
	lastClass       Classification
	lastRaw         Classification
	consistentCount int
	debounceN       int
	Thresholds      MFCCThresholds
	Debug           bool
}

// NewMFCCClassifier creates a classifier with default config for the given
// sample rate.
func NewMFCCClassifier(sampleRate int) *MFCCClassifier {
	cfg := DefaultMFCCConfig()
	cfg.SampleRate = sampleRate

	return &MFCCClassifier{
		extractor:  NewMFCCExtractor(cfg),
		sampleRate: sampleRate,
		lastClass:  ClassSilence,
		debounceN:  5,
		Thresholds: DefaultMFCCThresholds(),
	}
}

// Name returns the classifier identifier.
func (c *MFCCClassifier) Name() string { return "mfcc" }

// Classify analyses a chunk of PCM audio (expected ~2s at sampleRate) and
// returns the classification with debounce.
func (c *MFCCClassifier) Classify(samples []float32) ClassifyResult {
	if len(samples) == 0 {
		return c.debounce(ClassSilence)
	}

	// Step 1: silence gate.
	rms := RMSEnergy(samples)
	if rms < c.Thresholds.SilenceRMS {
		return c.debounce(ClassSilence)
	}

	// Step 2: compute MFCCs for all frames.
	mfccs := c.extractor.ComputeChunk(samples)
	if len(mfccs) < 2 {
		return c.debounce(ClassSilence)
	}

	// Step 3: per-coefficient variance, skip coeff 0 (energy).
	// Mean of variance across coefficients 1..12.
	variances := MFCCVariance(mfccs)
	var meanVar float64
	if len(variances) > 1 {
		for i := 1; i < len(variances); i++ {
			meanVar += variances[i]
		}
		meanVar /= float64(len(variances) - 1)
	}

	// Step 4: delta MFCCs -- mean absolute value across all frames and coeffs.
	deltas := DeltaMFCC(mfccs)
	var deltaMean float64
	if len(deltas) > 0 {
		var sum float64
		count := 0
		for _, d := range deltas {
			for _, v := range d {
				sum += math.Abs(v)
				count++
			}
		}
		if count > 0 {
			deltaMean = sum / float64(count)
		}
	}

	// Step 5: spectral rolloff (0.85) -- computed per-frame from power spectrum,
	// averaged across frames.
	rolloff := c.meanSpectralRolloff(samples)

	// Step 6: spectral flatness -- strong music indicator.
	flatness := SpectralFlatness(samples)

	// Step 7: low-energy frame percentage -- speech is bursty with pauses.
	lowEnergy := c.lowEnergyPercent(samples)

	// Step 8: scoring with both speech and music indicators.
	// WHY both directions: the original scoring only had positive (speech)
	// signals with no counter-evidence for music. Jazz and orchestral music
	// have high MFCC variance and high deltas, tripping all speech thresholds.
	// Adding flatness and low-energy % as music counter-signals prevents this.
	var score float64

	// MFCC variance: speech typically >40, music 14-30 (jazz can reach 30).
	if meanVar > c.Thresholds.MFCCVarianceSpeechMin {
		score += 3.0
	} else {
		score -= 1.5
	}

	// Delta MFCC: speech typically >3.5, music 2.1-2.9.
	if deltaMean > c.Thresholds.DeltaMFCCMeanSpeechMin {
		score += 2.0
	} else {
		score -= 1.0
	}

	// Spectral rolloff: speech rolls off lower.
	if rolloff < c.Thresholds.SpectralRolloffSpeechMax {
		score += 1.0
	}

	// Spectral flatness: low flatness = tonal = music.
	if flatness < c.Thresholds.SpectralFlatnessMusicMax {
		score -= 2.0
	} else if flatness > 0.25 {
		score += 1.5 // noise-like = speech
	}

	// Low-energy frame %: speech has lots of pauses (60-80%), music is sustained (40-55%).
	if lowEnergy > c.Thresholds.LowEnergyPercentMin {
		score += 2.0
	} else {
		score -= 1.0
	}

	raw := ClassMusic
	if score > 0 {
		raw = ClassSpeech
	}

	if c.Debug {
		debugLog("mfcc", "mfcc_var=%.1f delta=%.2f rolloff=%.0f flat=%.3f low_e=%.2f score=%.1f -> %s",
			meanVar, deltaMean, rolloff, flatness, lowEnergy, score, raw)
	}

	return c.debounce(raw)
}

// meanSpectralRolloff computes the 0.85 spectral rolloff per frame and returns
// the mean across all frames, in Hz.
func (c *MFCCClassifier) meanSpectralRolloff(samples []float32) float64 {
	cfg := c.extractor.config
	frameLen := int(cfg.FrameLenMs * float64(cfg.SampleRate) / 1000.0)
	hopLen := int(cfg.FrameHopMs * float64(cfg.SampleRate) / 1000.0)

	if len(samples) < frameLen {
		return 0
	}

	numFrames := (len(samples) - frameLen) / hopLen + 1
	freqPerBin := float64(cfg.SampleRate) / float64(cfg.FFTSize)
	bins := cfg.FFTSize/2 + 1

	var rolloffSum float64

	for i := 0; i < numFrames; i++ {
		start := i * hopLen

		// Build power spectrum for this frame.
		buf := make([]complex128, cfg.FFTSize)
		for j := 0; j < frameLen && j < cfg.FFTSize; j++ {
			// WHY: Hann window to match the MFCC pipeline windowing.
			w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(j)/float64(frameLen-1)))
			buf[j] = complex(float64(samples[start+j])*w, 0)
		}
		fft(buf)

		// Compute total power and find 85% rolloff point.
		var totalPower float64
		powers := make([]float64, bins)
		for k := 0; k < bins; k++ {
			mag := real(buf[k])*real(buf[k]) + imag(buf[k])*imag(buf[k])
			powers[k] = mag
			totalPower += mag
		}

		threshold := 0.85 * totalPower
		var cumulative float64
		rolloffBin := bins - 1
		for k := 0; k < bins; k++ {
			cumulative += powers[k]
			if cumulative >= threshold {
				rolloffBin = k
				break
			}
		}

		rolloffSum += float64(rolloffBin) * freqPerBin
	}

	return rolloffSum / float64(numFrames)
}

// lowEnergyPercent computes the fraction of 25ms frames with RMS below the
// chunk's mean RMS. Speech: 60-80% (pauses), Music: 40-55% (sustained).
func (c *MFCCClassifier) lowEnergyPercent(samples []float32) float64 {
	cfg := c.extractor.config
	frameLen := int(cfg.FrameLenMs * float64(cfg.SampleRate) / 1000.0)
	hopLen := int(cfg.FrameHopMs * float64(cfg.SampleRate) / 1000.0)

	if len(samples) < frameLen {
		return 0
	}

	numFrames := (len(samples)-frameLen)/hopLen + 1
	energies := make([]float64, numFrames)
	var totalEnergy float64

	for i := 0; i < numFrames; i++ {
		start := i * hopLen
		end := start + frameLen
		if end > len(samples) {
			end = len(samples)
		}
		energies[i] = float64(RMSEnergy(samples[start:end]))
		totalEnergy += energies[i]
	}

	meanEnergy := totalEnergy / float64(numFrames)
	lowCount := 0
	for _, e := range energies {
		if e < meanEnergy {
			lowCount++
		}
	}
	return float64(lowCount) / float64(numFrames)
}

// debounce requires debounceN consecutive identical raw classifications before
// the output switches. Same logic as the legacy classifier.
func (c *MFCCClassifier) debounce(raw Classification) ClassifyResult {
	if raw == c.lastRaw {
		c.consistentCount++
	} else {
		c.lastRaw = raw
		c.consistentCount = 1
	}

	if c.consistentCount >= c.debounceN {
		c.lastClass = c.lastRaw
	}
	return ClassifyResult{Raw: raw, Debounced: c.lastClass}
}

// MFCCVariance computes the variance of each MFCC coefficient across all
// frames. Returns a slice of length NumCoeffs.
func MFCCVariance(mfccs [][]float64) []float64 {
	if len(mfccs) == 0 {
		return nil
	}

	numCoeffs := len(mfccs[0])
	numFrames := float64(len(mfccs))

	// Compute means.
	means := make([]float64, numCoeffs)
	for _, frame := range mfccs {
		for j, v := range frame {
			means[j] += v
		}
	}
	for j := range means {
		means[j] /= numFrames
	}

	// Compute variances.
	variances := make([]float64, numCoeffs)
	for _, frame := range mfccs {
		for j, v := range frame {
			d := v - means[j]
			variances[j] += d * d
		}
	}
	for j := range variances {
		variances[j] /= numFrames
	}

	return variances
}

// DeltaMFCC computes the first-order difference of MFCCs across frames.
// delta[t] = mfcc[t+1] - mfcc[t], so the result has len(mfccs)-1 frames.
func DeltaMFCC(mfccs [][]float64) [][]float64 {
	if len(mfccs) < 2 {
		return nil
	}

	numCoeffs := len(mfccs[0])
	deltas := make([][]float64, len(mfccs)-1)

	for t := 0; t < len(mfccs)-1; t++ {
		deltas[t] = make([]float64, numCoeffs)
		for j := 0; j < numCoeffs; j++ {
			deltas[t][j] = mfccs[t+1][j] - mfccs[t][j]
		}
	}

	return deltas
}
