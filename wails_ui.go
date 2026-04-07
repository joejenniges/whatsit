package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/joe/radio-transcriber/internal/config"
	"github.com/joe/radio-transcriber/internal/storage"
)

// WailsUI implements the app.UI interface by delegating state changes to
// AppState, which owns the data and emits Wails events. This is the bridge
// between the orchestrator (which expects a UI to push updates to) and the
// centralized state management layer. See wails.md for the architecture.
type WailsUI struct {
	ctx   context.Context // Wails runtime context, set after startup
	state *AppState       // central state -- all mutations go through here
	done  chan struct{}    // closed on shutdown to unblock Run()

	mu               sync.Mutex
	musicMarkerShown bool // dedup: only emit one music-detected per segment

	// Callbacks set by the orchestrator via Set*Callback methods.
	onStart    func()
	onStop     func()
	onSave     func(*config.Config)
	onListen   func(enabled bool)
	loadHist   func(limit int) ([]storage.LogEntry, error)
	editSong   func(id int64, title, artist string) error
	insertSong func() (*storage.LogEntry, error)
}

// NewWailsUI creates a WailsUI adapter. The state and ctx must be set later
// via SetContext once the Wails runtime is ready (during startup).
func NewWailsUI(state *AppState) *WailsUI {
	return &WailsUI{
		state: state,
		done:  make(chan struct{}),
	}
}

// SetContext sets the Wails runtime context. Must be called from app.startup
// before the orchestrator emits any events.
func (w *WailsUI) SetContext(ctx context.Context) {
	w.ctx = ctx
}

// Close signals Run() to unblock. Called from app.shutdown.
func (w *WailsUI) Close() {
	select {
	case <-w.done:
		// already closed
	default:
		close(w.done)
	}
}

// --- app.UI interface implementation ---

func (w *WailsUI) SetCallbacks(onStart, onStop func(), onSave func(*config.Config)) {
	w.onStart = onStart
	w.onStop = onStop
	w.onSave = onSave
}

func (w *WailsUI) SetListenCallback(cb func(enabled bool)) {
	w.onListen = cb
}

func (w *WailsUI) SetLoadHistoryCallback(cb func(limit int) ([]storage.LogEntry, error)) {
	w.loadHist = cb
}

func (w *WailsUI) SetEditSongCallback(cb func(id int64, title, artist string) error) {
	w.editSong = cb
}

func (w *WailsUI) SetInsertSongCallback(cb func() (*storage.LogEntry, error)) {
	w.insertSong = cb
}

func (w *WailsUI) ShowDownloadScreen(modelsDir, modelSize string) {
	log.Printf("wails-ui: download screen (modelSize=%s)", modelSize)
	w.state.SetDownloadProgress(0, "Starting download...")
}

func (w *WailsUI) ShowMainScreen() {
	log.Printf("wails-ui: main screen")
	w.state.SetDownloadComplete()
}

func (w *WailsUI) UpdateDownloadProgress(downloaded, total int64) {
	var percent float64
	var message string
	if total > 0 {
		percent = float64(downloaded) / float64(total) * 100
		message = fmt.Sprintf("%s / %s", formatBytes(downloaded), formatBytes(total))
	}
	w.state.SetDownloadProgress(percent, message)
}

func (w *WailsUI) AppendTranscription(timestamp time.Time, text string, dbID int64) {
	w.state.AddEntry(UIEntry{
		ID:        dbID,
		Timestamp: timestamp.Format(time.RFC3339),
		Type:      "speech",
		Content:   text,
	})
}

func (w *WailsUI) AppendSong(title, artist string, dbID int64) {
	// Reset marker so the next music segment gets a fresh one.
	w.mu.Lock()
	w.musicMarkerShown = false
	w.mu.Unlock()

	// Update the existing music_unknown entry to a fully identified song.
	// WHY UpdateEntry: when music starts, AppendMusic creates a music_unknown
	// placeholder. Once identification succeeds, we update that entry rather
	// than adding a duplicate.
	w.state.UpdateEntry(dbID, map[string]interface{}{
		"type":    "song",
		"title":   title,
		"artist":  artist,
		"content": fmt.Sprintf("\"%s\" - %s", title, artist),
	})
}

func (w *WailsUI) UpdateSongLine(title, artist string) {
	// This updates the most recent song entry in the UI.
	// WHY not used currently: with AcoustID removed, song identification is
	// done once in AppendSong. Keeping the method to satisfy the UI interface.
}

func (w *WailsUI) AppendMusic() {
	w.mu.Lock()
	if w.musicMarkerShown {
		w.mu.Unlock()
		return
	}
	w.musicMarkerShown = true
	w.mu.Unlock()

	w.state.AddEntry(UIEntry{
		ID:        0, // no DB id yet for music markers
		Timestamp: time.Now().Format(time.RFC3339),
		Type:      "music_unknown",
		Content:   "Song played",
	})
}

func (w *WailsUI) ClearMusicMarker() {
	w.mu.Lock()
	w.musicMarkerShown = false
	w.mu.Unlock()
}

func (w *WailsUI) UpdateStatus(connected bool, classification string) {
	w.state.SetConnected(connected)
	w.state.SetClassification(classification)
}

func (w *WailsUI) UpdateLatency(latency time.Duration) {
	// Latency is informational only, not currently displayed in the new UI.
	// Could add to AppState if needed later.
}

func (w *WailsUI) ShowGPUWarning(message string) {
	w.state.SetGPUWarning(message)
}

// Run blocks until Close() is called. The orchestrator's Start() method
// calls this expecting it to block like a GUI event loop. With Wails, the
// event loop is handled by the Wails runtime, so we just block on a channel
// until shutdown.
func (w *WailsUI) Run() {
	<-w.done
}

// --- Helpers ---

func formatBytes(b int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case b >= gb:
		return fmt.Sprintf("%.1f GB", float64(b)/float64(gb))
	case b >= mb:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(mb))
	case b >= kb:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(kb))
	default:
		return fmt.Sprintf("%d B", b)
	}
}
