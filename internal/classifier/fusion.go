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
		// Note: these defaults can be overridden via UpdateThresholds.
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
	// WHY rhythm-primary for speech+music: On radio stations, CED flags
	// both speech=true AND music=true for almost EVERYTHING (the station's
	// audio processing, studio ambience, etc. read as "musical" to CED).
	// The Music label alone is unreliable. Rhythm is the tiebreaker.
	//
	// Decision priority:
	// 1. Singing + strong beat = music (lyrics)
	// 2. Speech flagged + no strong beat = speech (even if Music is also flagged)
	// 3. Music only (no speech) + beat = music
	// 4. Music only (no speech) + no beat = music (ambient/classical)
	// 5. Ambiguous = default to speech (bias toward transcription)

	case cedResult.IsSinging && hasBeat && !(cedResult.IsSpeech && isSpeechLabel(cedResult.TopLabel)):
		// Singing + beat, and the top label isn't a speech label.
		// This is the rock/pop lyrics case.
		raw = ClassMusic
		genre = cedResult.Genre
		genreScore = cedResult.GenreScore

	case cedResult.IsSpeech && !hasBeat:
		// Speech detected and no strong beat. Classify as speech regardless
		// of whether CED also flags Music (it almost always does on radio).
		raw = ClassSpeech

	case cedResult.IsSpeech && hasBeat && !cedResult.IsSinging:
		// Speech + beat but no singing. Could be DJ over music bed OR
		// heavy metal/harsh vocals that CED doesn't flag as "Singing".
		// WHY check top label: if CED's TOP label is Music or a genre,
		// the audio is primarily musical even though speech is also flagged.
		// Heavy metal vocals at rhythm 0.30-0.50 were leaking through
		// because singing=false (harsh vocals != typical singing to CED).
		if isMusicLabel(cedResult.TopLabel) || isGenreLabel(cedResult.TopLabel) {
			raw = ClassMusic
			genre = cedResult.Genre
			genreScore = cedResult.GenreScore
		} else {
			raw = ClassSpeech
		}

	case !cedResult.IsSpeech && cedResult.IsMusic && hasBeat:
		// Music without any speech flag + beat. Clear music.
		raw = ClassMusic
		genre = cedResult.Genre
		genreScore = cedResult.GenreScore

	case !cedResult.IsSpeech && cedResult.IsMusic && !hasBeat:
		// Music without speech, no beat. Ambient/classical/slow.
		raw = ClassMusic
		genre = cedResult.Genre
		genreScore = cedResult.GenreScore

	case !cedResult.IsSpeech && !cedResult.IsMusic && hasBeat:
		// Nothing flagged but rhythm detected -- instrumental.
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

// UpdateThresholds updates the classifier's thresholds at runtime.
// Called when the user saves settings without restarting.
func (f *FusionClassifier) UpdateThresholds(rhythmMusicMin, rhythmSpeechMax, cedSpeechMin, cedMusicMin float64) {
	f.rhythmMusicMin = rhythmMusicMin
	f.rhythmSpeechMax = rhythmSpeechMax
	f.cedSpeechMin = cedSpeechMin
	f.cedMusicMin = cedMusicMin
	log.Printf("fusion: thresholds updated: rhythmMusic=%.2f rhythmSpeech=%.2f cedSpeech=%.2f cedMusic=%.2f",
		rhythmMusicMin, rhythmSpeechMax, cedSpeechMin, cedMusicMin)
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

