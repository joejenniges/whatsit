package app

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"runtime/debug"
	"sync"
	"time"

	"github.com/joe/radio-transcriber/internal/audio"
	"github.com/joe/radio-transcriber/internal/classifier"
	"github.com/joe/radio-transcriber/internal/config"
	"github.com/joe/radio-transcriber/internal/storage"
	"github.com/joe/radio-transcriber/internal/transcriber"
)

// whisperSampleRate is the sample rate whisper.cpp expects (16 kHz mono).
const whisperSampleRate = 16000

// UI defines the interface the orchestrator uses to interact with the GUI.
// This decouples the orchestrator from any concrete UI framework (Fyne, etc).
type UI interface {
	SetCallbacks(onStart, onStop func(), onSave func(*config.Config))
	SetListenCallback(func(enabled bool))
	SetLoadHistoryCallback(func(limit int) ([]storage.LogEntry, error))
	SetEditSongCallback(func(id int64, title, artist string) error)
	SetInsertSongCallback(func() (*storage.LogEntry, error))
	ShowDownloadScreen(modelsDir, modelSize string)
	ShowMainScreen()
	UpdateDownloadProgress(downloaded, total int64)
	AppendTranscription(timestamp time.Time, text string, dbID int64)
	AppendSong(title, artist string, dbID int64)
	UpdateSongLine(title, artist string)
	AppendMusic()
	ClearMusicMarker()
	UpdateStatus(connected bool, classification string)
	UpdateLatency(latency time.Duration)
	ShowGPUWarning(message string)
	Run()
}

// Orchestrator is the central coordinator that wires the audio pipeline,
// classifier, transcriber, music identification, and storage together.
type Orchestrator struct {
	config *config.Config
	ui     UI
	db     *storage.Database

	streamer    *audio.Streamer
	decoder     *audio.Decoder
	resampler   *audio.Resampler
	classifier  classifier.AudioClassifier
	transcriber transcriber.ASREngine

	// WHY: Listener plays decoded stereo PCM through speakers when the user
	// toggles "Listen" on. It sits between decoder and resampler in the
	// pipeline -- pre-resampling so the user hears full-quality stereo audio.
	listener *audio.Listener

	speechBuffer  *audio.Buffer // segment mode: accumulate speech, transcribe on transition
	musicMarkerUp bool          // true if a "Song played" marker is currently showing
	musicDBLogged bool          // true if we've already written a music_unknown DB entry for this segment
	listenWanted  bool // true if user checked Listen before streaming started
	recorder      *audio.SegmentRecorder

	ctx    context.Context
	cancel context.CancelFunc

	// Guards streaming state so start/stop from UI callbacks don't race.
	mu        sync.Mutex
	streaming bool
}

// NewOrchestrator creates an Orchestrator. Call Start() to begin.
func NewOrchestrator(cfg *config.Config, ui UI) *Orchestrator {
	return &Orchestrator{
		config: cfg,
		ui:     ui,
	}
}

