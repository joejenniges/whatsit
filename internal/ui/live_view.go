package ui

import (
	"fmt"
	"image/color"
	"regexp"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"

	"github.com/joe/radio-transcriber/internal/storage"
)

// liveEntry tracks a single entry in the live transcript log, backed by a
// database row so edits can be persisted.
type liveEntry struct {
	dbID      int64
	entryType string // "speech", "song", "music_unknown"
	content   string // raw display text (without timestamp prefix for speech)
	title     string // song title (for song entries)
	artist    string // song artist (for song entries)
	timestamp time.Time
}

// LiveView shows a scrolling transcription log with search, filtering,
// highlighting, editable song lines, and a status bar with controls.
type LiveView struct {
	textArea   *tappableRichText
	scroll     *container.Scroll
	statusLbl  *widget.Label
	latencyLbl *widget.Label
	startBtn   *widget.Button
	stopBtn    *widget.Button
	listenBtn  *widget.Check
	gpuWarning *fyne.Container

	// Search bar widgets
	searchEntry   *widget.Entry
	filterBtn     *widget.Button
	clearBtn      *widget.Button
	insertSongBtn *widget.Button

	// Edit bar -- hidden by default, shown when editing a song entry.
	editBar      *fyne.Container
	editEntry    *widget.Entry
	editOKBtn    *widget.Button
	editCancelBtn *widget.Button

	root fyne.CanvasObject

	onStart       func()
	onStop        func()
	onListen      func(enabled bool)
	onLoadHistory func(limit int) ([]storage.LogEntry, error)
	onEditSong    func(id int64, title, artist string) error
	onInsertSong  func() (*storage.LogEntry, error)

	running   bool
	listening bool

	// musicSegIdx tracks which liveEntry index is the current "Music playing"
	// placeholder, or -1 if none.
	musicSegIdx int

	// entries is the backing data for the displayed transcript.
	entries []liveEntry

	// Search/filter state
	searchText   string
	filterActive bool

	// editingIdx is the index of the entry currently being edited, or -1.
	editingIdx int

	// songSegmentMap maps RichText segment indices to entry indices for song
	// lines. Rebuilt on each rebuildDisplay. Used by tappableRichText to find
	// which song entry was tapped.
	songSegmentMap map[int]int // segment index -> entry index
}

// NewLiveView creates the live transcription view.
func NewLiveView(onStart, onStop func(), onListen func(enabled bool)) *LiveView {
	lv := &LiveView{
		onStart:        onStart,
		onStop:         onStop,
		onListen:       onListen,
		musicSegIdx:    -1,
		editingIdx:     -1,
		songSegmentMap: make(map[int]int),
	}

	// Custom tappable RichText for song line editing.
	lv.textArea = newTappableRichText(lv)

	lv.scroll = container.NewVScroll(lv.textArea)
	lv.scroll.SetMinSize(fyne.NewSize(600, 350))

	// Search bar
	lv.searchEntry = widget.NewEntry()
	lv.searchEntry.SetPlaceHolder("Search transcripts... (* = wildcard)")
	lv.searchEntry.OnChanged = func(text string) {
		lv.searchText = text
		lv.rebuildDisplay()
	}

	lv.filterBtn = widget.NewButton("Filter", func() {
		lv.filterActive = !lv.filterActive
		if lv.filterActive {
			lv.filterBtn.Importance = widget.HighImportance
		} else {
			lv.filterBtn.Importance = widget.MediumImportance
		}
		lv.filterBtn.Refresh()
		lv.rebuildDisplay()
	})
	lv.filterBtn.Importance = widget.MediumImportance

	lv.clearBtn = widget.NewButton("X", func() {
		lv.searchEntry.SetText("")
		lv.searchText = ""
		lv.filterActive = false
		lv.filterBtn.Importance = widget.MediumImportance
		lv.filterBtn.Refresh()
		lv.rebuildDisplay()
	})

	lv.insertSongBtn = widget.NewButton("+ Song", func() {
		lv.handleInsertSong()
	})

	// Edit bar (hidden by default)
	lv.editEntry = widget.NewEntry()
	lv.editEntry.SetPlaceHolder("Title - Artist")
	lv.editOKBtn = widget.NewButton("OK", func() { lv.commitEdit() })
	lv.editCancelBtn = widget.NewButton("Cancel", func() { lv.cancelEdit() })
	lv.editEntry.OnSubmitted = func(_ string) { lv.commitEdit() }

	lv.editBar = container.NewBorder(
		nil, nil,
		widget.NewLabel("Song:"),
		container.NewHBox(lv.editOKBtn, lv.editCancelBtn),
		lv.editEntry,
	)
	lv.editBar.Hide()

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
	lv.listenBtn.Disable()

	// GPU warning banner -- hidden by default.
	lv.gpuWarning = container.NewHBox()
	lv.gpuWarning.Hide()

	// Search bar layout: [search entry] [+ Song] [Filter] [X]
	searchBar := container.NewBorder(
		nil, nil,
		nil,
		container.NewHBox(lv.insertSongBtn, lv.filterBtn, lv.clearBtn),
		lv.searchEntry,
	)

	buttons := container.NewHBox(lv.startBtn, lv.stopBtn, lv.listenBtn)
	statusInfo := container.NewHBox(lv.statusLbl, lv.latencyLbl)
	statusBar := container.NewBorder(
		nil, nil,
		statusInfo, buttons,
	)

	lv.root = container.NewBorder(
		container.NewVBox(lv.gpuWarning, searchBar, lv.editBar), // top
		statusBar, // bottom
		nil, nil,
		lv.scroll, // center
	)

	return lv
}

