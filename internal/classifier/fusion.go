package classifier

import (
	"log"
	"time"
)

// FusionClassifier combines CED-tiny AI classification with the rhythm
// detector for maximum accuracy on radio audio.
//
// WHY fusion: CED understands *what* audio sounds like (speech, singing,
// guitar, drums). The rhythm detector understands *structure* (is there a
// beat). Together they cover each other's weaknesses:
//   - CED alone misses the distinction between singing-over-music and
//     acapella singing. Rhythm resolves this.
//   - Rhythm alone can't distinguish speech from ambient/beatless music.
//     CED resolves this.
type FusionClassifier struct {
	ced    *CEDClassifier
	rhythm *RhythmAccumulator
	debug  bool

	// Thresholds
	silenceRMS      float64 // default: 0.005
	cedSpeechMin    float64 // default: 0.3 -- CED speech score to consider
	cedMusicMin     float64 // default: 0.3 -- CED music score to consider
	rhythmMusicMin  float64 // default: 0.25 -- rhythm strength for "has beat"
	rhythmSpeechMax float64 // default: 0.15 -- rhythm strength for "no beat"

	// Performance tracking
	LastInferenceMs float64 // last CED inference time in milliseconds

	// Debounce state
	lastClass       Classification
	consistentCount int
	debounceN       int // default: 2

	// Last CED result (for genre info)
	lastCEDResult *CEDResult
}

// FusionResult extends ClassifyResult with genre information.
type FusionResult struct {
	ClassifyResult
	Genre      string  // e.g. "Rock music", "" if not music
	GenreScore float64 // confidence of genre classification
}

// NewFusionClassifier creates a fusion classifier combining CED-tiny + rhythm.
func NewFusionClassifier(cedModelPath string, sampleRate int, debug bool) (*FusionClassifier, error) {
	ced, err := NewCEDClassifier(cedModelPath)
	if err != nil {
		return nil, err
	}

	return &FusionClassifier{
		ced:             ced,
		rhythm:          NewRhythmAccumulator(sampleRate),
		debug:           debug,
		silenceRMS:      0.005,
		cedSpeechMin:    0.3,
		cedMusicMin:     0.3,
		rhythmMusicMin:  0.25,
		rhythmSpeechMax: 0.15,
		lastClass:       ClassSilence,
		debounceN:       2,
	}, nil
}

// Classify implements AudioClassifier. Returns ClassifyResult with both
// raw and debounced classifications.
//
// Decision matrix:
//
//	CED says    | Rhythm says | Final        | Reason
//	------------|-------------|--------------|-------
//	Speech      | No beat     | SPEECH       | Clear speech
//	Speech      | Beat        | MUSIC        | Lyrics over music (rhythm wins)
//	Music/Genre | Beat        | MUSIC+genre  | Clear music with genre
//	Music/Genre | No beat     | MUSIC        | Ambient/slow music (CED wins)
//	Singing     | Beat        | MUSIC        | Singing over music
//	Singing     | No beat     | SPEECH       | Acapella (transcribe it)
//	Low scores  | No beat     | SILENCE      | Nothing significant
//	Low scores  | Beat        | MUSIC        | Instrumental only
func (f *FusionClassifier) Classify(samples []float32) ClassifyResult {
	return f.classifyInternal(samples).ClassifyResult
}

// ClassifyWithGenre returns classification plus genre information.
// Used by the orchestrator to log genre to the database.
func (f *FusionClassifier) ClassifyWithGenre(samples []float32) FusionResult {
	return f.classifyInternal(samples)
}

