package ui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"github.com/joe/radio-transcriber/internal/config"
)

// ConfigPage is the settings screen where users configure stream URL,
// model size, API keys, and other options.
type ConfigPage struct {
	cfg    *config.Config
	onSave func(*config.Config)

	streamURLEntry      *widget.Entry
	modelSizeSelect     *widget.Select
	modelSizeWarning    *widget.Label
	classifierSelect    *widget.Select
	saveAudioCheck      *widget.Check
	acoustIDEntry       *widget.Entry
	languageSelect      *widget.Select
	bufferSecsEntry     *widget.Entry
	windowSizeEntry     *widget.Entry
	windowStepEntry     *widget.Entry
	saveBtn             *widget.Button
	openFolderBtn       *widget.Button
	statusLbl           *widget.Label

	root fyne.CanvasObject
}

var supportedLanguages = []string{
	"en", "es", "fr", "de", "it", "pt", "nl", "pl", "ru",
	"ja", "ko", "zh", "ar", "hi", "tr", "sv", "da", "no", "fi",
}

// NewConfigPage creates the settings form, populated from the current config.
func NewConfigPage(cfg *config.Config, onSave func(*config.Config)) *ConfigPage {
	cp := &ConfigPage{
		cfg:    cfg,
		onSave: onSave,
	}

	// Stream URL
	cp.streamURLEntry = widget.NewEntry()
	cp.streamURLEntry.SetPlaceHolder("Enter MP3 stream URL")
	cp.streamURLEntry.SetText(cfg.StreamURL)

	// Whisper Model Size
	cp.modelSizeWarning = widget.NewLabel("Warning: small/medium models may not keep up with real-time transcription on CPU")
	cp.modelSizeWarning.Hide()

	cp.modelSizeSelect = widget.NewSelect(
		[]string{"tiny", "base", "small", "medium"},
		func(selected string) {
			if selected == "small" || selected == "medium" {
				cp.modelSizeWarning.Show()
			} else {
				cp.modelSizeWarning.Hide()
			}
		},
	)
	cp.modelSizeSelect.SetSelected(cfg.ModelSize)
	if cp.modelSizeSelect.Selected == "" {
		cp.modelSizeSelect.SetSelected("base")
	}

	// Classifier Tier
	cp.classifierSelect = widget.NewSelect(
		[]string{"basic", "scheirer", "mfcc", "whisper"},
		nil,
	)
	cp.classifierSelect.SetSelected(cfg.ClassifierTier)
	if cp.classifierSelect.Selected == "" {
		cp.classifierSelect.SetSelected("scheirer")
	}

	// Save Audio checkbox
	cp.saveAudioCheck = widget.NewCheck("Save original stereo audio to WAV files", nil)
	cp.saveAudioCheck.SetChecked(cfg.SaveAudio)

	// AcoustID API Key
	cp.acoustIDEntry = widget.NewEntry()
	cp.acoustIDEntry.SetPlaceHolder("AcoustID API key (optional)")
	cp.acoustIDEntry.SetText(cfg.AcoustIDKey)

	// Language
	cp.languageSelect = widget.NewSelect(supportedLanguages, nil)
	cp.languageSelect.SetSelected(cfg.Language)
	if cp.languageSelect.Selected == "" {
		cp.languageSelect.SetSelected("en")
	}

	// Buffer Seconds
	cp.bufferSecsEntry = widget.NewEntry()
	cp.bufferSecsEntry.SetPlaceHolder("Buffer duration in seconds")
	if cfg.BufferSecs > 0 {
		cp.bufferSecsEntry.SetText(strconv.Itoa(cfg.BufferSecs))
	} else {
		cp.bufferSecsEntry.SetText("10")
	}

	// Window Size
	cp.windowSizeEntry = widget.NewEntry()
	cp.windowSizeEntry.SetPlaceHolder("Rolling window size (seconds)")
	if cfg.WindowSizeSecs > 0 {
		cp.windowSizeEntry.SetText(strconv.Itoa(cfg.WindowSizeSecs))
	} else {
		cp.windowSizeEntry.SetText("10")
	}

	// Window Step
	cp.windowStepEntry = widget.NewEntry()
	cp.windowStepEntry.SetPlaceHolder("Rolling window step (seconds)")
	if cfg.WindowStepSecs > 0 {
		cp.windowStepEntry.SetText(strconv.Itoa(cfg.WindowStepSecs))
	} else {
		cp.windowStepEntry.SetText("3")
	}

	// Status label for save feedback
	cp.statusLbl = widget.NewLabel("")

	// Save button
	cp.saveBtn = widget.NewButton("Save", cp.handleSave)

	// Open Data Folder button
	cp.openFolderBtn = widget.NewButton("Open Data Folder", cp.handleOpenFolder)

	// Open Logs Folder button
	openLogsBtn := widget.NewButton("Open Logs Folder", cp.handleOpenLogsFolder)

	// Build the form layout using a grid with label-widget pairs.
	form := container.New(
		layout.NewFormLayout(),
		widget.NewLabel("Stream URL"), cp.streamURLEntry,
		widget.NewLabel("Whisper Model"), container.NewVBox(cp.modelSizeSelect, cp.modelSizeWarning),
		widget.NewLabel("Classifier Tier"), cp.classifierSelect,
		widget.NewLabel("Save Audio"), cp.saveAudioCheck,
		widget.NewLabel("AcoustID Key"), cp.acoustIDEntry,
		widget.NewLabel("Language"), cp.languageSelect,
		widget.NewLabel("Buffer (seconds)"), cp.bufferSecsEntry,
		widget.NewLabel("Window Size (s)"), cp.windowSizeEntry,
		widget.NewLabel("Window Step (s)"), cp.windowStepEntry,
	)

	buttons := container.NewHBox(cp.saveBtn, cp.openFolderBtn, openLogsBtn)

	cp.root = container.NewVBox(
		container.NewPadded(form),
		layout.NewSpacer(),
		container.NewPadded(
			container.NewVBox(
				buttons,
				cp.statusLbl,
			),
		),
	)

	return cp
}

