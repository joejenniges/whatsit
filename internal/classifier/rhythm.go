package classifier

import "math"

// RhythmDetector analyzes audio chunks for periodic beat structure.
// Music has a consistent beat (high rhythm strength).
// Speech does not (low rhythm strength).
//
// WHY: MFCC and Scheirer classifiers misclassify sung lyrics as speech
// because the vocal features look speech-like. But underneath the vocals,
// drums/bass/guitar are locked to a tempo grid. Autocorrelation of the
// onset envelope exposes this periodicity reliably.
type RhythmDetector struct {
	sampleRate int

	// Frame parameters
	frameLenSamples int // 25ms = 400 samples at 16kHz
	frameHopSamples int // 10ms = 160 samples at 16kHz

	// Tempo search range
	minBPM float64 // 60 BPM -- slowest tempo to consider
	maxBPM float64 // 200 BPM -- fastest tempo to consider

	// Previous frame's magnitude spectrum (for spectral flux)
	prevSpectrum []float64
}

// NewRhythmDetector creates a detector for the given sample rate.
func NewRhythmDetector(sampleRate int) *RhythmDetector {
	frameLenSamples := int(0.025 * float64(sampleRate)) // 25ms
	frameHopSamples := int(0.010 * float64(sampleRate)) // 10ms

	return &RhythmDetector{
		sampleRate:      sampleRate,
		frameLenSamples: frameLenSamples,
		frameHopSamples: frameHopSamples,
		minBPM:          60,
		maxBPM:          200,
		prevSpectrum:    nil,
	}
}

// RhythmStrength computes how "beat-like" a chunk of audio is.
//
// Returns a value between 0.0 and 1.0:
//
//	0.0 - 0.2: No rhythmic structure (speech, silence, noise)
//	0.2 - 0.4: Weak or ambiguous rhythm
//	0.4 - 0.7: Moderate rhythm (music with irregular patterns)
//	0.7 - 1.0: Strong rhythm (rock, pop, electronic)
func (r *RhythmDetector) RhythmStrength(samples []float32) float64 {
	onsetEnvelope := r.computeOnsetEnvelope(samples)

	if len(onsetEnvelope) < 10 {
		return 0.0
	}

	acf := r.autocorrelate(onsetEnvelope)
	return r.findTempoStrength(acf)
}

// computeOnsetEnvelope splits audio into frames, computes spectral flux
// for each frame, and returns the half-wave rectified onset strength.
//
// Spectral flux measures how much the spectrum changes between frames.
// Large positive changes correspond to note onsets (drum hits, chord changes).
// Half-wave rectification keeps only onsets (energy increases), not decays.
func (r *RhythmDetector) computeOnsetEnvelope(samples []float32) []float64 {
	numFrames := (len(samples) - r.frameLenSamples) / r.frameHopSamples
	if numFrames <= 0 {
		return nil
	}

	envelope := make([]float64, numFrames)
	fftSize := nextPow2(r.frameLenSamples)

	for i := 0; i < numFrames; i++ {
		start := i * r.frameHopSamples

		// Build complex buffer with Hann window applied
		buf := make([]complex128, fftSize)
		for j := 0; j < r.frameLenSamples && start+j < len(samples); j++ {
			w := 0.5 * (1.0 - math.Cos(2.0*math.Pi*float64(j)/float64(r.frameLenSamples-1)))
			buf[j] = complex(float64(samples[start+j])*w, 0)
		}
		// Remainder is already zero-valued

		// In-place FFT using existing radix-2 implementation from spectral.go
		fft(buf)

		// Magnitude spectrum (positive frequencies only)
		bins := fftSize/2 + 1
		spectrum := make([]float64, bins)
		for j := 0; j < bins; j++ {
			r2 := real(buf[j])
			im := imag(buf[j])
			spectrum[j] = math.Sqrt(r2*r2 + im*im)
		}

		// Spectral flux: sum of positive magnitude differences (half-wave rectified)
		flux := 0.0
		if r.prevSpectrum != nil && len(r.prevSpectrum) == len(spectrum) {
			for j := 0; j < len(spectrum); j++ {
				diff := spectrum[j] - r.prevSpectrum[j]
				if diff > 0 {
					flux += diff
				}
			}
		}

		envelope[i] = flux
		r.prevSpectrum = spectrum
	}

	// Normalize envelope to [0, 1]
	maxVal := 0.0
	for _, v := range envelope {
		if v > maxVal {
			maxVal = v
		}
	}
	if maxVal > 0 {
		for i := range envelope {
			envelope[i] /= maxVal
		}
	}

	return envelope
}

