//go:build whisper

package transcriber

import (
	"testing"
)

func TestNewTranscriber_MissingModel(t *testing.T) {
	_, err := NewTranscriber(TranscriberConfig{
		ModelPath: "/nonexistent/path/to/model.bin",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent model path, got nil")
	}
	t.Logf("got expected error: %v", err)
}

func TestNewTranscriber_EmptyPath(t *testing.T) {
	_, err := NewTranscriber(TranscriberConfig{})
	if err == nil {
		t.Fatal("expected error for empty model path, got nil")
	}
	if err.Error() != "model path is required" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTranscribe_ClosedTranscriber(t *testing.T) {
	// We can't create a real transcriber without a model, but we can test
	// that Transcribe on a nil-context returns the right error.
	tr := &Transcriber{
		ctx:    nil,
		config: TranscriberConfig{Language: "en", Threads: 1},
	}
	_, err := tr.Transcribe([]float32{0.0, 0.1, 0.2})
	if err == nil {
		t.Fatal("expected error from closed transcriber")
	}
	if err.Error() != "transcriber is closed" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestTranscribe_EmptySamples(t *testing.T) {
	tr := &Transcriber{
		ctx:    nil,
		config: TranscriberConfig{Language: "en", Threads: 1},
	}
	text, err := tr.Transcribe(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if text != "" {
		t.Fatalf("expected empty string, got %q", text)
	}
}

func TestIsHallucination(t *testing.T) {
	tests := []struct {
		text string
		want bool
	}{
		{"[BLANK_AUDIO]", true},
		{"[Music]", true},
		{"[MUSIC]", true},
		{"(silence)", true},
		{"[something weird]", true},  // any bracketed text
		{"Thank you.", true},
		{"Thanks for watching!", true},
		{"a", true},                   // too short
		{"..", true},                  // too short
		{"you you you", true},         // repeated
		{"the the the the", true},     // repeated
		{"Hello world", false},
		{"The quick brown fox", false},
		{"you and me", false},         // not all same word
	}

	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			// Pass nil ctx and -1 segIdx -- the token probability check
			// will be skipped because nTokens will be 0 for nil ctx.
			// We're testing the text-based checks only here.
			got := isHallucinationText(tt.text)
			if got != tt.want {
				t.Errorf("isHallucinationText(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}
