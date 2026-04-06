package classifier

import (
	"log"
	"regexp"
	"strings"
	"unicode"
)

// WhisperClassifyFunc is a function that runs whisper on audio and returns
// the transcribed text, average token probability, and any error.
// The orchestrator provides this by wrapping the transcriber.
type WhisperClassifyFunc func(samples []float32) (text string, avgProb float32, err error)

// WhisperClassifier uses whisper inference itself to distinguish speech from
// music. When whisper produces real words with decent confidence, the audio is
// speech. When it produces music markers, garbage, or low-confidence output,
// the audio is music.
//
// WHY: DSP-based classifiers (basic, scheirer, mfcc) all struggle with dynamic
// music like jazz because its acoustic features overlap with speech. Whisper
// is the ultimate arbiter -- if it can transcribe real words, it's speech.
type WhisperClassifier struct {
	classify WhisperClassifyFunc

	lastClass Classification
	lastText  string // transcription text from most recent speech classification

	// Accumulate audio before classifying. Short chunks (2s) are unreliable
	// for whisper -- need at least 4s for decent output.
	buffer    []float32
	bufferMin int // minimum samples before classifying (4s = 64000)

	Debug bool
}

// NewWhisperClassifier creates a classifier that uses whisper inference.
// classifyFunc is provided by the orchestrator wrapping the transcriber.
func NewWhisperClassifier(classifyFunc WhisperClassifyFunc) *WhisperClassifier {
	return &WhisperClassifier{
		classify:  classifyFunc,
		lastClass: ClassMusic, // assume music until proven otherwise
		bufferMin: 64000,      // 4 seconds at 16kHz
	}
}

// Classify appends samples to the internal buffer and, once enough audio has
// accumulated, runs whisper to determine if the audio is speech or music.
func (c *WhisperClassifier) Classify(samples []float32) Classification {
	c.buffer = append(c.buffer, samples...)

	if len(c.buffer) < c.bufferMin {
		return c.lastClass
	}

	// Run whisper on accumulated buffer.
	text, avgProb, err := c.classify(c.buffer)
	c.buffer = c.buffer[:0] // clear buffer regardless of result

	if err != nil {
		log.Printf("whisper-classifier: inference error: %v", err)
		return c.lastClass
	}

	if c.Debug {
		log.Printf("whisper-classifier: text=%q avgProb=%.3f", text, avgProb)
	}

	// Classify based on whisper output.
	text = strings.TrimSpace(text)

	if text == "" {
		c.lastClass = ClassSilence
		c.lastText = ""
		return c.lastClass
	}

	if isWhisperMusicOutput(text) {
		c.lastClass = ClassMusic
		c.lastText = ""
		return c.lastClass
	}

	// WHY 0.3 not 0.4: The hallucination filter in whisper.go uses 0.4 for
	// individual segment rejection. Here we're classifying the whole chunk,
	// and we want to be more permissive -- a short DJ break with some
	// background music might have lower confidence but still be real speech.
	if avgProb < 0.3 {
		if c.Debug {
			log.Printf("whisper-classifier: low confidence (%.3f < 0.3), classifying as music", avgProb)
		}
		c.lastClass = ClassMusic
		c.lastText = ""
		return c.lastClass
	}

	// Real words with decent confidence -> speech.
	c.lastClass = ClassSpeech
	c.lastText = text
	return c.lastClass
}

// Name returns the classifier tier name.
func (c *WhisperClassifier) Name() string { return "whisper" }

// LastText returns the transcription text from the most recent Classify call
// that returned ClassSpeech. Empty if the last call returned music/silence.
func (c *WhisperClassifier) LastText() string {
	return c.lastText
}

// isWhisperMusicOutput returns true if whisper's output indicates music rather
// than real speech. Checks for music markers, very short output, and repeated
// phrase hallucinations.
func isWhisperMusicOutput(text string) bool {
	lower := strings.ToLower(strings.TrimSpace(text))

	// Empty after trimming.
	if lower == "" {
		return true
	}

	// Known music/non-speech markers (exact match).
	markers := []string{
		"[music]", "[blank_audio]", "[silence]", "[applause]",
		"(music)", "(silence)", "(blank audio)",
		"[music playing]", "(music playing)",
		"[no speech detected]",
		"thank you.", "thanks for watching.",
		"thanks for watching!", "thank you for watching.",
		"thank you for watching!",
	}
	for _, m := range markers {
		if lower == m {
			return true
		}
	}

	// Bracketed/parenthesized content -> non-speech marker.
	if (strings.HasPrefix(lower, "[") && strings.HasSuffix(lower, "]")) ||
		(strings.HasPrefix(lower, "(") && strings.HasSuffix(lower, ")")) {
		return true
	}

	// Strip all music notes and markers to count real words.
	cleaned := stripMusicMarkers(lower)
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return true
	}

	// Very short output (<3 real words) is likely not meaningful speech.
	words := realWords(cleaned)
	if len(words) < 3 {
		return true
	}

	// Repeated phrase hallucination: all words identical ("you you you you").
	if allWordsIdentical(words) {
		return true
	}

	// Repeated phrase pattern: "thank you thank you thank you" (repeating bigrams).
	if isRepeatedPhrase(words) {
		return true
	}

	return false
}

// bracketPattern matches [anything] or (anything) markers.
var bracketPattern = regexp.MustCompile(`[\[\(][^\]\)]*[\]\)]`)

// musicNotePattern matches unicode music symbols.
var musicNotePattern = regexp.MustCompile(`[♪♫♩♬🎵🎶]`)

// stripMusicMarkers removes music notes and bracketed markers from text.
func stripMusicMarkers(text string) string {
	text = bracketPattern.ReplaceAllString(text, "")
	text = musicNotePattern.ReplaceAllString(text, "")
	return text
}

// realWords splits text into words, filtering out punctuation-only tokens.
func realWords(text string) []string {
	fields := strings.Fields(text)
	var words []string
	for _, f := range fields {
		// Keep only if it contains at least one letter or digit.
		hasAlphaNum := false
		for _, r := range f {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				hasAlphaNum = true
				break
			}
		}
		if hasAlphaNum {
			words = append(words, f)
		}
	}
	return words
}

// allWordsIdentical returns true if all words in the slice are the same.
func allWordsIdentical(words []string) bool {
	if len(words) < 3 {
		return false
	}
	for _, w := range words[1:] {
		if w != words[0] {
			return false
		}
	}
	return true
}

// isRepeatedPhrase detects repeated short phrases like "thank you thank you thank you".
// Checks for repeating patterns of 1-3 words.
func isRepeatedPhrase(words []string) bool {
	if len(words) < 3 {
		return false
	}
	for phraseLen := 1; phraseLen <= 3; phraseLen++ {
		if len(words)%phraseLen != 0 {
			continue
		}
		repeats := len(words) / phraseLen
		if repeats < 3 {
			continue
		}
		pattern := words[:phraseLen]
		allMatch := true
		for i := 1; i < repeats; i++ {
			for j := 0; j < phraseLen; j++ {
				if words[i*phraseLen+j] != pattern[j] {
					allMatch = false
					break
				}
			}
			if !allMatch {
				break
			}
		}
		if allMatch {
			return true
		}
	}
	return false
}