// autocorrelate computes the normalized autocorrelation of the onset envelope.
//
// ACF[lag] = sum(centered[i] * centered[i+lag]) / ACF[0]
//
// Reveals periodicity: if the envelope has a consistent beat, the ACF peaks
// at lags corresponding to the beat period and its multiples.
func (r *RhythmDetector) autocorrelate(envelope []float64) []float64 {
	n := len(envelope)
	maxLag := n / 2
	acf := make([]float64, maxLag)

	// Subtract mean to remove DC bias
	m := 0.0
	for _, v := range envelope {
		m += v
	}
	m /= float64(n)

	centered := make([]float64, n)
	for i, v := range envelope {
		centered[i] = v - m
	}

	for lag := 0; lag < maxLag; lag++ {
		sum := 0.0
		for i := 0; i < n-lag; i++ {
			sum += centered[i] * centered[i+lag]
		}
		acf[lag] = sum
	}

	// Normalize by ACF[0] (variance)
	if acf[0] > 0 {
		norm := acf[0]
		for i := range acf {
			acf[i] /= norm
		}
	}

	return acf
}

// findTempoStrength searches the autocorrelation for peaks in the musical
// tempo range (60-200 BPM) and returns a strength score.
//
// Lag-to-BPM: lag (frames) * hopSecs = period (seconds), BPM = 60/period
//
// Strength = (peakValue - meanValue) with harmonic boosting for peaks at
// 2x and 0.5x the primary lag. Real music almost always has energy at
// harmonic multiples; this distinguishes it from coincidental periodicity.
func (r *RhythmDetector) findTempoStrength(acf []float64) float64 {
	if len(acf) == 0 {
		return 0.0
	}

	hopSecs := float64(r.frameHopSamples) / float64(r.sampleRate)

	// Convert BPM range to lag range (higher BPM = smaller lag)
	minLag := int(60.0 / (r.maxBPM * hopSecs)) // 200 BPM -> small lag
	maxLag := int(60.0 / (r.minBPM * hopSecs))  // 60 BPM -> large lag

	if minLag < 1 {
		minLag = 1
	}
	if maxLag >= len(acf) {
		maxLag = len(acf) - 1
	}
	if minLag >= maxLag {
		return 0.0
	}

	peakValue := -1.0
	peakLag := minLag
	sum := 0.0
	count := 0

	for lag := minLag; lag <= maxLag; lag++ {
		val := acf[lag]
		sum += val
		count++
		if val > peakValue {
			peakValue = val
			peakLag = lag
		}
	}

	meanValue := sum / float64(count)

	if peakValue <= meanValue || peakValue <= 0 {
		return 0.0
	}

	strength := peakValue - meanValue
	if strength > 1.0 {
		strength = 1.0
	}

	// Harmonic check: real music has energy at multiples of the beat period.
	harmonicBoost := 0.0

	// Double-period (half tempo)
	doubleLag := peakLag * 2
	if doubleLag < len(acf) && acf[doubleLag] > meanValue {
		harmonicBoost += 0.15
	}

	// Half-period (double tempo)
	halfLag := peakLag / 2
	if halfLag >= minLag && acf[halfLag] > meanValue {
		harmonicBoost += 0.15
	}

	// Triple-period (one-third tempo)
	tripleLag := peakLag * 3
	if tripleLag < len(acf) && acf[tripleLag] > meanValue {
		harmonicBoost += 0.1
	}

	strength += harmonicBoost
	if strength > 1.0 {
		strength = 1.0
	}

	return strength
}

// Reset clears the previous spectrum state.
// Call this when starting a new audio stream or after a gap.
func (r *RhythmDetector) Reset() {
	r.prevSpectrum = nil
}
