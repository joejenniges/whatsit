package classifier

// Classification represents the audio content type of a chunk.
type Classification string

const (
	ClassSpeech  Classification = "speech"
	ClassMusic   Classification = "music"
	ClassSilence Classification = "silence"
)

// AudioClassifier is the interface all classification tiers implement.
type AudioClassifier interface {
	// Classify analyses a chunk of PCM audio (mono float32) and returns
	// the classification. Implementations may maintain internal state
	// (e.g. debounce, previous spectrum) between calls.
	Classify(samples []float32) Classification

	// Name returns a human-readable identifier for the tier.
	Name() string
}

// NewClassifier creates a classifier for the given tier.
// Valid tiers: "basic", "scheirer", "mfcc".
// Default (empty string or unknown): "scheirer".
//
// Note: "whisper" tier cannot be created here because it requires a callback.
// The orchestrator creates it directly via NewWhisperClassifier.
// When debug is true, the classifier logs raw feature values to stderr
// after each Classify() call.
func NewClassifier(tier string, sampleRate int, debug bool) AudioClassifier {
	switch tier {
	case "basic":
		c := NewBasicClassifier()
		c.Debug = debug
		return c
	case "scheirer":
		c := NewScheirerClassifier(sampleRate)
		c.Debug = debug
		return c
	case "mfcc":
		c := NewMFCCClassifier(sampleRate)
		c.Debug = debug
		return c
	case "whisper":
		// WHY nil: Whisper classifier needs a WhisperClassifyFunc callback
		// that wraps the transcriber. The orchestrator creates it directly
		// via NewWhisperClassifier and injects the callback.
		return nil
	default:
		// WHY: scheirer is the best cost/accuracy tradeoff for radio audio.
		// It implements the Scheirer & Slaney (1997) 4-feature approach
		// which was specifically designed for broadcast classification.
		c := NewScheirerClassifier(sampleRate)
		c.Debug = debug
		return c
	}
}
