package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/joe/radio-transcriber/internal/config"
	"github.com/joe/radio-transcriber/internal/storage"
	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// WailsUI implements the app.UI interface by emitting Wails events to the
// Svelte frontend instead of rendering a native GUI. This is the bridge
// between the orchestrator (which expects a UI to push updates to) and the
// Wails WebView2 frontend.
type WailsUI struct {
	ctx  context.Context // Wails runtime context, set after startup
	done chan struct{}    // closed on shutdown to unblock Run()

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

// NewWailsUI creates a WailsUI adapter. The ctx must be set later via
// SetContext once the Wails runtime is ready (during startup).
func NewWailsUI() *WailsUI {
	return &WailsUI{
		done: make(chan struct{}),
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
	if w.ctx == nil {
		return
	}
	log.Printf("wails-ui: emitting show-download (modelSize=%s)", modelSize)
	wailsRuntime.EventsEmit(w.ctx, "show-download", map[string]interface{}{
		"modelSize": modelSize,
	})
}

func (w *WailsUI) ShowMainScreen() {
	if w.ctx == nil {
		return
	}
	log.Printf("wails-ui: emitting show-main")
	wailsRuntime.EventsEmit(w.ctx, "show-main", nil)
}

func (w *WailsUI) UpdateDownloadProgress(downloaded, total int64) {
	if w.ctx == nil {
		return
	}
	var percent float64
	var speed, eta string
	if total > 0 {
		percent = float64(downloaded) / float64(total)
	}
	speed = formatBytes(downloaded) // simplified -- real speed needs delta tracking
	eta = ""
	wailsRuntime.EventsEmit(w.ctx, "download-progress", map[string]interface{}{
		"downloaded": downloaded,
		"total":      total,
		"percent":    percent,
		"speed":      speed,
		"eta":        eta,
	})
}

func (w *WailsUI) AppendTranscription(timestamp time.Time, text string, dbID int64) {
	if w.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(w.ctx, "transcription", map[string]interface{}{
		"id":        dbID,
		"timestamp": timestamp.Format(time.RFC3339),
		"text":      text,
	})
}

func (w *WailsUI) AppendSong(title, artist string, dbID int64) {
	if w.ctx == nil {
		return
	}
	// Reset marker so the next song gets a fresh one.
	w.mu.Lock()
	w.musicMarkerShown = false
	w.mu.Unlock()

	wailsRuntime.EventsEmit(w.ctx, "song-identified", map[string]interface{}{
		"id":     dbID,
		"title":  title,
		"artist": artist,
	})
}

func (w *WailsUI) UpdateSongLine(title, artist string) {
	if w.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(w.ctx, "song-updated", map[string]interface{}{
		"title":  title,
		"artist": artist,
	})
}

func (w *WailsUI) AppendMusic() {
	if w.ctx == nil {
		return
	}
	w.mu.Lock()
	if w.musicMarkerShown {
		w.mu.Unlock()
		return
	}
	w.musicMarkerShown = true
	w.mu.Unlock()

	wailsRuntime.EventsEmit(w.ctx, "music-detected", map[string]interface{}{})
}

func (w *WailsUI) ClearMusicMarker() {
	if w.ctx == nil {
		return
	}
	w.mu.Lock()
	w.musicMarkerShown = false
	w.mu.Unlock()
	wailsRuntime.EventsEmit(w.ctx, "music-cleared", map[string]interface{}{})
}

func (w *WailsUI) UpdateStatus(connected bool, classification string) {
	if w.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(w.ctx, "status", map[string]interface{}{
		"connected":      connected,
		"classification": classification,
	})
}

func (w *WailsUI) UpdateLatency(latency time.Duration) {
	if w.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(w.ctx, "latency", map[string]interface{}{
		"ms": latency.Milliseconds(),
	})
}

func (w *WailsUI) ShowGPUWarning(message string) {
	if w.ctx == nil {
		return
	}
	wailsRuntime.EventsEmit(w.ctx, "gpu-warning", message)
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