// Container returns the root canvas object for this view.
func (lv *LiveView) Container() fyne.CanvasObject {
	return lv.root
}

// LoadHistory loads recent entries from the database and renders them
// chronologically. Called once after the view is created.
func (lv *LiveView) LoadHistory() {
	if lv.onLoadHistory == nil {
		return
	}

	entries, err := lv.onLoadHistory(100)
	if err != nil {
		return
	}

	if len(entries) == 0 {
		return
	}

	// GetRecentEntries returns newest-first; reverse for chronological display.
	for i, j := 0, len(entries)-1; i < j; i, j = i+1, j-1 {
		entries[i], entries[j] = entries[j], entries[i]
	}

	fyne.Do(func() {
		for _, e := range entries {
			le := liveEntry{
				dbID:      e.ID,
				entryType: e.EntryType,
				content:   e.Content,
				title:     e.Title,
				artist:    e.Artist,
				timestamp: e.Timestamp,
			}
			lv.entries = append(lv.entries, le)
		}
		lv.rebuildDisplay()
		lv.scrollToBottom()
	})
}

// SetLoadHistoryCallback sets the callback used to load history on launch.
func (lv *LiveView) SetLoadHistoryCallback(cb func(limit int) ([]storage.LogEntry, error)) {
	lv.onLoadHistory = cb
}

// SetEditSongCallback sets the callback for persisting song edits.
func (lv *LiveView) SetEditSongCallback(cb func(id int64, title, artist string) error) {
	lv.onEditSong = cb
}

// SetInsertSongCallback sets the callback for inserting a new song entry.
func (lv *LiveView) SetInsertSongCallback(cb func() (*storage.LogEntry, error)) {
	lv.onInsertSong = cb
}

// ShowGPUWarning displays a warning banner at the top of the live view.
// Safe to call from any goroutine.
func (lv *LiveView) ShowGPUWarning(message string) {
	warningText := canvas.NewText(message, color.NRGBA{R: 255, G: 140, B: 0, A: 255})
	warningText.TextStyle = fyne.TextStyle{Bold: true}
	warningText.TextSize = 12

	fyne.Do(func() {
		lv.gpuWarning.Objects = []fyne.CanvasObject{
			container.NewPadded(warningText),
		}
		lv.gpuWarning.Show()
		lv.gpuWarning.Refresh()
	})
}

// AppendTranscription adds a timestamped speech transcription line.
// Safe to call from any goroutine.
func (lv *LiveView) AppendTranscription(timestamp time.Time, text string, dbID int64) {
	fyne.Do(func() {
		le := liveEntry{
			dbID:      dbID,
			entryType: "speech",
			content:   strings.TrimSpace(text),
			timestamp: timestamp,
		}
		lv.entries = append(lv.entries, le)
		lv.rebuildDisplay()
		lv.scrollToBottom()
	})
}