// Start initializes all subsystems and runs the UI event loop (blocks).
func (o *Orchestrator) Start() {
	appDir, err := config.AppDir()
	if err != nil {
		log.Fatalf("app: resolve app directory: %v", err)
	}

	// Open SQLite database.
	dbPath := filepath.Join(appDir, "transcripts.db")
	o.db, err = storage.New(dbPath)
	if err != nil {
		log.Fatalf("app: open database: %v", err)
	}

	modelsDir := filepath.Join(appDir, "models")

	log.Printf("app: ASR engine: %s", o.config.ASREngine)

	if o.config.ASREngine == "parakeet" {
		log.Printf("app: initializing parakeet engine...")
		exists, modelPath, vocabPath, err := transcriber.EnsureParakeetModel(modelsDir)
		if err != nil {
			log.Printf("app: parakeet model check failed: %v -- falling back to whisper", err)
			o.config.ASREngine = "whisper"
		} else if !exists {
			log.Printf("app: parakeet model not found, starting download...")
			o.ui.ShowDownloadScreen(modelsDir, "parakeet-ctc-0.6b")
			o.ui.SetCallbacks(nil, nil, nil)

			go func() {
				dlModel, dlVocab, dlErr := transcriber.DownloadParakeetModel(
					context.Background(), modelsDir,
					func(downloaded, total int64) {
						o.ui.UpdateDownloadProgress(downloaded, total)
					},
				)
				if dlErr != nil {
					log.Printf("app: parakeet download failed: %v -- falling back to whisper", dlErr)
					// Can't easily fall back mid-download. User needs to restart.
					return
				}
				o.finishInitParakeet(dlModel, dlVocab)
			}()

			o.ui.Run()
			o.shutdown()
			return
		} else {
			o.finishInitParakeet(modelPath, vocabPath)
			o.ui.Run()
			o.shutdown()
			return
		}
	}

	// Whisper model (default, or fallback from parakeet failure).
	{
		modelSize := o.config.ModelSize
		if modelSize == "" {
			modelSize = "base"
		}

		exists, modelPath, err := transcriber.EnsureModel(modelsDir, modelSize)
		if err != nil {
			log.Fatalf("app: check model: %v", err)
		}

		if !exists {
			o.ui.ShowDownloadScreen(modelsDir, modelSize)
			o.ui.SetCallbacks(nil, nil, nil)

			// Download in background so the UI can render the progress screen.
			// WHY goroutine: UI.Run() blocks on the Fyne event loop, and download
			// needs to happen concurrently. We start the download, then fall through
			// to Run(). The download goroutine calls ShowMainScreen when done.
			go func() {
				dlPath, dlErr := transcriber.DownloadModel(
					context.Background(), modelsDir, modelSize,
					func(downloaded, total int64) {
						o.ui.UpdateDownloadProgress(downloaded, total)
					},
				)
				if dlErr != nil {
					log.Fatalf("app: download model: %v", dlErr)
				}
				modelPath = dlPath
				o.finishInit(modelPath)
			}()

			o.ui.Run() // blocks
			o.shutdown()
			return
		}

		o.finishInit(modelPath)
		o.ui.Run() // blocks
		o.shutdown()
	}
}

