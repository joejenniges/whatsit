package ui

import (
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"

	"github.com/joe/radio-transcriber/internal/config"
	"github.com/joe/radio-transcriber/internal/storage"
)

// App is the main UI application. It owns the Fyne app, window, and all
// screens. The orchestrator provides callbacks for start/stop/save actions.
type App struct {
	fyneApp fyne.App
	window  fyne.Window
	config  *config.Config

	onStart  func()
	onStop   func()
	onSave   func(*config.Config)
	onListen func(enabled bool)

	// History loading callback -- returns recent entries from the database.
	onLoadHistory func(limit int) ([]storage.LogEntry, error)

	// Song edit callback -- persists edits to song entries in the database.
	onEditSong func(id int64, title, artist string) error

	// Insert song callback -- creates a new song entry in the database.
	onInsertSong func() (*storage.LogEntry, error)

	// Screens -- only one is shown at a time.
	downloadScreen *DownloadScreen
	liveView       *LiveView
	configPage     *ConfigPage
}

// NewApp creates the Fyne application and main window. Call SetCallbacks
// before Run to wire up orchestrator actions.
func NewApp(cfg *config.Config) *App {
	fyneApp := app.NewWithID("com.joe.radiotranscriber")
	fyneApp.Settings().SetTheme(theme.DarkTheme())

	window := fyneApp.NewWindow("RadioTranscriber")
	window.Resize(fyne.NewSize(700, 500))

	return &App{
		fyneApp: fyneApp,
		window:  window,
		config:  cfg,
	}
}

// SetCallbacks wires the UI to the orchestrator. Must be called before Run.
func (a *App) SetCallbacks(onStart, onStop func(), onSave func(*config.Config)) {
	a.onStart = onStart
	a.onStop = onStop
	a.onSave = onSave
}

// SetListenCallback sets the callback for the listen toggle.
func (a *App) SetListenCallback(cb func(enabled bool)) {
	a.onListen = cb
}

// SetLoadHistoryCallback sets the callback for loading recent entries on launch.
func (a *App) SetLoadHistoryCallback(cb func(limit int) ([]storage.LogEntry, error)) {
	a.onLoadHistory = cb
}

// SetEditSongCallback sets the callback for persisting song edits.
func (a *App) SetEditSongCallback(cb func(id int64, title, artist string) error) {
	a.onEditSong = cb
}

// SetInsertSongCallback sets the callback for inserting a new song entry.
func (a *App) SetInsertSongCallback(cb func() (*storage.LogEntry, error)) {
	a.onInsertSong = cb
}

// Run shows the window and blocks on the Fyne event loop. Must be called
// from the main goroutine.
func (a *App) Run() {
	a.window.ShowAndRun()
}

// ShowDownloadScreen replaces the window content with the model download
// progress screen.
func (a *App) ShowDownloadScreen(modelsDir, modelSize string) {
	a.downloadScreen = NewDownloadScreen(modelSize)
	a.window.SetContent(a.downloadScreen.Container())
}

// ShowMainScreen replaces the window content with the live view and config
// tabs. This is the normal operating view. Safe to call from any goroutine.
func (a *App) ShowMainScreen() {
	fyne.Do(func() {
		a.liveView = NewLiveView(a.onStart, a.onStop, a.onListen)
		a.liveView.SetLoadHistoryCallback(a.onLoadHistory)
		a.liveView.SetEditSongCallback(a.onEditSong)
		a.liveView.SetInsertSongCallback(a.onInsertSong)
		a.configPage = NewConfigPage(a.config, a.onSave)

		tabs := container.NewAppTabs(
			container.NewTabItem("Live", a.liveView.Container()),
			container.NewTabItem("Settings", a.configPage.Container()),
		)
		tabs.SetTabLocation(container.TabLocationTop)

		a.window.SetContent(tabs)

		// Load history after the view is created and shown.
		go a.liveView.LoadHistory()
	})
}

// UpdateDownloadProgress forwards download progress to the download screen.
// Safe to call from any goroutine.
func (a *App) UpdateDownloadProgress(downloaded, total int64) {
	if a.downloadScreen != nil {
		a.downloadScreen.UpdateProgress(downloaded, total)
	}
}

// AppendTranscription adds a timestamped transcription line to the live view.
// Safe to call from any goroutine.
func (a *App) AppendTranscription(timestamp time.Time, text string, dbID int64) {
	if a.liveView != nil {
		a.liveView.AppendTranscription(timestamp, text, dbID)
	}
}

// AppendSong adds a song divider to the live view.
// Safe to call from any goroutine.
func (a *App) AppendSong(title, artist string, dbID int64) {
	if a.liveView != nil {
		a.liveView.AppendSong(title, artist, dbID)
	}
}

// UpdateSongLine updates the current song line without adding a new entry.
// Safe to call from any goroutine.
func (a *App) UpdateSongLine(title, artist string) {
	if a.liveView != nil {
		a.liveView.UpdateSongLine(title, artist)
	}
}

// AppendMusic adds an unknown-music divider to the live view.
// Safe to call from any goroutine.
func (a *App) AppendMusic() {
	if a.liveView != nil {
		a.liveView.AppendMusic()
	}
}

func (a *App) AppendMusicWithID(dbID int64) {
	if a.liveView != nil {
		a.liveView.AppendMusic()
	}
}

// ClearMusicMarker resets the music segment tracker in the live view.
// Safe to call from any goroutine.
func (a *App) ClearMusicMarker() {
	if a.liveView != nil {
		a.liveView.ClearMusicMarker()
	}
}

// UpdateStatus updates the status bar in the live view.
// Safe to call from any goroutine.
func (a *App) UpdateStatus(connected bool, classification string) {
	if a.liveView != nil {
		a.liveView.UpdateStatus(connected, classification)
	}
}

// UpdateLatency updates the latency indicator in the live view.
// Safe to call from any goroutine.
func (a *App) UpdateLatency(latency time.Duration) {
	if a.liveView != nil {
		a.liveView.UpdateLatency(latency)
	}
}

func (a *App) UpdateWhisperLoad(load float64) {}

// ShowGPUWarning displays a warning banner in the live view about GPU status.
// Safe to call from any goroutine.
func (a *App) ShowGPUWarning(message string) {
	if a.liveView != nil {
		a.liveView.ShowGPUWarning(message)
	}
}
