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
	SetSearchCallback(func(query string, limit int) ([]storage.LogEntry, error))
	ShowDownloadScreen(modelsDir, modelSize string)
	ShowMainScreen()
	UpdateDownloadProgress(downloaded, total int64)
	AppendTranscription(timestamp time.Time, text string)
	AppendSong(title, artist string)
	AppendMusic()
	ClearMusicMarker()
	UpdateStatus(connected bool, classification string)
	UpdateLatency(latency time.Duration)
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

	musicBuffer *audio.Buffer
	recorder    *audio.SegmentRecorder

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
	// Initialize audio segment recorder and clean up old files.
	appDir, err := config.AppDir()
	if err != nil {
		log.Fatalf("app: resolve app directory: %v", err)
	}
	audioDir := filepath.Join(appDir, "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		log.Fatalf("app: create audio directory: %v", err)
	}
	o.recorder = audio.NewSegmentRecorder(audioDir, whisperSampleRate)

	// Clean up audio files older than 24 hours in the background.
	go audio.CleanupOldAudio(audioDir, 24*time.Hour)

	lang := o.config.Language
	if lang == "" {
		lang = "en"
	}

	windowSize := o.config.WindowSizeSecs * whisperSampleRate
	windowStep := o.config.WindowStepSecs * whisperSampleRate

	t, err := transcriber.NewTranscriber(transcriber.TranscriberConfig{
		ModelPath:  modelPath,
		Language:   lang,
		WindowSize: windowSize,
		WindowStep: windowStep,
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

	o.ui.SetSearchCallback(func(query string, limit int) ([]storage.LogEntry, error) {
		return o.db.SearchEntries(query, limit)
	})

	o.ui.ShowMainScreen()
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

	// Initialize the audio listener for speaker output. Non-fatal if it fails
	// (e.g., no audio output device). Starts disabled -- user toggles it on.
	listener, listenErr := audio.NewListener(inputRate, 2)
	if listenErr != nil {
		log.Printf("app: listener unavailable, listen feature disabled: %v", listenErr)
	} else {
		o.listener = listener
	}

	// WHY tee channel: The decoder outputs stereo int16. We need to feed those
	// samples to both the resampler (for transcription) and the listener (for
	// playback). A simple goroutine copies each chunk to the listener and
	// forwards it to the resampler's input channel.
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

// processingLoop reads resampled audio chunks and routes them based on classification.
func (o *Orchestrator) processingLoop() {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("PANIC in processingLoop: %v\n%s", r, debug.Stack())
			o.ui.UpdateStatus(false, "crashed")
		}
	}()

	var lastClass classifier.Classification
	var silenceSamples int

	for chunk := range o.resampler.Output() {
		class := o.classifier.Classify(chunk)

		switch class {
		case classifier.ClassSpeech:
			// Transitioning from music -> speech: flush music buffer,
			// end the music segment, and reset the transcriber.
			if lastClass == classifier.ClassMusic {
				o.endRecorderSegment()
				o.flushMusicBuffer()
				o.transcriber.Reset()
				o.ui.ClearMusicMarker()
			}

			// Start a speech segment if not already recording one.
			if _, err := o.recorder.StartSegment("speech"); err != nil {
				log.Printf("app: start speech segment: %v", err)
			}
			silenceSamples = 0

			// Record audio to WAV.
			if err := o.recorder.Write(chunk); err != nil {
				log.Printf("app: write speech audio: %v", err)
			}

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
					o.ui.AppendTranscription(now, text)
				}
			} else {
				// Non-whisper classifier: feed to rolling window as before.
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
					o.ui.AppendTranscription(now, text)
				}
			}

		case classifier.ClassMusic:
			// Transitioning from speech -> music: end the speech segment,
			// reset transcriber window, show "Music playing" marker.
			if lastClass == classifier.ClassSpeech {
				o.endRecorderSegment()
				o.transcriber.Reset()
				o.ui.AppendMusic() // Shows once; subsequent calls are no-ops.
			}

			// Start a music segment if not already recording one.
			if _, err := o.recorder.StartSegment("music"); err != nil {
				log.Printf("app: start music segment: %v", err)
			}
			silenceSamples = 0

			// Record audio to WAV.
			if err := o.recorder.Write(chunk); err != nil {
				log.Printf("app: write music audio: %v", err)
			}

			o.musicBuffer.Write(chunk)

			if o.musicBuffer.Duration(whisperSampleRate) >= 15*time.Second {
				go o.identifyMusic()
			}

		case classifier.ClassSilence:
			silenceSamples += len(chunk)
			// Flush on long silence (>5s).
			if silenceSamples > whisperSampleRate*5 {
				o.endRecorderSegment()
				o.transcriber.Reset()
				o.flushMusicBuffer()
				silenceSamples = 0
			}
		}

		if class != classifier.ClassSilence {
			lastClass = class
		}

		o.ui.UpdateStatus(true, string(class))
	}

	// Channel closed -- streamer/decoder/resampler pipeline ended.
	o.endRecorderSegment()
	o.transcriber.Reset()
	o.flushMusicBuffer()
	o.ui.UpdateStatus(false, "disconnected")
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

	durationMs := int64(len(samples)) * 1000 / int64(whisperSampleRate)

	// Try fingerprinting + AcoustID lookup if available.
	if o.fingerprinter != nil && o.acoustidClient != nil {
		fp, dur, fpErr := o.fingerprinter.Fingerprint(samples, whisperSampleRate)
		if fpErr != nil {
			log.Printf("app: fingerprint: %v", fpErr)
		} else {
			song, lookupErr := o.acoustidClient.Identify(fp, dur)
			if lookupErr != nil {
				log.Printf("app: acoustid lookup: %v", lookupErr)
			} else if song != nil {
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
				o.ui.AppendSong(song.Title, song.Artist)
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
