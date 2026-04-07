package main

import (
	"context"
	"sync"
	"time"

	wailsRuntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// AppState is the single source of truth for all UI-relevant state.
// Go owns this; the frontend gets a snapshot on init and incremental
// updates via events. See wails.md for the architecture.
type AppState struct {
	mu  sync.RWMutex
	ctx context.Context

	// Connection
	Connected      bool   `json:"connected"`
	Classification string `json:"classification"`

	// Entries (last 200 for display)
	Entries []UIEntry `json:"entries"`

	// Download
	Downloading     bool    `json:"downloading"`
	DownloadPercent float64 `json:"downloadPercent"`
	DownloadMessage string  `json:"downloadMessage"`

	// Warnings
	GPUWarning string `json:"gpuWarning"`
}

// UIEntry is the frontend-facing entry representation.
// WHY separate from storage.LogEntry: decouples the DB schema from the
// wire format. We can add fields like Fresh without touching storage.
type UIEntry struct {
	ID        int64  `json:"id"`
	Timestamp string `json:"timestamp"` // RFC3339
	Type      string `json:"type"`      // speech, song, music_unknown
	Content   string `json:"content"`
	Title     string `json:"title,omitempty"`
	Artist    string `json:"artist,omitempty"`
	Fresh     bool   `json:"fresh,omitempty"`
}

func NewAppState() *AppState {
	return &AppState{}
}

func (s *AppState) SetContext(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx = ctx
}

func (s *AppState) SetConnected(connected bool) {
	s.mu.Lock()
	s.Connected = connected
	s.mu.Unlock()
	s.emitStatus()
}

func (s *AppState) SetClassification(class string) {
	s.mu.Lock()
	s.Classification = class
	s.mu.Unlock()
	s.emitStatus()
}

func (s *AppState) AddEntry(entry UIEntry) {
	entry.Fresh = true
	s.mu.Lock()
	s.Entries = append(s.Entries, entry)
	if len(s.Entries) > 200 {
		s.Entries = s.Entries[len(s.Entries)-200:]
	}
	s.mu.Unlock()
	s.emit("entry:new", entry)
}

func (s *AppState) UpdateEntry(id int64, updates map[string]interface{}) {
	s.mu.Lock()
	for i := range s.Entries {
		if s.Entries[i].ID == id {
			if t, ok := updates["type"].(string); ok {
				s.Entries[i].Type = t
			}
			if c, ok := updates["content"].(string); ok {
				s.Entries[i].Content = c
			}
			if t, ok := updates["title"].(string); ok {
				s.Entries[i].Title = t
			}
			if a, ok := updates["artist"].(string); ok {
				s.Entries[i].Artist = a
			}
			break
		}
	}
	s.mu.Unlock()
	s.emit("entry:update", map[string]interface{}{"id": id, "updates": updates})
}

func (s *AppState) RemoveEntry(id int64) {
	s.mu.Lock()
	for i := range s.Entries {
		if s.Entries[i].ID == id {
			s.Entries = append(s.Entries[:i], s.Entries[i+1:]...)
			break
		}
	}
	s.mu.Unlock()
	s.emit("entry:remove", map[string]interface{}{"id": id})
}

func (s *AppState) SetDownloadProgress(percent float64, message string) {
	s.mu.Lock()
	s.Downloading = true
	s.DownloadPercent = percent
	s.DownloadMessage = message
	s.mu.Unlock()
	s.emit("download:progress", map[string]interface{}{
		"percent": percent,
		"message": message,
	})
}

func (s *AppState) SetDownloadComplete() {
	s.mu.Lock()
	s.Downloading = false
	s.DownloadPercent = 100
	s.mu.Unlock()
	s.emit("download:complete", nil)
}

func (s *AppState) SetGPUWarning(msg string) {
	s.mu.Lock()
	s.GPUWarning = msg
	s.mu.Unlock()
	s.emit("app:gpuWarning", msg)
}

func (s *AppState) emitStatus() {
	s.mu.RLock()
	data := map[string]interface{}{
		"connected":      s.Connected,
		"classification": s.Classification,
	}
	s.mu.RUnlock()
	s.emit("status:update", data)
}

func (s *AppState) emit(event string, data interface{}) {
	s.mu.RLock()
	ctx := s.ctx
	s.mu.RUnlock()
	if ctx != nil {
		wailsRuntime.EventsEmit(ctx, event, data)
	}
}

// Snapshot returns a copy of the current state for GetInitialState.
// The copy is safe to serialize without holding the lock.
func (s *AppState) Snapshot() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	entries := make([]UIEntry, len(s.Entries))
	copy(entries, s.Entries)
	// Strip Fresh flag from snapshot -- it's only meaningful for live events
	for i := range entries {
		entries[i].Fresh = false
	}

	return map[string]interface{}{
		"Connected":      s.Connected,
		"Classification": s.Classification,
		"Entries":         entries,
		"Downloading":     s.Downloading,
		"DownloadPercent": s.DownloadPercent,
		"DownloadMessage": s.DownloadMessage,
		"GPUWarning":      s.GPUWarning,
	}
}

// EntryFromDB converts a timestamp and text into a UIEntry.
// Convenience for the common transcription case.
func EntryFromDB(id int64, ts time.Time, entryType, content, title, artist string) UIEntry {
	return UIEntry{
		ID:        id,
		Timestamp: ts.Format(time.RFC3339),
		Type:      entryType,
		Content:   content,
		Title:     title,
		Artist:    artist,
	}
}
