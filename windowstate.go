package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"

	"github.com/joe/radio-transcriber/internal/config"
)

// WindowState stores the window position and size between sessions.
type WindowState struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

const windowStateFile = "window.json"

// LoadWindowState reads saved window state from AppData.
// Returns defaults if the file doesn't exist or can't be read.
func LoadWindowState() WindowState {
	defaults := WindowState{X: -1, Y: -1, Width: 900, Height: 600}

	appDir, err := config.AppDir()
	if err != nil {
		return defaults
	}

	data, err := os.ReadFile(filepath.Join(appDir, windowStateFile))
	if err != nil {
		return defaults
	}

	var state WindowState
	if err := json.Unmarshal(data, &state); err != nil {
		return defaults
	}

	// Sanity check dimensions.
	if state.Width < 400 {
		state.Width = 900
	}
	if state.Height < 300 {
		state.Height = 600
	}

	// Sanity check position. Reject positions that are clearly off-screen.
	// A reasonable max is ~8000px (4x 4K monitors). Negative values beyond
	// a small margin are also rejected (some window managers use small
	// negative offsets for window chrome).
	if state.X < -50 || state.X > 8000 || state.Y < -50 || state.Y > 4000 {
		state.X = -1 // -1 means let the OS decide
		state.Y = -1
	}

	return state
}

// SaveWindowState writes window state to AppData.
func SaveWindowState(state WindowState) {
	appDir, err := config.AppDir()
	if err != nil {
		return
	}

	data, err := json.Marshal(state)
	if err != nil {
		return
	}

	if err := os.WriteFile(filepath.Join(appDir, windowStateFile), data, 0o644); err != nil {
		log.Printf("save window state: %v", err)
	}
}
