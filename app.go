package main

import (
	"context"
	"log"
	"path/filepath"

	"github.com/joe/radio-transcriber/internal/config"
	"github.com/joe/radio-transcriber/internal/storage"
)

// App struct holds the Wails application context and backend references.
type App struct {
	ctx    context.Context
	config *config.Config
	db     *storage.Database
}

// NewApp creates a new App instance.
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the Wails runtime from Go.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	cfg, err := config.Load()
	if err != nil {
		log.Printf("Failed to load config: %v", err)
		return
	}
	a.config = cfg

	// Open database
	appDir, _ := config.AppDir()
	db, err := storage.New(filepath.Join(appDir, "transcripts.db"))
	if err != nil {
		log.Printf("Failed to open database: %v", err)
		return
	}
	a.db = db
}

// shutdown is called when the app is closing.
func (a *App) shutdown(ctx context.Context) {
	if a.db != nil {
		a.db.Close()
	}
}

// GetRecentEntries returns the last N entries from the database.
// This is bound to the frontend.
func (a *App) GetRecentEntries(limit int) ([]storage.LogEntry, error) {
	if a.db == nil {
		return nil, nil
	}
	return a.db.GetRecentEntries(limit)
}

// GetConfig returns the current configuration.
func (a *App) GetConfig() *config.Config {
	return a.config
}
