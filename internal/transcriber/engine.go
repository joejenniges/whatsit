package transcriber

// Compile-time check that Transcriber implements ASREngine.
var _ ASREngine = (*Transcriber)(nil)

// ASREngine defines the interface for speech-to-text engines.
// The orchestrator interacts with this interface, not concrete implementations.
// This allows swapping whisper.cpp for Parakeet, Vosk, or any other engine
// without changing the orchestrator code.
type ASREngine interface {
	// Transcribe runs inference on a complete audio segment (16kHz mono float32).
	// Returns the transcribed text. Used in segment transcription mode.
	Transcribe(samples []float32) (string, error)

	// FeedChunk adds audio to a rolling window and returns new text when
	// enough audio has accumulated. Used in rolling transcription mode.
	// Returns: text (new portion only), triggered (whether inference ran), error.
	FeedChunk(chunk []float32) (text string, triggered bool, err error)

	// ClassifyChunk runs inference and returns raw text + average token
	// probability. Used by the whisper classifier tier. Engines that don't
	// support this should return ("", 0, nil).
	ClassifyChunk(samples []float32) (text string, avgProb float32, err error)

	// Reset clears any internal state (rolling window, previous text).
	// Called on classification transitions.
	Reset()

	// Close releases all resources. The engine must not be used after Close.
	Close()
}