// AppendSong adds or updates a song identification line. If there's an existing
// music placeholder, it replaces that entry with the identified song info.
// Safe to call from any goroutine.
func (lv *LiveView) AppendSong(title, artist string, dbID int64) {
	fyne.Do(func() {
		if lv.musicSegIdx >= 0 && lv.musicSegIdx < len(lv.entries) {
			lv.entries[lv.musicSegIdx].entryType = "song"
			lv.entries[lv.musicSegIdx].content = title + " - " + artist
			lv.entries[lv.musicSegIdx].title = title
			lv.entries[lv.musicSegIdx].artist = artist
			if dbID > 0 {
				lv.entries[lv.musicSegIdx].dbID = dbID
			}
		} else {
			le := liveEntry{
				dbID:      dbID,
				entryType: "song",
				content:   title + " - " + artist,
				title:     title,
				artist:    artist,
				timestamp: time.Now(),
			}
			lv.entries = append(lv.entries, le)
		}
		lv.musicSegIdx = -1
		lv.rebuildDisplay()
		lv.scrollToBottom()
	})
}

// UpdateSongLine updates the current music placeholder with song info without
// adding a new entry. Safe to call from any goroutine.
func (lv *LiveView) UpdateSongLine(title, artist string) {
	fyne.Do(func() {
		if lv.musicSegIdx >= 0 && lv.musicSegIdx < len(lv.entries) {
			lv.entries[lv.musicSegIdx].title = title
			lv.entries[lv.musicSegIdx].artist = artist
			if title != "" {
				lv.entries[lv.musicSegIdx].content = title + " - " + artist
				lv.entries[lv.musicSegIdx].entryType = "song"
			}
			lv.rebuildDisplay()
		}
	})
}

// AppendMusic adds a "--- Song played ---" placeholder. Only one per music
// segment -- subsequent calls while already showing music are no-ops.
// Safe to call from any goroutine.
func (lv *LiveView) AppendMusic() {
	fyne.Do(func() {
		if lv.musicSegIdx >= 0 {
			return
		}

		le := liveEntry{
			entryType: "music_unknown",
			content:   "Song played",
			timestamp: time.Now(),
		}
		lv.musicSegIdx = len(lv.entries)
		lv.entries = append(lv.entries, le)
		lv.rebuildDisplay()
		lv.scrollToBottom()
	})
}

// ClearMusicMarker resets the music segment tracker.
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

// handleInsertSong inserts a new song entry at the bottom of the log and
// immediately opens it for editing.
func (lv *LiveView) handleInsertSong() {
	if lv.onInsertSong == nil {
		return
	}

	entry, err := lv.onInsertSong()
	if err != nil {
		return
	}

	le := liveEntry{
		dbID:      entry.ID,
		entryType: "song",
		content:   "Song played",
		timestamp: entry.Timestamp,
	}
	lv.entries = append(lv.entries, le)
	lv.rebuildDisplay()
	lv.scrollToBottom()

	// Open the newly inserted entry for editing.
	lv.startEditing(len(lv.entries) - 1)
}

// startEditing opens the edit bar for the song entry at the given index.
func (lv *LiveView) startEditing(idx int) {
	if idx < 0 || idx >= len(lv.entries) {
		return
	}
	e := lv.entries[idx]
	if e.entryType != "song" && e.entryType != "music_unknown" {
		return
	}

	lv.editingIdx = idx

	if e.title != "" {
		lv.editEntry.SetText(e.title + " - " + e.artist)
	} else if e.content != "" && e.content != "Song played" {
		lv.editEntry.SetText(e.content)
	} else {
		lv.editEntry.SetText("")
	}

	lv.editBar.Show()
	// Focus the edit entry so the user can type immediately.
	if c := fyne.CurrentApp().Driver().AllWindows(); len(c) > 0 {
		c[0].Canvas().Focus(lv.editEntry)
	}
}

// commitEdit saves the current edit and closes the edit bar.
func (lv *LiveView) commitEdit() {
	idx := lv.editingIdx
	if idx < 0 || idx >= len(lv.entries) {
		lv.cancelEdit()
		return
	}

	text := strings.TrimSpace(lv.editEntry.Text)
	if text == "" {
		text = "Song played"
	}

	title := text
	artist := ""
	if parts := strings.SplitN(text, " - ", 2); len(parts) == 2 {
		title = strings.TrimSpace(parts[0])
		artist = strings.TrimSpace(parts[1])
	}

	lv.entries[idx].content = text
	lv.entries[idx].title = title
	lv.entries[idx].artist = artist
	if lv.entries[idx].entryType == "music_unknown" {
		lv.entries[idx].entryType = "song"
	}

	// Persist to database.
	if lv.onEditSong != nil && lv.entries[idx].dbID > 0 {
		_ = lv.onEditSong(lv.entries[idx].dbID, title, artist)
	}

	lv.editingIdx = -1
	lv.editBar.Hide()
	lv.rebuildDisplay()
}

