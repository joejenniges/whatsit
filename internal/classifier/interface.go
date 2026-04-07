package classifier

// Classification represents the audio content type of a chunk.
type Classification string

const (
	ClassSpeech  Classification = "speech"
	ClassMusic   Classification = "music"
	ClassSilence Classification = "silence"
)

// ClassifyResult contains both the raw (pre-debounce) and final (debounced)
// classification. The orchestrator uses Raw to buffer audio during transitions,
// preventing audio loss when the debounce hasn't confirmed a state change yet.
//
// WHY both values: With debounce_chunks=5 and 2-second chunks, the classifier
// needs 10 seconds of consistent classification before switching state. A
// 5-second DJ break between songs only produces 2-3 speech chunks -- not enough
// to flip the debounce. Without Raw, those chunks get classified as "music"
// (the debounced state) and the speech is silently discarded.
type ClassifyResult struct {
	Raw       Classification // What this chunk actually looks like
	Debounced Classification // The stable, debounced output
}

// AudioClassifier is the interface all classification tiers implement.
type AudioClassifier interface {
	// Classify analyses a chunk of PCM audio (mono float32) and returns
	// both raw and debounced classifications. Implementations may maintain
	// internal state (e.g. debounce, previous spectrum) between calls.
	Classify(samples []float32) ClassifyResult

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
	case "scheirer+rhythm":
		inner := NewScheirerClassifier(sampleRate)
		inner.Debug = debug
		return NewEnhancedClassifier(inner, sampleRate, debug)
	case "mfcc+rhythm":
		inner := NewMFCCClassifier(sampleRate)
		inner.Debug = debug
		return NewEnhancedClassifier(inner, sampleRate, debug)
	case "fusion":
		// WHY nil: Fusion classifier needs the CED model path, which is
		// resolved by the orchestrator. The orchestrator creates it directly
		// via NewFusionClassifier.
		return nil
	case "whisper", "whisper+rhythm":
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
