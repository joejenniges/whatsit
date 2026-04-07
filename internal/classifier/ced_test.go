package classifier

import (
	"math"
	"testing"
)

func TestLoadAudioSetLabels_Count(t *testing.T) {
	labels := LoadAudioSetLabels()
	if len(labels) != 527 {
		t.Errorf("expected 527 labels, got %d", len(labels))
	}
}

func TestLoadAudioSetLabels_KnownIndices(t *testing.T) {
	labels := LoadAudioSetLabels()

	checks := map[int]string{
		0:   "Speech",
		1:   "Male speech, man speaking",
		2:   "Female speech, woman speaking",
		27:  "Singing",
		137: "Music",
		140: "Guitar",
		153: "Piano",
		162: "Drum kit",
		216: "Pop music",
		219: "Rock music",
		235: "Jazz",
		237: "Classical music",
		500: "Silence",
		526: "Field recording",
	}

	for idx, expected := range checks {
		if labels[idx] != expected {
			t.Errorf("label[%d]: expected %q, got %q", idx, expected, labels[idx])
		}
	}
}

func TestIsSpeechLabel(t *testing.T) {
	if !isSpeechLabel("Speech") {
		t.Error("Speech should be a speech label")
	}
	if !isSpeechLabel("Male speech, man speaking") {
		t.Error("Male speech should be a speech label")
	}
	if isSpeechLabel("Music") {
		t.Error("Music should not be a speech label")
	}
	if isSpeechLabel("Singing") {
		t.Error("Singing should not be a speech label")
	}
}

func TestIsSingingLabel(t *testing.T) {
	if !isSingingLabel("Singing") {
		t.Error("Singing should be a singing label")
	}
	if !isSingingLabel("Rapping") {
		t.Error("Rapping should be a singing label")
	}
	if isSingingLabel("Speech") {
		t.Error("Speech should not be a singing label")
	}
}

func TestIsMusicLabel(t *testing.T) {
	if !isMusicLabel("Music") {
		t.Error("Music should be a music label")
	}
	if !isMusicLabel("Rock music") {
		t.Error("Rock music should be a music label")
	}
	if !isMusicLabel("Jazz") {
		t.Error("Jazz should be a music label")
	}
	if isMusicLabel("Speech") {
		t.Error("Speech should not be a music label")
	}
}

func TestIsGenreLabel(t *testing.T) {
	genres := []string{
		"Rock music", "Pop music", "Hip hop music", "Jazz", "Blues",
		"Classical music", "Electronic music", "Heavy metal",
	}
	for _, g := range genres {
		if !isGenreLabel(g) {
			t.Errorf("%q should be a genre label", g)
		}
	}
	if isGenreLabel("Music") {
		t.Error("Music (top-level) should not be a genre label")
	}
}

func TestPadOrTruncate_Pad(t *testing.T) {
	// 10 frames, pad to 1024.
	spec := make([][]float32, 10)
	for i := range spec {
		spec[i] = make([]float32, 64)
		spec[i][0] = float32(i) // sentinel value
	}

	result := padOrTruncate(spec, 1024, 64)
	if len(result) != 1024 {
		t.Fatalf("expected 1024 frames, got %d", len(result))
	}

	// Original frames should be preserved.
	for i := 0; i < 10; i++ {
		if result[i][0] != float32(i) {
			t.Errorf("frame %d: expected sentinel %d, got %f", i, i, result[i][0])
		}
	}

	// Padded frames should be zero.
	for i := 10; i < 1024; i++ {
		for j := 0; j < 64; j++ {
			if result[i][j] != 0 {
				t.Errorf("padded frame %d bin %d: expected 0, got %f", i, j, result[i][j])
			}
		}
	}
}

func TestPadOrTruncate_Truncate(t *testing.T) {
	spec := make([][]float32, 2000)
	for i := range spec {
		spec[i] = make([]float32, 64)
		spec[i][0] = float32(i)
	}

	result := padOrTruncate(spec, 1024, 64)
	if len(result) != 1024 {
		t.Fatalf("expected 1024 frames, got %d", len(result))
	}
	if result[1023][0] != 1023 {
		t.Errorf("last frame sentinel: expected 1023, got %f", result[1023][0])
	}
}

func TestPadOrTruncate_ExactSize(t *testing.T) {
	spec := make([][]float32, 1024)
	for i := range spec {
		spec[i] = make([]float32, 64)
	}

	result := padOrTruncate(spec, 1024, 64)
	if len(result) != 1024 {
		t.Fatalf("expected 1024 frames, got %d", len(result))
	}
}

func TestComputeMelSpectrogram_OutputShape(t *testing.T) {
	// 2 seconds of 16kHz audio = 32000 samples.
	// Expected frames: (32000 - 512) / 160 + 1 = 197 frames.
	samples := make([]float32, 32000)
	// Fill with a simple sine wave to get non-zero output.
	for i := range samples {
		samples[i] = float32(math.Sin(2 * math.Pi * 440 * float64(i) / 16000))
	}

	c := &CEDClassifier{
		melFB:      NewMelFilterbank(64, 512, 16000),
		sampleRate: 16000,
		nMels:      64,
		fftSize:    512,
		hopLength:  160,
	}

	spec := c.computeMelSpectrogram(samples)

	expectedFrames := (32000-512)/160 + 1
	if len(spec) != expectedFrames {
		t.Errorf("expected %d frames, got %d", expectedFrames, len(spec))
	}

	if len(spec) > 0 && len(spec[0]) != 64 {
		t.Errorf("expected 64 mel bins, got %d", len(spec[0]))
	}

	// Values should be finite (no NaN or Inf).
	for i, frame := range spec {
		for j, v := range frame {
			if math.IsNaN(float64(v)) || math.IsInf(float64(v), 0) {
				t.Errorf("frame %d bin %d: non-finite value %f", i, j, v)
			}
		}
	}
}

func TestComputeMelSpectrogram_EmptyInput(t *testing.T) {
	c := &CEDClassifier{
		melFB:      NewMelFilterbank(64, 512, 16000),
		sampleRate: 16000,
		nMels:      64,
		fftSize:    512,
		hopLength:  160,
	}

	spec := c.computeMelSpectrogram(nil)
	if spec != nil {
		t.Error("expected nil for empty input")
	}

	spec = c.computeMelSpectrogram(make([]float32, 100))
	if spec != nil {
		t.Error("expected nil for input shorter than FFT size")
	}
}
