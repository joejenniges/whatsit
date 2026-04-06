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
	textArea   *widget.RichText
	scroll     *container.Scroll
	statusLbl  *widget.Label
	latencyLbl *widget.Label
	startBtn   *widget.Button
	stopBtn    *widget.Button
	listenBtn  *widget.Check

	root fyne.CanvasObject

	onStart  func()
	onStop   func()
	onListen func(enabled bool)

	// running tracks whether we're currently streaming, to toggle button
	// enabled state.
	running   bool
	listening bool

	// musicSegIdx is the index of the current "--- Music playing ---" segment
	// in textArea.Segments, or -1 if there isn't one. Used to replace it with
	// song info when identified.
	musicSegIdx int
}

// NewLiveView creates the live transcription view.
func NewLiveView(onStart, onStop func(), onListen func(enabled bool)) *LiveView {
	lv := &LiveView{
		onStart:     onStart,
		onStop:      onStop,
		onListen:    onListen,
		musicSegIdx: -1,
	}

	// RichText starts empty. We append segments as events arrive.
	lv.textArea = widget.NewRichText()
	lv.textArea.Wrapping = fyne.TextWrapWord

	lv.scroll = container.NewVScroll(lv.textArea)
	lv.scroll.SetMinSize(fyne.NewSize(600, 350))

	lv.statusLbl = widget.NewLabel("[Disconnected]  Classification: --")
	lv.latencyLbl = widget.NewLabel("")

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

	statusInfo := container.NewHBox(lv.statusLbl, lv.latencyLbl)
	statusBar := container.NewBorder(
		nil, nil,
		statusInfo, buttons,
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

// AppendSong adds a song identification divider. If there's an existing
// "--- Music playing ---" marker, it replaces that marker with the song info.
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
		if lv.musicSegIdx >= 0 && lv.musicSegIdx < len(lv.textArea.Segments) {
			// Replace the placeholder with the actual song info.
			lv.textArea.Segments[lv.musicSegIdx] = seg
		} else {
			lv.textArea.Segments = append(lv.textArea.Segments, seg)
		}
		lv.musicSegIdx = -1
		lv.textArea.Refresh()
		lv.scrollToBottom()
	})
}

// AppendMusic adds a "--- Music playing ---" placeholder. Only adds one per
// music segment -- subsequent calls while already showing music are no-ops.
// The placeholder can be replaced by AppendSong if the song is identified.
// Safe to call from any goroutine.
func (lv *LiveView) AppendMusic() {
	fyne.Do(func() {
		// Already showing a music marker for this segment.
		if lv.musicSegIdx >= 0 {
			return
		}

		seg := &widget.TextSegment{
			Text: "--- Music playing ---\n",
			Style: widget.RichTextStyle{
				TextStyle: fyne.TextStyle{Italic: true},
			},
		}

		lv.musicSegIdx = len(lv.textArea.Segments)
		lv.textArea.Segments = append(lv.textArea.Segments, seg)
		lv.textArea.Refresh()
		lv.scrollToBottom()
	})
}

// ClearMusicMarker resets the music segment tracker. Call when transitioning
// away from music (e.g. back to speech).
func (lv *LiveView) ClearMusicMarker() {
	fyne.Do(func() {
		lv.musicSegIdx = -1
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

// UpdateLatency updates the latency indicator in the status bar.
// Safe to call from any goroutine.
func (lv *LiveView) UpdateLatency(latency time.Duration) {
	text := fmt.Sprintf("Latency: ~%ds", int(latency.Seconds()))
	fyne.Do(func() {
		lv.latencyLbl.SetText(text)
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
