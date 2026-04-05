package ui

import (
	"fmt"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// LiveView shows a scrolling transcription log with a status bar and
// start/stop controls.
type LiveView struct {
	textArea  *widget.RichText
	scroll    *container.Scroll
	statusLbl *widget.Label
	startBtn  *widget.Button
	stopBtn   *widget.Button
	listenBtn *widget.Check

	root fyne.CanvasObject

	onStart  func()
	onStop   func()
	onListen func(enabled bool)

	// running tracks whether we're currently streaming, to toggle button
	// enabled state.
	running   bool
	listening bool
}

// NewLiveView creates the live transcription view.
func NewLiveView(onStart, onStop func(), onListen func(enabled bool)) *LiveView {
	lv := &LiveView{
		onStart:  onStart,
		onStop:   onStop,
		onListen: onListen,
	}

	// RichText starts empty. We append segments as events arrive.
	lv.textArea = widget.NewRichText()
	lv.textArea.Wrapping = fyne.TextWrapWord

	lv.scroll = container.NewVScroll(lv.textArea)
	lv.scroll.SetMinSize(fyne.NewSize(600, 350))

	lv.statusLbl = widget.NewLabel("[Disconnected]  Classification: --")

	lv.startBtn = widget.NewButton("Start", func() {
		lv.setRunning(true)
		if lv.onStart != nil {
			lv.onStart()
		}
	})

	lv.stopBtn = widget.NewButton("Stop", func() {
		lv.setRunning(false)
		if lv.onStop != nil {
			lv.onStop()
		}
	})
	lv.stopBtn.Disable()

	lv.listenBtn = widget.NewCheck("Listen", func(on bool) {
		lv.listening = on
		if lv.onListen != nil {
			lv.onListen(on)
		}
	})
	lv.listenBtn.Disable() // Only enabled while streaming.

	buttons := container.NewHBox(lv.startBtn, lv.stopBtn, lv.listenBtn)

	statusBar := container.NewBorder(
		nil, nil,
		lv.statusLbl, buttons,
	)

	lv.root = container.NewBorder(
		nil,       // top
		statusBar, // bottom
		nil, nil,
		lv.scroll, // center (fills remaining space)
	)

	return lv
}

// Container returns the root canvas object for this view.
func (lv *LiveView) Container() fyne.CanvasObject {
	return lv.root
}

// AppendTranscription adds a timestamped speech transcription line.
// Safe to call from any goroutine.
func (lv *LiveView) AppendTranscription(timestamp time.Time, text string) {
	ts := timestamp.Local().Format("15:04:05")
	line := fmt.Sprintf("[%s] %s", ts, strings.TrimSpace(text))

	seg := &widget.TextSegment{
		Text:  line + "\n",
		Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
	}

	fyne.Do(func() {
		lv.textArea.Segments = append(lv.textArea.Segments, seg)
		lv.textArea.Refresh()
		lv.scrollToBottom()
	})
}

// AppendSong adds a song identification divider.
// Safe to call from any goroutine.
func (lv *LiveView) AppendSong(title, artist string) {
	line := fmt.Sprintf("--- \"%s\" - %s ---", title, artist)

	seg := &widget.TextSegment{
		Text: line + "\n",
		Style: widget.RichTextStyle{
			TextStyle: fyne.TextStyle{Bold: true},
		},
	}

	fyne.Do(func() {
		lv.textArea.Segments = append(lv.textArea.Segments, seg)
		lv.textArea.Refresh()
		lv.scrollToBottom()
	})
}

// AppendMusic adds a generic music-playing divider.
// Safe to call from any goroutine.
func (lv *LiveView) AppendMusic() {
	seg := &widget.TextSegment{
		Text: "--- Music playing ---\n",
		Style: widget.RichTextStyle{
			TextStyle: fyne.TextStyle{Italic: true},
		},
	}

	fyne.Do(func() {
		lv.textArea.Segments = append(lv.textArea.Segments, seg)
		lv.textArea.Refresh()
		lv.scrollToBottom()
	})
}

// UpdateStatus updates the status bar text. Safe to call from any goroutine.
func (lv *LiveView) UpdateStatus(connected bool, classification string) {
	connText := "[Disconnected]"
	if connected {
		connText = "[Connected]"
	}

	classText := classification
	if classText == "" {
		classText = "--"
	}

	fyne.Do(func() {
		lv.statusLbl.SetText(fmt.Sprintf("%s  Classification: %s", connText, classText))
	})
}

// setRunning updates button enabled states.
func (lv *LiveView) setRunning(running bool) {
	lv.running = running
	if running {
		lv.startBtn.Disable()
		lv.stopBtn.Enable()
		lv.listenBtn.Enable()
	} else {
		lv.startBtn.Enable()
		lv.stopBtn.Disable()
		// Turn off listening when stream stops.
		if lv.listening {
			lv.listenBtn.SetChecked(false)
			lv.listening = false
			if lv.onListen != nil {
				lv.onListen(false)
			}
		}
		lv.listenBtn.Disable()
	}
}

// scrollToBottom scrolls the scroll container to the bottom.
func (lv *LiveView) scrollToBottom() {
	lv.scroll.ScrollToBottom()
}