// finishInit loads the whisper model, sets up callbacks, and switches to the main screen.
func (o *Orchestrator) finishInit(modelPath string) {
	// Set up audio directory and clean up old files.
	// WHY recorder is NOT initialized here: We need the decoder's actual sample
	// rate (e.g. 48kHz) to save pre-resampled stereo audio. The recorder is
	// created in startStreaming after the decoder reports the stream sample rate.
	appDir, err := config.AppDir()
	if err != nil {
		log.Fatalf("app: resolve app directory: %v", err)
	}
	audioDir := filepath.Join(appDir, "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		log.Fatalf("app: create audio directory: %v", err)
	}

	// Clean up audio files older than 24 hours in the background.
	go audio.CleanupOldAudio(audioDir, 24*time.Hour)

	lang := o.config.Language
	if lang == "" {
		lang = "en"
	}

	windowSize := o.config.WindowSizeSecs * whisperSampleRate
	windowStep := o.config.WindowStepSecs * whisperSampleRate

	// Determine GPU availability. The Vulkan DLL must be present next to
	// the exe AND the user must have GPU enabled in settings.
	gpuAvailable := transcriber.IsGPUAvailable()
	useGPU := o.config.UseGPU && gpuAvailable
	log.Printf("app: GPU config: requested=%v, available=%v, effective=%v", o.config.UseGPU, gpuAvailable, useGPU)

	t, err := transcriber.NewTranscriber(transcriber.TranscriberConfig{
		ModelPath:  modelPath,
		Language:   lang,
		WindowSize: windowSize,
		WindowStep: windowStep,
		UseGPU:     useGPU,
	})
	if err != nil {
		log.Fatalf("app: init transcriber: %v", err)
	}
	o.transcriber = t

	tier := o.config.ClassifierTier
	if tier == "" {
		tier = "whisper"
	}
	if tier == "whisper" || tier == "whisper+rhythm" {
		// WHY callback: The classifier package can't import the transcriber
		// package (circular dep), so we inject whisper via a callback.
		wc := classifier.NewWhisperClassifier(func(samples []float32) (string, float32, error) {
			return o.transcriber.ClassifyChunk(samples)
		})
		wc.Debug = o.config.ClassifierDebug
		if tier == "whisper+rhythm" {
			o.classifier = classifier.NewEnhancedClassifier(wc, whisperSampleRate, o.config.ClassifierDebug)
		} else {
			o.classifier = wc
		}
	} else {
		o.classifier = classifier.NewClassifier(tier, whisperSampleRate, o.config.ClassifierDebug)
	}
	log.Printf("app: classifier tier: %s (%s)", tier, o.classifier.Name())

	// Wire UI callbacks.
	o.ui.SetCallbacks(
		func() { o.startStreaming() },
		func() { o.stopStreaming() },
		func(cfg *config.Config) {
			o.config = cfg
			if saveErr := config.Save(cfg); saveErr != nil {
				log.Printf("app: save config: %v", saveErr)
			}
		},
	)

	o.ui.SetListenCallback(func(enabled bool) {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.listenWanted = enabled
		if o.listener != nil {
			o.listener.SetEnabled(enabled)
		}
	})

	o.ui.SetLoadHistoryCallback(func(limit int) ([]storage.LogEntry, error) {
		return o.db.GetRecentEntries(limit)
	})

	o.ui.SetEditSongCallback(func(id int64, title, artist string) error {
		return o.db.UpdateEntrySong(id, title, artist)
	})

	o.ui.SetInsertSongCallback(func() (*storage.LogEntry, error) {
		entry := &storage.LogEntry{
			Timestamp: time.Now(),
			EntryType: "song",
			Content:   "Song played",
		}
		if err := o.db.InsertEntry(entry); err != nil {
			return nil, err
		}
		return entry, nil
	})

	o.ui.ShowMainScreen()

	// Show GPU status warning in the live view if applicable.
	if o.config.UseGPU && !gpuAvailable {
		o.ui.ShowGPUWarning("GPU acceleration unavailable -- using CPU (slower). Install Vulkan GPU drivers for better performance.")
	} else if !o.config.UseGPU {
		o.ui.ShowGPUWarning("GPU acceleration disabled in settings -- using CPU.")
	}
}

// finishInitParakeet loads the Parakeet ONNX model and sets up callbacks.
// Mirrors finishInit but creates a ParakeetEngine instead of whisper Transcriber.
func (o *Orchestrator) finishInitParakeet(modelPath, vocabPath string) {
	appDir, err := config.AppDir()
	if err != nil {
		log.Fatalf("app: resolve app directory: %v", err)
	}
	audioDir := filepath.Join(appDir, "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		log.Fatalf("app: create audio directory: %v", err)
	}

	go audio.CleanupOldAudio(audioDir, 24*time.Hour)

	windowSize := o.config.WindowSizeSecs * whisperSampleRate
	windowStep := o.config.WindowStepSecs * whisperSampleRate

	t, err := transcriber.NewParakeetEngine(transcriber.ParakeetConfig{
		ModelPath:  modelPath,
		VocabPath:  vocabPath,
		WindowSize: windowSize,
		WindowStep: windowStep,
	})
	if err != nil {
		log.Fatalf("app: init parakeet engine: %v", err)
	}
	o.transcriber = t

	tier := o.config.ClassifierTier
	if tier == "" {
		tier = "whisper"
	}
	if tier == "whisper" || tier == "whisper+rhythm" {
		wc := classifier.NewWhisperClassifier(func(samples []float32) (string, float32, error) {
			return o.transcriber.ClassifyChunk(samples)
		})
		wc.Debug = o.config.ClassifierDebug
		if tier == "whisper+rhythm" {
			o.classifier = classifier.NewEnhancedClassifier(wc, whisperSampleRate, o.config.ClassifierDebug)
		} else {
			o.classifier = wc
		}
	} else {
		o.classifier = classifier.NewClassifier(tier, whisperSampleRate, o.config.ClassifierDebug)
	}
	log.Printf("app: classifier tier: %s (%s)", tier, o.classifier.Name())

	o.ui.SetCallbacks(
		func() { o.startStreaming() },
		func() { o.stopStreaming() },
		func(cfg *config.Config) {
			o.config = cfg
			if saveErr := config.Save(cfg); saveErr != nil {
				log.Printf("app: save config: %v", saveErr)
			}
		},
	)

	o.ui.SetListenCallback(func(enabled bool) {
		o.mu.Lock()
		defer o.mu.Unlock()
		o.listenWanted = enabled
		if o.listener != nil {
			o.listener.SetEnabled(enabled)
		}
	})

	o.ui.SetLoadHistoryCallback(func(limit int) ([]storage.LogEntry, error) {
		return o.db.GetRecentEntries(limit)
	})

	o.ui.SetEditSongCallback(func(id int64, title, artist string) error {
		return o.db.UpdateEntrySong(id, title, artist)
	})

	o.ui.SetInsertSongCallback(func() (*storage.LogEntry, error) {
		entry := &storage.LogEntry{
			Timestamp: time.Now(),
			EntryType: "song",
			Content:   "Song played",
		}
		if err := o.db.InsertEntry(entry); err != nil {
			return nil, err
		}
		return entry, nil
	})

	o.ui.ShowMainScreen()
	log.Printf("app: parakeet engine ready")
}

