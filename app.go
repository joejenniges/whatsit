package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/joe/radio-transcriber/internal/app"
	"github.com/joe/radio-transcriber/internal/config"
	"github.com/joe/radio-transcriber/internal/storage"
	"github.com/joe/radio-transcriber/internal/transcriber"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// ModelStatus is returned by GetModelStatus to tell the frontend whether
// the whisper model is downloaded and ready.
type ModelStatus struct {
	Exists bool   `json:"exists"`
	Path   string `json:"path"`
	Size   string `json:"size"`
}

// App struct holds the Wails application context and backend references.
type App struct {
	ctx          context.Context
	config       *config.Config
	db           *storage.Database
	orchestrator *app.Orchestrator
	wailsUI      *WailsUI
	state        *AppState
}

// NewApp creates a new App instance.
func NewApp() *App {
	state := NewAppState()
	return &App{
		state: state,
	}
}

// startup is called when the Wails app starts. The context is saved so we
// can call Wails runtime functions (EventsEmit, etc.) from Go.
//
// WHY goroutine for orchestrator.Start(): The orchestrator's Start() method
// blocks on UI.Run() (which for WailsUI blocks on a channel). We must return
// from startup() promptly so Wails can finish initializing the window and
// WebView. The orchestrator runs in the background and pushes events to the
// frontend via WailsUI.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfg, err := config.Load()
	if err != nil {
		log.Printf("Failed to load config: %v", err)
		return
	}
	a.config = cfg

	// Open database.
	appDir, err := config.AppDir()
	if err != nil {
		log.Printf("Failed to resolve app directory: %v", err)
		return
	}
	db, err := storage.New(filepath.Join(appDir, "transcripts.db"))
	if err != nil {
		log.Printf("Failed to open database: %v", err)
		return
	}
	a.db = db

	// Load recent entries from DB into AppState so the frontend has history on launch.
	if recent, err := db.GetRecentEntries(200); err == nil && len(recent) > 0 {
		// GetRecentEntries returns newest-first, reverse for chronological order.
		for i, j := 0, len(recent)-1; i < j; i, j = i+1, j-1 {
			recent[i], recent[j] = recent[j], recent[i]
		}
		for _, le := range recent {
			a.state.AddEntrySilent(UIEntry{
				ID:        le.ID,
				Timestamp: le.Timestamp.Format(time.RFC3339),
				Type:      le.EntryType,
				Content:   le.Content,
				Title:     le.Title,
				Artist:    le.Artist,
			})
		}
		log.Printf("app: loaded %d entries from DB into state", len(recent))
	}

	// Create WailsUI adapter with the shared AppState and set the runtime context.
	a.state.SetContext(ctx)
	a.wailsUI = NewWailsUI(a.state)
	a.wailsUI.SetContext(ctx)

	// Set the classifier tier on the state so the frontend can display it.
	tier := a.config.ClassifierTier
	if tier == "" {
		tier = "whisper+rhythm"
	}
	a.state.SetClassifierTier(tier)

	// Create orchestrator with the WailsUI adapter.
	a.orchestrator = app.NewOrchestrator(a.config, a.wailsUI)

	// Start the orchestrator in a goroutine. It will call WailsUI.Run()
	// which blocks until Close() is called from shutdown().
	go a.orchestrator.Start()
}

// shutdown is called when the Wails app is closing.
func (a *App) shutdown(ctx context.Context) {
	// Save window position/size for next launch.
	// WHY recover: WindowGetPosition/Size may panic during shutdown
	// if the window is already being destroyed.
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("save window state: recovered from panic: %v", r)
			}
		}()
		x, y := wailsRuntime.WindowGetPosition(a.ctx)
		w, h := wailsRuntime.WindowGetSize(a.ctx)
		if w > 0 && h > 0 {
			SaveWindowState(WindowState{X: x, Y: y, Width: w, Height: h})
			log.Printf("app: saved window state: %dx%d at (%d,%d)", w, h, x, y)
		}
	}()

	// Unblock the orchestrator's Start() -> UI.Run() -> <-done channel.
	if a.wailsUI != nil {
		a.wailsUI.Close()
	}

	time.Sleep(1 * time.Second)
}

// --- State binding ---

// GetInitialState returns a snapshot of all UI-relevant state.
// Called ONCE when the frontend first loads to catch up on any state
// that was set before the frontend was ready.
func (a *App) GetInitialState() map[string]interface{} {
	return a.state.Snapshot()
}

// --- Config bindings ---

// GetConfig returns the current configuration.
func (a *App) GetConfig() *config.Config {
	return a.config
}

// SaveConfig persists a new configuration to disk and updates the in-memory
// config. The orchestrator's onSave callback is invoked if set.
func (a *App) SaveConfig(cfg config.Config) error {
	// Apply runtime-changeable settings immediately.
	if a.orchestrator != nil && cfg.SaveAudio != a.config.SaveAudio {
		a.orchestrator.SetSaveAudio(cfg.SaveAudio)
	}

	if a.wailsUI != nil && a.wailsUI.onSave != nil {
		a.wailsUI.onSave(&cfg)
	} else {
		a.config = &cfg
		if err := config.Save(&cfg); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	return nil
}

// --- Database query bindings ---

// GetRecentEntries returns the last N entries from the database.
func (a *App) GetRecentEntries(limit int) ([]storage.LogEntry, error) {
	if a.db == nil {
		return nil, nil
	}
	return a.db.GetRecentEntries(limit)
}

// SearchEntries searches entries by content, returning up to limit results.
func (a *App) SearchEntries(query string, limit int) ([]storage.LogEntry, error) {
	if a.db == nil {
		return nil, nil
	}
	return a.db.SearchEntries(query, limit)
}

// UpdateEntryContent updates the text content of an existing entry.
func (a *App) UpdateEntryContent(id int64, content string) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	return a.db.UpdateEntryContent(id, content)
}