// Container returns the root canvas object for this page.
func (cp *ConfigPage) Container() fyne.CanvasObject {
	return cp.root
}

// handleSave reads form values, updates the config, and calls the onSave
// callback.
func (cp *ConfigPage) handleSave() {
	bufSecs, err := strconv.Atoi(cp.bufferSecsEntry.Text)
	if err != nil || bufSecs <= 0 {
		cp.statusLbl.SetText("Buffer seconds must be a positive number.")
		return
	}

	winSize, err2 := strconv.Atoi(cp.windowSizeEntry.Text)
	if err2 != nil || winSize < 5 {
		cp.statusLbl.SetText("Window size must be at least 5 seconds.")
		return
	}
	winStep, err3 := strconv.Atoi(cp.windowStepEntry.Text)
	if err3 != nil || winStep < 1 {
		cp.statusLbl.SetText("Window step must be at least 1 second.")
		return
	}

	cp.cfg.StreamURL = cp.streamURLEntry.Text
	cp.cfg.ModelSize = cp.modelSizeSelect.Selected
	cp.cfg.ClassifierTier = cp.classifierSelect.Selected
	cp.cfg.SaveAudio = cp.saveAudioCheck.Checked
	cp.cfg.AcoustIDKey = cp.acoustIDEntry.Text
	cp.cfg.Language = cp.languageSelect.Selected
	cp.cfg.BufferSecs = bufSecs
	cp.cfg.WindowSizeSecs = winSize
	cp.cfg.WindowStepSecs = winStep

	if cp.onSave != nil {
		cp.onSave(cp.cfg)
	}

	cp.statusLbl.SetText("Settings saved.")
}

// handleOpenLogsFolder opens the logs directory in the OS file manager.
func (cp *ConfigPage) handleOpenLogsFolder() {
	dir, err := config.AppDir()
	if err != nil {
		cp.statusLbl.SetText("Could not determine data folder.")
		return
	}

	logsDir := filepath.Join(dir, "logs")
	// Create the logs directory if it doesn't exist yet.
	_ = os.MkdirAll(logsDir, 0o755)

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", logsDir)
	case "darwin":
		cmd = exec.Command("open", logsDir)
	default:
		cmd = exec.Command("xdg-open", logsDir)
	}

	if err := cmd.Start(); err != nil {
		cp.statusLbl.SetText("Failed to open logs folder: " + err.Error())
	}
}

// handleOpenFolder opens the RadioTranscriber data directory in the OS file
// manager.
func (cp *ConfigPage) handleOpenFolder() {
	dir, err := config.AppDir()
	if err != nil {
		cp.statusLbl.SetText("Could not determine data folder.")
		return
	}

	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("explorer", dir)
	case "darwin":
		cmd = exec.Command("open", dir)
	default:
		cmd = exec.Command("xdg-open", dir)
	}

	if err := cmd.Start(); err != nil {
		cp.statusLbl.SetText("Failed to open folder: " + err.Error())
	}
}