// cancelEdit closes the edit bar without saving.
func (lv *LiveView) cancelEdit() {
	lv.editingIdx = -1
	lv.editBar.Hide()
}

// rebuildDisplay reconstructs the RichText segments from lv.entries,
// applying search highlighting and filtering.
func (lv *LiveView) rebuildDisplay() {
	var segments []widget.RichTextSegment
	pattern := lv.compileSearchPattern()
	songMap := make(map[int]int) // segment index -> entry index

	for i, e := range lv.entries {
		displayText := lv.formatEntry(e)

		// Filter: if active and search has text, skip non-matching entries.
		if lv.filterActive && lv.searchText != "" {
			if pattern == nil || !pattern.MatchString(displayText) {
				continue
			}
		}

		isSong := e.entryType == "song" || e.entryType == "music_unknown"

		if isSong {
			segs := lv.buildSongSegments(displayText, pattern)
			// Map the first segment of this song line to the entry index.
			songMap[len(segments)] = i
			segments = append(segments, segs...)
		} else {
			segs := lv.buildTextSegments(displayText, pattern)
			segments = append(segments, segs...)
		}
	}

	lv.songSegmentMap = songMap
	lv.textArea.Segments = segments
	lv.textArea.Refresh()
}

// formatEntry returns the display text for an entry.
func (lv *LiveView) formatEntry(e liveEntry) string {
	switch e.entryType {
	case "speech":
		ts := e.timestamp.Local().Format("15:04:05")
		return fmt.Sprintf("[%s] %s", ts, e.content)
	case "song":
		if e.title != "" && e.artist != "" {
			return fmt.Sprintf("--- \"%s\" - %s ---", e.title, e.artist)
		}
		if e.content != "" && e.content != "Song played" {
			return fmt.Sprintf("--- %s ---", e.content)
		}
		return "--- Song played ---"
	case "music_unknown":
		return "--- Song played ---"
	default:
		return e.content
	}
}

// buildSongSegments builds RichText segments for a song line with an [edit]
// indicator appended.
func (lv *LiveView) buildSongSegments(displayText string, pattern *regexp.Regexp) []widget.RichTextSegment {
	var segments []widget.RichTextSegment

	songStyle := widget.RichTextStyle{
		TextStyle: fyne.TextStyle{Bold: true},
	}

	if pattern != nil && lv.searchText != "" {
		segs := lv.highlightText(displayText, pattern, songStyle)
		segments = append(segments, segs...)
	} else {
		segments = append(segments, &widget.TextSegment{
			Text:  displayText,
			Style: songStyle,
		})
	}

	// Append " [edit]" in a subtle style so the user knows it's clickable.
	editStyle := widget.RichTextStyle{
		TextStyle: fyne.TextStyle{Italic: true},
		ColorName: theme.ColorNamePlaceHolder,
	}
	segments = append(segments, &widget.TextSegment{
		Text:  "  [edit]\n",
		Style: editStyle,
	})

	return segments
}

// buildTextSegments builds RichText segments for a text line, applying search
// highlighting if a pattern is active.
func (lv *LiveView) buildTextSegments(text string, pattern *regexp.Regexp) []widget.RichTextSegment {
	normalStyle := widget.RichTextStyle{TextStyle: fyne.TextStyle{}}

	if pattern != nil && lv.searchText != "" {
		segs := lv.highlightText(text, pattern, normalStyle)
		segs = append(segs, &widget.TextSegment{
			Text:  "\n",
			Style: widget.RichTextStyle{},
		})
		return segs
	}

	return []widget.RichTextSegment{
		&widget.TextSegment{
			Text:  text + "\n",
			Style: normalStyle,
		},
	}
}