// UpdateEntrySong updates the title and artist of a song entry.
func (a *App) UpdateEntrySong(id int64, title, artist string) error {
	if a.wailsUI != nil && a.wailsUI.editSong != nil {
		return a.wailsUI.editSong(id, title, artist)
	}
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	return a.db.UpdateEntrySong(id, title, artist)
}

// InsertSongEntry creates a new "Song played" entry via the orchestrator's
// insert callback (which handles DB insertion and returns the entry).
func (a *App) InsertSongEntry() (*storage.LogEntry, error) {
	if a.wailsUI != nil && a.wailsUI.insertSong != nil {
		return a.wailsUI.insertSong()
	}
	if a.db == nil {
		return nil, fmt.Errorf("database not initialized")
	}
	entry := &storage.LogEntry{
		Timestamp: time.Now(),
		EntryType: "song",
		Content:   "Song played",
	}
	if err := a.db.InsertEntry(entry); err != nil {
		return nil, err
	}
	return entry, nil
}

// DeleteEntry removes an entry from the database by ID.
func (a *App) DeleteEntry(id int64) error {
	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}
	return a.db.DeleteEntry(id)
}

// OpenLogsFolder opens the logs directory in the OS file manager.
func (a *App) OpenLogsFolder() error {
	appDir, err := config.AppDir()
	if err != nil {
		return err
	}
	logsDir := filepath.Join(appDir, "logs")
	os.MkdirAll(logsDir, 0o755)
	return exec.Command("explorer", logsDir).Start()
}

// OpenDataFolder opens the app data directory in the OS file manager.
func (a *App) OpenDataFolder() error {
	appDir, err := config.AppDir()
	if err != nil {
		return err
	}
	return exec.Command("explorer", appDir).Start()
}

// --- Streaming control bindings ---

// StartStreaming tells the orchestrator to connect to the stream and begin
// processing audio. The orchestrator's onStart callback handles the actual
// pipeline setup.
func (a *App) StartStreaming() error {
	if a.wailsUI == nil {
		return fmt.Errorf("not initialized")
	}
	if a.wailsUI.onStart == nil {
		return fmt.Errorf("orchestrator not ready")
	}
	a.wailsUI.onStart()
	return nil
}

// StopStreaming tells the orchestrator to disconnect from the stream.
func (a *App) StopStreaming() {
	if a.wailsUI != nil && a.wailsUI.onStop != nil {
		a.wailsUI.onStop()
	}
}

// SetListenEnabled toggles audio playback through speakers.
func (a *App) SetListenEnabled(enabled bool) {
	if a.wailsUI != nil && a.wailsUI.onListen != nil {
		a.wailsUI.onListen(enabled)
	}
}

// --- System info bindings ---

// IsGPUAvailable checks whether Vulkan GPU acceleration is available.
func (a *App) IsGPUAvailable() bool {
	return transcriber.IsGPUAvailable()
}

// GetModelStatus returns information about whether the whisper model exists,
// its path, and formatted file size.
func (a *App) GetModelStatus() ModelStatus {
	appDir, err := config.AppDir()
	if err != nil {
		return ModelStatus{}
	}
	modelsDir := filepath.Join(appDir, "models")

	// Check the appropriate model based on ASR engine setting.
	if a.config != nil && a.config.ASREngine == "parakeet" {
		exists, modelPath, _, err := transcriber.EnsureParakeetModel(modelsDir)
		if err != nil {
			return ModelStatus{Path: modelPath}
		}
		status := ModelStatus{Exists: exists, Path: modelPath}
		if exists {
			if info, err := os.Stat(modelPath); err == nil {
				status.Size = formatBytes(info.Size())
			}
		}
		return status
	}

	modelSize := "base"
	if a.config != nil && a.config.ModelSize != "" {
		modelSize = a.config.ModelSize
	}

	exists, modelPath, err := transcriber.EnsureModel(modelsDir, modelSize)
	if err != nil {
		return ModelStatus{Path: modelPath}
	}

	status := ModelStatus{
		Exists: exists,
		Path:   modelPath,
	}

	if exists {
		if info, err := os.Stat(modelPath); err == nil {
			status.Size = formatBytes(info.Size())
		}
	}

	return status
}

// DownloadModel downloads the whisper model in the background, emitting
// progress events to the frontend. This is used when the frontend's download
// screen is shown manually (e.g., model deleted or size changed).
//
// Note: The orchestrator's Start() already handles the download-on-first-run
// flow. This binding is for explicit user-triggered downloads.
func (a *App) DownloadModel() error {
	appDir, err := config.AppDir()
	if err != nil {
		return fmt.Errorf("resolve app directory: %w", err)
	}
	modelsDir := filepath.Join(appDir, "models")

	modelSize := "base"
	if a.config != nil && a.config.ModelSize != "" {
		modelSize = a.config.ModelSize
	}

	go func() {
		_, dlErr := transcriber.DownloadModel(
			context.Background(), modelsDir, modelSize,
			func(downloaded, total int64) {
				if a.wailsUI != nil {
					a.wailsUI.UpdateDownloadProgress(downloaded, total)
				}
			},
		)
		if dlErr != nil {
			log.Printf("download model: %v", dlErr)
			if a.ctx != nil {
				// Emit error to frontend.
				a.wailsUI.UpdateDownloadProgress(-1, 0)
			}
			return
		}
		// Signal download complete.
		if a.wailsUI != nil {
			a.wailsUI.UpdateDownloadProgress(1, 1)
		}
	}()

	return nil
}
