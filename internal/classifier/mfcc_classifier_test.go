package classifier

import "testing"

func TestMFCCClassifier_Name(t *testing.T) {
	c := NewMFCCClassifier(16000)
	if c.Name() != "mfcc" {
		t.Errorf("expected name 'mfcc', got '%s'", c.Name())
	}
}

func TestMFCCClassifier_Silence(t *testing.T) {
	c := NewMFCCClassifier(16000)
	c.debounceN = 1 // disable debounce for unit test

	samples := make([]float32, 32000) // 2s silence
	result := c.Classify(samples)
	if result.Debounced != ClassSilence {
		t.Errorf("expected ClassSilence for zero samples, got %s", result.Debounced)
	}
}

func TestMFCCClassifier_EmptyInput(t *testing.T) {
	c := NewMFCCClassifier(16000)
	c.debounceN = 1

	result := c.Classify(nil)
	if result.Debounced != ClassSilence {
		t.Errorf("expected ClassSilence for nil input, got %s", result.Debounced)
	}
}

func TestMFCCClassifier_Debounce(t *testing.T) {
	c := NewMFCCClassifier(16000)
	c.debounceN = 3

	// Generate speech-like samples: bursty energy with high ZCR.
	speech := makeSpeechLike(32000)

	// First two calls should still return debounced=silence (default, debouncing).
	r1 := c.Classify(speech)
	if r1.Debounced != ClassSilence {
		t.Errorf("debounce call 1: expected Debounced=ClassSilence, got %s", r1.Debounced)
	}

	r2 := c.Classify(speech)
	if r2.Debounced != ClassSilence {
		t.Errorf("debounce call 2: expected Debounced=ClassSilence, got %s", r2.Debounced)
	}

	// Third call should switch.
	r3 := c.Classify(speech)
	if r3.Debounced == ClassSilence {
		t.Errorf("debounce call 3: expected non-silence, got %s", r3.Debounced)
	}
}

func TestMFCCClassifier_DebounceInterrupt(t *testing.T) {
	c := NewMFCCClassifier(16000)
	c.debounceN = 3

	speech := makeSpeechLike(32000)
	silence := make([]float32, 32000)

	// Two speech, then silence interrupts.
	c.Classify(speech)
	c.Classify(speech)
	c.Classify(silence) // reset

	// Need 3 more consecutive to switch.
	r := c.Classify(speech)
	if r.Debounced != ClassSilence {
		t.Errorf("expected Debounced=ClassSilence after interrupted debounce, got %s", r.Debounced)
	}
}
