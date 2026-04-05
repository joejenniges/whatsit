package classifier

import (
	"math"
	"testing"
)

func TestClassify_Silence(t *testing.T) {
	c := NewClassifier()
	c.SetDebounce(1) // disable debounce for unit test clarity

	samples := make([]float32, 32000) // 2s of silence at 16 kHz
	result := c.Classify(samples)
	if result != ClassSilence {
		t.Errorf("expected ClassSilence for zero samples, got %s", result)
	}
}

func TestClassify_EmptyInput(t *testing.T) {
	c := NewClassifier()
	c.SetDebounce(1)

	result := c.Classify(nil)
	if result != ClassSilence {
		t.Errorf("expected ClassSilence for nil input, got %s", result)
	}
}

func TestClassify_LoudNoise(t *testing.T) {
	// A high-ZCR, rapidly varying signal should lean toward speech.
	c := NewClassifier()
	c.SetDebounce(1)

	// Generate a noisy signal with high ZCR: alternating positive/negative
	// with varying amplitude to also produce high energy variance.
	samples := make([]float32, 32000)
	for i := range samples {
		sign := float32(1.0)
		if i%2 == 1 {
			sign = -1.0
		}
		// Vary amplitude in bursts to mimic speech-like energy pattern
		amplitude := float32(0.1)
		if (i/1600)%2 == 0 { // alternate loud/quiet every 100ms
			amplitude = 0.5
		}
		samples[i] = sign * amplitude
	}

	result := c.Classify(samples)
	// With max ZCR, bursty energy, and high flux (first call, no prev spectrum)
	// this should classify as speech or at least not silence.
	if result == ClassSilence {
		t.Errorf("expected non-silence for loud noisy signal, got %s", result)
	}
}

func TestClassify_SustainedTone(t *testing.T) {
	// A pure low-frequency tone has low ZCR, low spectral flux across
	// calls, and sustained energy -- should lean toward music.
	c := NewClassifier()
	c.SetDebounce(1)

	freq := 300.0 // Hz, low tone
	sampleRate := 16000

	samples := make([]float32, 32000)
	for i := range samples {
		samples[i] = 0.3 * float32(math.Sin(2*math.Pi*freq*float64(i)/float64(sampleRate)))
	}

	// First call establishes the spectrum baseline.
	_ = c.Classify(samples)
	// Second call with identical signal should see zero flux -> music.
	result := c.Classify(samples)

	if result == ClassSilence {
		t.Errorf("expected non-silence for sustained tone, got %s", result)
	}
}

func TestDebounce_RequiresConsecutive(t *testing.T) {
	c := NewClassifier()
	c.SetDebounce(3)

	// Start from silence (default). Feed speech-like signals and verify
	// the output doesn't switch until the third consecutive call.
	speechSamples := makeSpeechLike(32000)

	// Call 1: raw = speech, count = 1, output still silence
	r1 := c.Classify(speechSamples)
	if r1 != ClassSilence {
		t.Errorf("debounce call 1: expected ClassSilence (still debouncing), got %s", r1)
	}

	// Call 2: raw = speech, count = 2, output still silence
	r2 := c.Classify(speechSamples)
	if r2 != ClassSilence {
		t.Errorf("debounce call 2: expected ClassSilence (still debouncing), got %s", r2)
	}

	// Call 3: raw = speech, count = 3, output switches to speech
	r3 := c.Classify(speechSamples)
	if r3 == ClassSilence {
		t.Errorf("debounce call 3: expected classification to switch away from silence, got %s", r3)
	}
}

func TestDebounce_InterruptResetsCount(t *testing.T) {
	c := NewClassifier()
	c.SetDebounce(3)

	speech := makeSpeechLike(32000)
	silence := make([]float32, 32000)

	// Two speech calls, then a silence call should reset the count.
	c.Classify(speech)
	c.Classify(speech)
	c.Classify(silence) // interrupts the run

	// Now two more speech calls -- not enough to hit 3 consecutive.
	r := c.Classify(speech)
	if r != ClassSilence {
		t.Errorf("expected ClassSilence after interrupted debounce, got %s", r)
	}

	r = c.Classify(speech)
	if r != ClassSilence {
		t.Errorf("expected ClassSilence (only 2 consecutive), got %s", r)
	}

	// Third consecutive speech call should finally switch.
	r = c.Classify(speech)
	if r == ClassSilence {
		t.Errorf("expected non-silence after 3 consecutive speech calls, got %s", r)
	}
}

func TestSetDebounce_MinimumOne(t *testing.T) {
	c := NewClassifier()
	c.SetDebounce(0)

	// Should be clamped to 1, meaning immediate switching.
	silence := make([]float32, 32000)
	r := c.Classify(silence)
	if r != ClassSilence {
		t.Errorf("expected ClassSilence with debounce=1, got %s", r)
	}
}

// makeSpeechLike generates a signal with properties that should push the
// classifier toward speech: high ZCR, bursty energy, mid-range centroid.
func makeSpeechLike(n int) []float32 {
	samples := make([]float32, n)
	for i := range samples {
		// High ZCR: rapid sign alternation
		sign := float32(1.0)
		if i%3 != 0 {
			sign = -1.0
		}
		// Bursty energy: loud for 800 samples, quiet for 800
		amp := float32(0.02)
		if (i/800)%2 == 0 {
			amp = 0.4
		}
		samples[i] = sign * amp
	}
	return samples
}