// startStreaming sets up the audio pipeline and begins the processing loop.
func (o *Orchestrator) startStreaming() {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.streaming {
		return
	}
	if o.config.StreamURL == "" {
		log.Printf("app: no stream URL configured")
		return
	}

	o.ctx, o.cancel = context.WithCancel(context.Background())

	// 1. Streamer -> raw MP3 bytes
	o.streamer = audio.NewStreamer(o.ctx, o.config.StreamURL)
	if err := o.streamer.Start(); err != nil {
		log.Printf("app: start streamer: %v", err)
		o.cancel()
		return
	}

	// 2. Decoder -> stereo int16 PCM (reports actual sample rate)
	o.decoder = audio.NewDecoder(o.streamer.Output())
	if err := o.decoder.Start(o.ctx); err != nil {
		log.Printf("app: start decoder: %v", err)
		o.streamer.Stop()
		o.cancel()
		return
	}

	// WHY decoder.SampleRate(): The stream could be 44100 or 48000 Hz.
	// We must pass the actual rate to the resampler, not assume 44100.
	inputRate := o.decoder.SampleRate()
	log.Printf("app: stream sample rate: %d Hz", inputRate)

	// WHY recorder init here (not in finishInit): We need the decoder's actual
	// sample rate to save pre-resampled stereo int16 audio that sounds good.
	// The old approach saved 16kHz mono float32 which was unusable for music.
	appDir, appDirErr := config.AppDir()
	if appDirErr != nil {
		log.Printf("app: resolve app directory for recorder: %v", appDirErr)
	}
	audioDir := filepath.Join(appDir, "audio")
	o.recorder = audio.NewSegmentRecorder(audioDir, inputRate, 2, o.config.SaveAudio)

	// Initialize the audio listener once. Reuse across start/stop cycles
	// because oto only allows one context per process lifetime.
	if o.listener == nil {
		listener, listenErr := audio.NewListener(inputRate, 2)
		if listenErr != nil {
			log.Printf("app: listener unavailable, listen feature disabled: %v", listenErr)
		} else {
			o.listener = listener
			// Apply the listen state the user set before streaming started.
			if o.listenWanted {
				o.listener.SetEnabled(true)
				log.Printf("app: listener auto-enabled (was requested before start)")
			}
		}
	}

	// WHY tee channel: The decoder outputs stereo int16. We need to feed those
	// samples to the listener (playback), the recorder (WAV saving), and the
	// resampler (for transcription). A goroutine fans out each chunk.
	resamplerInput := make(chan []int16, 32)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("PANIC in audio tee: %v\n%s", r, debug.Stack())
			}
		}()
		defer close(resamplerInput)
		for samples := range o.decoder.Output() {
			// Feed to listener (no-op if disabled or nil).
			if o.listener != nil {
				o.listener.Write(samples)
			}
			// Feed to recorder (no-op if disabled or no active segment).
			if o.recorder != nil {
				o.recorder.WriteInt16(samples)
			}
			// Forward to resampler.
			select {
			case resamplerInput <- samples:
			case <-o.ctx.Done():
				return
			}
		}
	}()

	// 3. Resampler -> mono float32 at 16 kHz
	o.resampler = audio.NewResampler(resamplerInput, inputRate, whisperSampleRate)
	o.resampler.Start(o.ctx)

	// Buffer for speech audio. Sized for ~60s of 16kHz mono.
	o.speechBuffer = audio.NewBuffer(whisperSampleRate * 60)

	o.streaming = true
	o.ui.UpdateStatus(true, "connected")

	go o.processingLoop()
}