func (f *FusionClassifier) classifyInternal(samples []float32) FusionResult {
	// 1. Silence check.
	rms := RMSEnergy(samples)
	if rms < f.silenceRMS {
		raw := ClassSilence
		return FusionResult{
			ClassifyResult: f.debounce(raw),
		}
	}

	// 2. Run CED-tiny.
	cedStart := time.Now()
	cedResult, err := f.ced.Classify(samples)
	f.LastInferenceMs = float64(time.Since(cedStart).Microseconds()) / 1000.0
	if err != nil {
		// WHY fallback: CED failure shouldn't crash the pipeline. Fall back
		// to rhythm-only classification, which is better than nothing.
		log.Printf("ced: inference error, falling back to rhythm-only: %v", err)
		rhythmStrength := f.rhythm.AddChunkAndAnalyze(samples)
		raw := ClassSpeech
		if rhythmStrength > f.rhythmMusicMin {
			raw = ClassMusic
		}
		return FusionResult{
			ClassifyResult: f.debounce(raw),
		}
	}
	f.lastCEDResult = cedResult

	// 3. Get rhythm strength.
	rhythmStrength := f.rhythm.AddChunkAndAnalyze(samples)

	// 4. Fusion decision.
	hasBeat := rhythmStrength > f.rhythmMusicMin
	noBeat := rhythmStrength < f.rhythmSpeechMax

	var raw Classification
	var genre string
	var genreScore float64

	switch {
	case cedResult.IsSpeech && !cedResult.IsSinging && noBeat:
		// Speech detected with no beat. Even if CED also flags "Music"
		// (common with background music in ads/studios), no beat means
		// it's speech content. Trust the lack of rhythm.
		// WHY removed !cedResult.IsMusic: CED frequently flags both Speech
		// and Music simultaneously for radio announcer voice over ambient
		// music bed. The beat is the tiebreaker, not the Music label.
		raw = ClassSpeech

	case cedResult.IsSpeech && cedResult.IsMusic && !cedResult.IsSinging && !hasBeat:
		// Speech + Music both flagged, no beat, no singing.
		// This is DJ talking over background music. Transcribe it.
		raw = ClassSpeech

	case cedResult.IsSinging && hasBeat:
		// Singing over music -- this is the rock lyrics case.
		// WHY music: Singing + beat = song playing. Don't transcribe lyrics.
		raw = ClassMusic
		genre = cedResult.Genre
		genreScore = cedResult.GenreScore

	case cedResult.IsSinging && noBeat:
		// Acapella -- transcribe it.
		// WHY speech: Singing without a beat is likely acapella or someone
		// humming/singing along without music. Worth transcribing.
		raw = ClassSpeech

	case cedResult.IsMusic && hasBeat:
		// Clear music with beat.
		raw = ClassMusic
		genre = cedResult.Genre
		genreScore = cedResult.GenreScore

	case cedResult.IsMusic && !hasBeat:
		// Music without clear beat (ambient, classical, slow).
		// WHY still music: CED confidently says music. Trust it even
		// without rhythm confirmation -- ambient/classical often lack beats.
		raw = ClassMusic
		genre = cedResult.Genre
		genreScore = cedResult.GenreScore

	case cedResult.IsSpeech && hasBeat:
		// CED thinks speech but there's a beat.
		// Could be DJ over music bed or lyrics.
		if cedResult.IsMusic || cedResult.IsSinging {
			raw = ClassMusic
			genre = cedResult.Genre
			genreScore = cedResult.GenreScore
		} else {
			// Speech over music bed -- DJ talking, transcribe it.
			raw = ClassSpeech
		}

	case !cedResult.IsSpeech && !cedResult.IsMusic && !cedResult.IsSinging && hasBeat:
		// Low CED scores but rhythm detected -- instrumental.
		raw = ClassMusic

	case !cedResult.IsSpeech && !cedResult.IsMusic && !cedResult.IsSinging && noBeat:
		// Nothing detected -- silence or noise.
		raw = ClassSilence

	default:
		// Ambiguous -- default to speech (bias toward transcription).
		// WHY speech: False positives on speech are cheaper than missing
		// speech. Whisper will just transcribe silence/noise as empty string.
		raw = ClassSpeech
	}

	if f.debug {
		debugLog("fusion", "ced=[speech=%v music=%v singing=%v top=%s(%.2f) genre=%s(%.2f)] rhythm=%.3f -> %s",
			cedResult.IsSpeech, cedResult.IsMusic, cedResult.IsSinging,
			cedResult.TopLabel, cedResult.TopScore,
			cedResult.Genre, cedResult.GenreScore,
			rhythmStrength, raw)
	}

	// 5. Debounce.
	result := f.debounce(raw)

	return FusionResult{
		ClassifyResult: result,
		Genre:          genre,
		GenreScore:     genreScore,
	}
}

// debounce requires debounceN consecutive identical classifications before
// switching output. Asymmetric: quick to speech (1 chunk), slow to music
// (2 chunks). Silence transitions are instant.
func (f *FusionClassifier) debounce(raw Classification) ClassifyResult {
	if raw == f.lastClass {
		f.consistentCount = f.debounceN
		return ClassifyResult{Raw: raw, Debounced: f.lastClass}
	}

	if raw == ClassSilence {
		f.lastClass = ClassSilence
		f.consistentCount = f.debounceN
		return ClassifyResult{Raw: raw, Debounced: f.lastClass}
	}

	f.consistentCount++

	// Asymmetric debounce: quick to speech (1 chunk), slow to music (2 chunks).
	// WHY: Missing a speech transition loses audio forever. Missing a music
	// transition just means we transcribe a bit of music (cheap).
	requiredCount := 1
	if raw == ClassMusic {
		requiredCount = f.debounceN
	}

	if f.consistentCount >= requiredCount {
		f.lastClass = raw
		f.consistentCount = f.debounceN
	}
	return ClassifyResult{Raw: raw, Debounced: f.lastClass}
}

// Name returns the classifier name for display.
func (f *FusionClassifier) Name() string {
	return "ced-tiny+rhythm"
}

// GetLastCEDResult returns the full CED result from the last classification.
func (f *FusionClassifier) GetLastCEDResult() *CEDResult {
	return f.lastCEDResult
}

// GetLastGenre returns the genre from the last classification.
// Useful for the UI and database logging.
func (f *FusionClassifier) GetLastGenre() (string, float64) {
	if f.lastCEDResult != nil {
		return f.lastCEDResult.Genre, f.lastCEDResult.GenreScore
	}
	return "", 0
}

// Destroy cleans up ONNX resources.
func (f *FusionClassifier) Destroy() {
	if f.ced != nil {
		f.ced.Destroy()
	}
}

