package classifier

import "testing"

func TestIsWhisperMusicOutput(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		music bool
	}{
		{"empty", "", true},
		{"music marker", "[Music]", true},
		{"music marker lower", "[music]", true},
		{"blank audio", "[BLANK_AUDIO]", true},
		{"silence marker", "[Silence]", true},
		{"parenthesized", "(Music playing)", true},
		{"music notes only", "♪♫♪♫", true},
		{"thank you hallucination", "Thank you.", true},
		{"thanks for watching", "Thanks for watching!", true},
		{"repeated word", "you you you you", true},
		{"repeated phrase", "thank you thank you thank you", true},
		{"too short", "Oh", true},
		{"two words only", "Oh no", true},
		{"real speech short", "Hello there folks", false},
		{"real speech", "Welcome back to the show we have a great lineup tonight", false},
		{"mixed markers and speech", "[Music] and now the news is here today", false},
		{"single word repeated 2x", "yes yes", true}, // <3 words
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isWhisperMusicOutput(tt.text)
			if got != tt.music {
				t.Errorf("isWhisperMusicOutput(%q) = %v, want %v", tt.text, got, tt.music)
			}
		})
	}
}

func TestIsRepeatedPhrase(t *testing.T) {
	tests := []struct {
		name   string
		words  []string
		repeat bool
	}{
		{"single repeated", []string{"you", "you", "you"}, true},
		{"bigram repeated", []string{"thank", "you", "thank", "you", "thank", "you"}, true},
		{"trigram repeated", []string{"oh", "my", "god", "oh", "my", "god", "oh", "my", "god"}, true},
		{"not repeated", []string{"the", "quick", "brown", "fox"}, false},
		{"too short", []string{"hi", "there"}, false},
		{"partial repeat", []string{"yes", "yes", "no"}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := isRepeatedPhrase(tt.words)
			if got != tt.repeat {
				t.Errorf("isRepeatedPhrase(%v) = %v, want %v", tt.words, got, tt.repeat)
			}
		})
	}
}

func TestWhisperClassifierBuffering(t *testing.T) {
	callCount := 0
	mockClassify := func(samples []float32) (string, float32, error) {
		callCount++
		return "Hello this is a test broadcast", 0.85, nil
	}

	c := NewWhisperClassifier(mockClassify)

	// Feed less than 4 seconds (64000 samples) -- should not trigger inference.
	short := make([]float32, 32000) // 2 seconds
	class := c.Classify(short)
	if callCount != 0 {
		t.Errorf("expected no inference call with 2s of audio, got %d calls", callCount)
	}
	if class != ClassMusic {
		t.Errorf("expected default ClassMusic before first inference, got %s", class)
	}

	// Feed another 2 seconds -- now at 4s, should trigger.
	class = c.Classify(short)
	if callCount != 1 {
		t.Errorf("expected 1 inference call after 4s of audio, got %d", callCount)
	}
	if class != ClassSpeech {
		t.Errorf("expected ClassSpeech for real text with high prob, got %s", class)
	}
	if c.LastText() != "Hello this is a test broadcast" {
		t.Errorf("expected LastText to be set, got %q", c.LastText())
	}
}

func TestWhisperClassifierMusicResult(t *testing.T) {
	mockClassify := func(samples []float32) (string, float32, error) {
		return "[Music]", 0.15, nil
	}

	c := NewWhisperClassifier(mockClassify)

	// Feed enough audio to trigger inference.
	chunk := make([]float32, 64000)
	class := c.Classify(chunk)
	if class != ClassMusic {
		t.Errorf("expected ClassMusic for [Music] output, got %s", class)
	}
	if c.LastText() != "" {
		t.Errorf("expected empty LastText for music, got %q", c.LastText())
	}
}

func TestWhisperClassifierLowConfidence(t *testing.T) {
	mockClassify := func(samples []float32) (string, float32, error) {
		return "some garbled text here now", 0.2, nil
	}

	c := NewWhisperClassifier(mockClassify)

	chunk := make([]float32, 64000)
	class := c.Classify(chunk)
	if class != ClassMusic {
		t.Errorf("expected ClassMusic for low confidence, got %s", class)
	}
}

func TestWhisperClassifierSilence(t *testing.T) {
	mockClassify := func(samples []float32) (string, float32, error) {
		return "", 0, nil
	}

	c := NewWhisperClassifier(mockClassify)

	chunk := make([]float32, 64000)
	class := c.Classify(chunk)
	if class != ClassSilence {
		t.Errorf("expected ClassSilence for empty text, got %s", class)
	}
}