// pendingChunk holds audio that was buffered during a classifier state
// transition. We store the raw classification so we can process each chunk
// according to what it actually sounded like, not what the debounce said.
type pendingChunk struct {
	samples  []float32
	rawClass classifier.Classification
}

// processingLoop reads resampled audio chunks and routes them based on
// classification. Uses a transition buffer to avoid losing audio when the
// debounce hasn't confirmed a state change yet.
//
// WHY the buffer: With debounce_chunks=5 and 2-second chunks, the classifier
// needs 10 seconds of consistent classification before switching. A 5-second
// DJ break between songs only produces 2-3 speech chunks -- not enough to
// flip the debounce. Without buffering, those speech chunks get silently
// classified as "music" and discarded. The buffer captures them and processes
// them by their raw classification once the transition resolves.
func (o *Orchestrator) processingLoop() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in processingLoop: %v\n%s", r, debug.Stack())
			o.ui.UpdateStatus(false, "crashed")
		}
	}()

	var currentState classifier.Classification = classifier.ClassSilence
	var pending []pendingChunk
	var silenceSamples int

	for chunk := range o.resampler.Output() {
		// WHY normalize for classification but not transcription: The classifier
		// thresholds are amplitude-dependent -- a quiet station vs a loud one
		// produces different feature values, causing misclassification. Whisper
		// handles varying volume fine (it normalizes internally).
		normalized := audio.NormalizeRMS(chunk, 0.1)
		result := o.classifier.Classify(normalized)

		if result.Debounced != currentState && currentState != "" {
			// State transition confirmed by debounce.
			// The pending chunks were the start of this new content --
			// process them by their raw classification so nothing is lost.
			o.handleTransition(currentState, result.Debounced)
			for _, pc := range pending {
				o.processChunkAs(pc.rawClass, pc.samples)
			}
			pending = nil
			currentState = result.Debounced
			o.processChunkAs(result.Debounced, chunk)
			silenceSamples = 0

		} else if result.Raw != result.Debounced {
			// Raw differs from debounced -- we're mid-transition.
			// Buffer the chunk with its raw classification instead of
			// routing it through the debounced (possibly wrong) state.
			pending = append(pending, pendingChunk{chunk, result.Raw})

			// Safety cap: don't buffer more than ~30 seconds of audio.
			// If we hit this, something is oscillating without committing.
			// Flush by raw class so nothing is permanently lost.
			if len(pending) > 15 {
				for _, pc := range pending {
					o.processChunkAs(pc.rawClass, pc.samples)
				}
				pending = nil
			}

		} else {
			// Stable state: Raw == Debounced == currentState.
			// If we have pending chunks from a brief blip that didn't flip
			// the debounce, process them by their raw classification.
			// This is the key path for short DJ breaks: 2-3 speech chunks
			// get buffered, music resumes, and they flush here as speech.
			if len(pending) > 0 {
				for _, pc := range pending {
					o.processChunkAs(pc.rawClass, pc.samples)
				}
				pending = nil
			}
			currentState = result.Debounced
			o.processChunkAs(result.Debounced, chunk)
		}

		// Track silence for long-silence flush.
		if result.Raw == classifier.ClassSilence {
			silenceSamples += len(chunk)
			if silenceSamples > whisperSampleRate*5 {
				o.flushSpeechBuffer()
				o.endRecorderSegment()
				o.transcriber.Reset()
				silenceSamples = 0
			}
		} else {
			silenceSamples = 0
		}

		// Show raw classification in the UI for responsiveness --
		// the user sees what the audio actually sounds like, not the
		// debounced state which lags behind.
		o.ui.UpdateStatus(true, string(result.Raw))
	}

	// Channel closed -- streamer/decoder/resampler pipeline ended.
	// Flush any remaining pending chunks.
	for _, pc := range pending {
		o.processChunkAs(pc.rawClass, pc.samples)
	}
	o.flushSpeechBuffer()
	o.endRecorderSegment()
	o.transcriber.Reset()
	o.ui.UpdateStatus(false, "disconnected")
}

