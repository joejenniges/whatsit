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
	"github.com/joe/radio-transcriber/internal/musicid"
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
	transcriber *transcriber.Transcriber

	// WHY optional: musicid requires libchromaprint and an AcoustID API key.
	// If either is unavailable, we still run -- just skip song identification.
	fingerprinter  *musicid.Fingerprinter
	acoustidClient *musicid.AcoustIDClient

	// WHY: Listener plays decoded stereo PCM through speakers when the user
	// toggles "Listen" on. It sits between decoder and resampler in the
	// pipeline -- pre-resampling so the user hears full-quality stereo audio.
	listener *audio.Listener

	musicBuffer    *audio.Buffer
	rawMusicBuffer []int16 // original stereo int16 for Chromaprint fingerprinting
	rawMusicRate   int     // sample rate of raw audio
	rawMusicMu     sync.Mutex
	musicSamples   int  // total music samples accumulated (not reset on bounce)
	recorder       *audio.SegmentRecorder

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

	// Check whisper model.
	modelsDir := filepath.Join(appDir, "models")
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

	// Try to init fingerprinter. Non-fatal if it fails (chromaprint may not be installed).
	fp, fpErr := musicid.NewFingerprinter()
	if fpErr != nil {
		log.Printf("app: fingerprinter unavailable, music ID disabled: %v", fpErr)
	} else {
		o.fingerprinter = fp
	}

	// Init AcoustID client if API key is configured.
	if o.config.AcoustIDKey != "" {
		client, clientErr := musicid.NewAcoustIDClient(o.config.AcoustIDKey)
		if clientErr != nil {
			log.Printf("app: acoustid client init failed: %v", clientErr)
		} else {
			o.acoustidClient = client
			log.Printf("app: acoustid client initialized")
		}
	}

	tier := o.config.ClassifierTier
	if tier == "" {
		tier = "scheirer"
	}
	if tier == "whisper" {
		// WHY callback: The classifier package can't import the transcriber
		// package (circular dep), so we inject whisper via a callback.
		wc := classifier.NewWhisperClassifier(func(samples []float32) (string, float32, error) {
			return o.transcriber.ClassifyChunk(samples)
		})
		wc.Debug = o.config.ClassifierDebug
		o.classifier = wc
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
	o.rawMusicRate = inputRate

	// Initialize the audio listener for speaker output. Non-fatal if it fails
	// (e.g., no audio output device). Starts disabled -- user toggles it on.
	listener, listenErr := audio.NewListener(inputRate, 2)
	if listenErr != nil {
		log.Printf("app: listener unavailable, listen feature disabled: %v", listenErr)
	} else {
		o.listener = listener
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
			// Buffer raw stereo int16 for Chromaprint fingerprinting.
			// WHY here: Chromaprint needs the original pre-resampled audio
			// (48kHz stereo) to produce valid fingerprints. AcoustID was
			// rejecting fingerprints from 16kHz mono as "invalid fingerprint".
			o.rawMusicMu.Lock()
			o.rawMusicBuffer = append(o.rawMusicBuffer, samples...)
			// Cap at ~60 seconds of stereo audio at the stream rate.
			maxRaw := o.rawMusicRate * 2 * 60
			if len(o.rawMusicBuffer) > maxRaw {
				o.rawMusicBuffer = o.rawMusicBuffer[len(o.rawMusicBuffer)-maxRaw:]
			}
			o.rawMusicMu.Unlock()
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

	// Music buffer sized for ~60s of 16 kHz audio. Speech goes directly
	// to the transcriber's rolling window via FeedChunk.
	o.musicBuffer = audio.NewBuffer(whisperSampleRate * 60)

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
				o.endRecorderSegment()
				o.transcriber.Reset()
				o.flushMusicBuffer()
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
	o.endRecorderSegment()
	o.transcriber.Reset()
	o.flushMusicBuffer()
	o.ui.UpdateStatus(false, "disconnected")
}

// handleTransition performs cleanup when the debounce confirms a state change.
// This is where segments end/begin and the transcriber resets.
func (o *Orchestrator) handleTransition(from, to classifier.Classification) {
	o.endRecorderSegment()

	if from == classifier.ClassMusic && to == classifier.ClassSpeech {
		// WHY no flushMusicBuffer here: On bouncy stations, music->speech
		// transitions happen frequently. Flushing the music buffer each time
		// means we never accumulate enough audio for fingerprinting. The
		// identifyMusic call is triggered by musicSamples counter instead.
		// The raw audio buffer keeps accumulating in the tee goroutine.
		o.transcriber.Reset()
		o.ui.ClearMusicMarker()
	}

	if from == classifier.ClassSpeech && to == classifier.ClassMusic {
		o.transcriber.Reset()
		o.ui.AppendMusic()
	}

	if from == classifier.ClassSpeech && to == classifier.ClassSilence {
		o.transcriber.Reset()
	}

	if from == classifier.ClassMusic && to == classifier.ClassSilence {
		o.flushMusicBuffer()
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

		// WHY no Write call here: The tee goroutine feeds pre-resampled
		// stereo int16 samples to the recorder via WriteInt16. This loop
		// only manages segment start/end transitions based on classification.

		// WHY two paths: The whisper classifier already ran inference
		// to classify the audio, so it has the transcription text.
		// Re-transcribing through the rolling window would waste CPU.
		// Non-whisper classifiers use DSP features, so they still need
		// the rolling window for transcription.
		if wc, ok := o.classifier.(*classifier.WhisperClassifier); ok {
			if text := wc.LastText(); text != "" {
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
		}

	case classifier.ClassMusic:
		// Start a music segment if not already recording one.
		if _, err := o.recorder.StartSegment("music"); err != nil {
			log.Printf("app: start music segment: %v", err)
		}

		o.musicBuffer.Write(chunk)
		o.musicSamples += len(chunk)

		// WHY musicSamples instead of musicBuffer.Duration(): When the
		// classifier bounces (music->speech->music), handleTransition
		// flushes the music buffer each time. With musicSamples we track
		// total music audio seen across bounces, so fingerprinting still
		// triggers even if the buffer keeps getting drained.
		if o.musicSamples >= whisperSampleRate*15 {
			o.musicSamples = 0
			go o.identifyMusic()
		}

	case classifier.ClassSilence:
		// Don't feed silence to transcriber or music buffer.
	}
}

// flushMusicBuffer identifies whatever is in the music buffer.
func (o *Orchestrator) flushMusicBuffer() {
	if o.musicBuffer == nil || o.musicBuffer.Len() == 0 {
		return
	}
	go o.identifyMusic()
}

// identifyMusic drains the music buffer, fingerprints it, and tries to
// identify the song via AcoustID. If identified, replaces the UI's
// "Music playing" placeholder with the song name.
func (o *Orchestrator) identifyMusic() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in identifyMusic: %v\n%s", r, debug.Stack())
		}
	}()

	samples := o.musicBuffer.ReadAll()
	if len(samples) == 0 {
		return
	}

	// Grab the raw stereo int16 audio for fingerprinting.
	o.rawMusicMu.Lock()
	rawSamples := make([]int16, len(o.rawMusicBuffer))
	copy(rawSamples, o.rawMusicBuffer)
	o.rawMusicBuffer = o.rawMusicBuffer[:0]
	o.rawMusicMu.Unlock()

	durationMs := int64(len(samples)) * 1000 / int64(whisperSampleRate)

	log.Printf("app: identifyMusic: %d resampled samples, %d raw stereo samples", len(samples), len(rawSamples))

	// Try fingerprinting + AcoustID lookup if available.
	if o.fingerprinter == nil {
		log.Printf("app: identifyMusic: fingerprinter not available, skipping")
	} else if o.acoustidClient == nil {
		log.Printf("app: identifyMusic: no AcoustID API key configured, skipping")
	} else if len(rawSamples) == 0 {
		log.Printf("app: identifyMusic: no raw audio available for fingerprinting")
	}
	if o.fingerprinter != nil && o.acoustidClient != nil && len(rawSamples) > 0 {
		log.Printf("app: fingerprinting %d raw samples at %dHz stereo (%ds)", len(rawSamples), o.rawMusicRate, len(rawSamples)/o.rawMusicRate/2)
		fp, dur, fpErr := o.fingerprinter.FingerprintInt16(rawSamples, o.rawMusicRate, 2)
		if fpErr != nil {
			log.Printf("app: fingerprint error: %v", fpErr)
		} else {
			log.Printf("app: fingerprint OK, duration=%ds, fp_len=%d, sending to AcoustID", dur, len(fp))
			song, lookupErr := o.acoustidClient.Identify(fp, dur)
			if lookupErr != nil {
				log.Printf("app: acoustid lookup error: %v", lookupErr)
			} else if song == nil {
				log.Printf("app: acoustid: no match found")
			}
			if song != nil {
				log.Printf("app: identified song: %q by %s (score %.2f)", song.Title, song.Artist, song.Score)
				entry := &storage.LogEntry{
					Timestamp:  time.Now(),
					EntryType:  "song",
					Content:    song.Title + " - " + song.Artist,
					Title:      song.Title,
					Artist:     song.Artist,
					Album:      song.Album,
					Confidence: song.Score,
					DurationMs: durationMs,
					AudioPath:  o.recorder.CurrentPath(),
				}
				if err := o.db.InsertEntry(entry); err != nil {
					log.Printf("app: insert song entry: %v", err)
				}
				// Replace the "Music playing" placeholder with the actual song.
				o.ui.AppendSong(song.Title, song.Artist, entry.ID)
				return
			}
		}
	}

	// No song identified -- just log it. The UI already shows "Music playing"
	// from the transition, so don't add another marker.
	entry := &storage.LogEntry{
		Timestamp:  time.Now(),
		EntryType:  "music_unknown",
		DurationMs: durationMs,
		AudioPath:  o.recorder.CurrentPath(),
	}
	if err := o.db.InsertEntry(entry); err != nil {
		log.Printf("app: insert music entry: %v", err)
	}
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

	if o.listener != nil {
		o.listener.SetEnabled(false)
		o.listener.Close()
		o.listener = nil
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
	if o.listener != nil {
		o.listener.Close()
	}
	if o.fingerprinter != nil {
		o.fingerprinter.Close()
	}
	if o.db != nil {
		if err := o.db.Close(); err != nil {
			log.Printf("app: close database: %v", err)
		}
	}
}