// highlightText splits text into segments, highlighting portions that match
// the search pattern. Non-matching parts use baseStyle; matching parts use
// bold + primary color.
func (lv *LiveView) highlightText(text string, pattern *regexp.Regexp, baseStyle widget.RichTextStyle) []widget.RichTextSegment {
	locs := pattern.FindAllStringIndex(text, -1)
	if len(locs) == 0 {
		return []widget.RichTextSegment{
			&widget.TextSegment{Text: text, Style: baseStyle},
		}
	}

	highlightStyle := widget.RichTextStyle{
		TextStyle: fyne.TextStyle{Bold: true},
		ColorName: theme.ColorNamePrimary,
	}

	var segments []widget.RichTextSegment
	pos := 0
	for _, loc := range locs {
		if loc[0] > pos {
			segments = append(segments, &widget.TextSegment{
				Text:  text[pos:loc[0]],
				Style: baseStyle,
			})
		}
		segments = append(segments, &widget.TextSegment{
			Text:  text[loc[0]:loc[1]],
			Style: highlightStyle,
		})
		pos = loc[1]
	}
	if pos < len(text) {
		segments = append(segments, &widget.TextSegment{
			Text:  text[pos:],
			Style: baseStyle,
		})
	}
	return segments
}

// compileSearchPattern converts the user's search text (simple glob with * as
// wildcard) into a compiled regexp. Returns nil if search text is empty or
// pattern is invalid.
func (lv *LiveView) compileSearchPattern() *regexp.Regexp {
	if lv.searchText == "" {
		return nil
	}

	// Split on * to get literal parts, escape each, rejoin with .*
	parts := strings.Split(lv.searchText, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	patternStr := "(?i)" + strings.Join(parts, ".*")

	re, err := regexp.Compile(patternStr)
	if err != nil {
		return nil
	}
	return re
}

// tappableRichText extends RichText to detect taps on song lines for editing.
// WHY custom widget: Fyne's RichText doesn't have per-segment tap callbacks.
// We estimate which line was tapped by Y position and check if it corresponds
// to a song entry in the songSegmentMap.
type tappableRichText struct {
	widget.RichText
	lv *LiveView
}

func newTappableRichText(lv *LiveView) *tappableRichText {
	rt := &tappableRichText{lv: lv}
	rt.Wrapping = fyne.TextWrapWord
	rt.ExtendBaseWidget(rt)
	return rt
}

// Tapped handles tap events on the RichText. We estimate which entry was
// tapped and open editing if it's a song line.
func (rt *tappableRichText) Tapped(ev *fyne.PointEvent) {
	if len(rt.lv.songSegmentMap) == 0 {
		return
	}

	// Estimate which line was tapped. Fyne's default text size is ~14px,
	// and with padding the effective line height is ~20-24px.
	// This is an approximation -- wrapping makes it imprecise for long lines.
	lineH := float32(22)
	tapLine := int(ev.Position.Y / lineH)

	// Walk through segments counting newlines to find which entry the tap
	// falls on.
	currentLine := 0
	for segIdx, seg := range rt.Segments {
		if ts, ok := seg.(*widget.TextSegment); ok {
			// Check if this segment's line range includes the tap line.
			newlines := strings.Count(ts.Text, "\n")
			startLine := currentLine
			currentLine += newlines

			if tapLine >= startLine && tapLine <= currentLine {
				// Find the closest song segment at or before this segment index.
				if entryIdx, ok := rt.lv.findSongForSegment(segIdx); ok {
					rt.lv.startEditing(entryIdx)
					return
				}
			}
		}
	}
}

// findSongForSegment finds the song entry index for a segment index by
// checking the songSegmentMap for the closest song start at or before segIdx.
func (lv *LiveView) findSongForSegment(segIdx int) (int, bool) {
	bestSegStart := -1
	bestEntryIdx := -1
	for songSegStart, entryIdx := range lv.songSegmentMap {
		if songSegStart <= segIdx && songSegStart > bestSegStart {
			bestSegStart = songSegStart
			bestEntryIdx = entryIdx
		}
	}
	if bestEntryIdx < 0 {
		return 0, false
	}

	// Verify this segment is actually within this song's segments (not past
	// the next entry's segments). A song line produces 2-3 segments typically.
	// If the gap is too large, it's probably a different entry.
	if segIdx-bestSegStart > 5 {
		return 0, false
	}
	return bestEntryIdx, true
}

// Ensure tappableRichText implements Tappable.
var _ fyne.Tappable = (*tappableRichText)(nil)