// handleTransition performs cleanup when the debounce confirms a state change.
// This is where segments end/begin and the transcriber resets.
func (o *Orchestrator) handleTransition(from, to classifier.Classification) {
	o.endRecorderSegment()

	if from == classifier.ClassMusic && to == classifier.ClassSpeech {
		o.transcriber.Reset()
		// WHY NOT reset musicMarkerUp/musicDBLogged here: the whisper classifier
		// bounces music->speech->music every ~4 seconds on vocal music. Resetting
		// here would cause a new "Song played" marker on every bounce. Only reset
		// when we get actual transcribed speech (in flushSpeechBuffer) or on
		// silence (real end of segment).
	}

	if from == classifier.ClassSpeech && to == classifier.ClassMusic {
		// Flush speech buffer before transitioning (segment mode transcribes here).
		o.flushSpeechBuffer()
		o.transcriber.Reset()
		if !o.musicMarkerUp {
			// Write one DB entry and one UI marker per music segment.
			entry := &storage.LogEntry{
				Timestamp: time.Now(),
				EntryType: "music_unknown",
			}
			if dbErr := o.db.InsertEntry(entry); dbErr != nil {
				log.Printf("app: insert music entry: %v", dbErr)
			}
			o.ui.AppendMusic()
			o.musicMarkerUp = true
			o.musicDBLogged = true
		}
	}

	if from == classifier.ClassSpeech && to == classifier.ClassSilence {
		// Flush speech buffer -- the speaker stopped talking.
		o.flushSpeechBuffer()
		o.transcriber.Reset()
		// Silence is a real end of segment -- reset music flags.
		o.musicMarkerUp = false
		o.musicDBLogged = false
	}

	if from == classifier.ClassMusic && to == classifier.ClassSilence {
		o.musicMarkerUp = false
		o.musicDBLogged = false
	}
}

// processChunkAs routes a single audio chunk to the appropriate handler
// based on the given classification. This is the unified processing path
// used for both live chunks and buffered pending chunks.
func (o *Orchestrator) processChunkAs(class classifier.Classification, chunk []float32) {
	switch class {
	case classifier.ClassSpeech:
		// Start a speech segment if not already recording one.
		if _, err := o.recorder.StartSegment("speech"); err != nil {
			log.Printf("app: start speech segment: %v", err)
		}

		// WHY: Even with the whisper classifier (which produces text during
		// classification), we still accumulate audio in the speech buffer and
		// transcribe in 10-second segments. The whisper classifier's 4-second
		// fragments are too short for coherent text -- they cut mid-sentence
		// and produce garbage like "on the 40 degrees high and."
		// Discard the classifier's text; use the segment buffer for clean output.
		if wc, ok := o.classifier.(*classifier.WhisperClassifier); ok {
			wc.ConsumeText() // discard -- we'll retranscribe from the buffer
			o.speechBuffer.Write(chunk)
			if o.speechBuffer.Duration(whisperSampleRate) >= 10*time.Second {
				o.flushSpeechBuffer()
			}
		} else if o.config.TranscriptionMode == "rolling" {
			// Rolling mode: feed to rolling window, get progressive output.
			text, triggered, err := o.transcriber.FeedChunk(chunk)
			if err != nil {
				log.Printf("app: transcribe: %v", err)
			} else if triggered && text != "" {
				now := time.Now()
				entry := &storage.LogEntry{
					Timestamp: now,
					EntryType: "speech",
					Content:   text,
					AudioPath: o.recorder.CurrentPath(),
				}
				if dbErr := o.db.InsertEntry(entry); dbErr != nil {
					log.Printf("app: insert speech entry: %v", dbErr)
				}
				o.ui.AppendTranscription(now, text, entry.ID)
			}
		} else {
			// Segment mode (default): accumulate speech audio, transcribe
			// once when the segment ends (transition to music/silence).
			o.speechBuffer.Write(chunk)

			// WHY 10-second flush: For trivia, the announcer reads a question
			// during speech segments. Shorter flush intervals mean questions
			// appear faster and are more isolated in the transcript. 10 seconds
			// gives whisper enough context for accuracy while keeping latency low.
			// Also handles the case where the classifier never transitions.
			if o.speechBuffer.Duration(whisperSampleRate) >= 10*time.Second {
				o.flushSpeechBuffer()
			}
		}

	case classifier.ClassMusic:
		// Start a music segment if not already recording one.
		if _, err := o.recorder.StartSegment("music"); err != nil {
			log.Printf("app: start music segment: %v", err)
		}

	case classifier.ClassSilence:
		// Don't feed silence to transcriber or music buffer.
	}
}

// flushSpeechBuffer transcribes accumulated speech audio in one shot (segment mode).
// Produces cleaner text than rolling window -- no overlapping duplicates.
func (o *Orchestrator) flushSpeechBuffer() {
	if o.speechBuffer == nil || o.speechBuffer.Len() == 0 {
		return
	}

	samples := o.speechBuffer.ReadAll()
	if len(samples) == 0 {
		return
	}

	log.Printf("app: transcribing speech segment: %d samples (%.1fs)",
		len(samples), float64(len(samples))/float64(whisperSampleRate))

	text, err := o.transcriber.Transcribe(samples)
	if err != nil {
		log.Printf("app: transcribe segment: %v", err)
		return
	}
	if text == "" {
		return
	}

	now := time.Now()
	entry := &storage.LogEntry{
		Timestamp:  now,
		EntryType:  "speech",
		Content:    text,
		DurationMs: int64(len(samples)) * 1000 / int64(whisperSampleRate),
		AudioPath:  o.recorder.CurrentPath(),
	}
	if dbErr := o.db.InsertEntry(entry); dbErr != nil {
		log.Printf("app: insert speech entry: %v", dbErr)
	}
	o.ui.AppendTranscription(now, text, entry.ID)

	// Real speech was transcribed -- reset music flags so the next song
	// gets a fresh marker and DB entry.
	o.musicMarkerUp = false
	o.musicDBLogged = false
}

// endRecorderSegment closes the current audio segment file.
func (o *Orchestrator) endRecorderSegment() {
	if o.recorder == nil {
		return
	}
	if path, err := o.recorder.EndSegment(); err != nil {
		log.Printf("app: end audio segment: %v", err)
	} else if path != "" {
		log.Printf("app: saved audio segment: %s", path)
	}
}

// stopStreaming tears down the audio pipeline.
func (o *Orchestrator) stopStreaming() {
	o.mu.Lock()
	defer o.mu.Unlock()

	if !o.streaming {
		return
	}

	o.cancel()

	// The pipeline channels will close in order (streamer -> decoder -> resampler),
	// which causes processingLoop to exit and flush buffers.

	// WHY not Close() the listener here: oto only allows ONE context per
	// process lifetime. If we Close() and then startStreaming() tries to
	// create a new listener, oto panics with "context is already created".
	// Instead, just disable it. The listener stays alive for reuse.
	if o.listener != nil {
		o.listener.SetEnabled(false)
	}

	o.streaming = false
	o.ui.UpdateStatus(false, "stopped")
}

// shutdown cleans up all resources. Called when the UI exits.
func (o *Orchestrator) shutdown() {
	o.stopStreaming()

	if o.transcriber != nil {
		o.transcriber.Close()
	}
	// WHY listener.Close() only in shutdown, not stopStreaming:
	// oto allows only one context per process. We keep the listener alive
	// across start/stop cycles and only close it when the app exits.
	if o.listener != nil {
		o.listener.SetEnabled(false)
		o.listener.Close()
		o.listener = nil
	}
	if o.db != nil {
		if err := o.db.Close(); err != nil {
			log.Printf("app: close database: %v", err)
		}
	}
}
