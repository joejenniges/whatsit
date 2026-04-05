package ui

import (
	"fmt"
	"strings"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"github.com/joe/radio-transcriber/internal/storage"
)

// HistoryPage provides a search interface over past transcription entries
// stored in the SQLite database.
type HistoryPage struct {
	searchEntry *widget.Entry
	searchBtn   *widget.Button
	resultsList *widget.RichText
	scroll      *container.Scroll
	statusLbl   *widget.Label

	root fyne.CanvasObject

	// onSearch is called with the query string and max results; returns matches.
	onSearch func(query string, limit int) ([]storage.LogEntry, error)
}

// NewHistoryPage creates the history search view.
func NewHistoryPage(onSearch func(query string, limit int) ([]storage.LogEntry, error)) *HistoryPage {
	hp := &HistoryPage{
		onSearch: onSearch,
	}

	hp.searchEntry = widget.NewEntry()
	hp.searchEntry.SetPlaceHolder("Search transcripts...")
	hp.searchEntry.OnSubmitted = func(_ string) { hp.doSearch() }

	hp.searchBtn = widget.NewButton("Search", hp.doSearch)

	hp.statusLbl = widget.NewLabel("")

	hp.resultsList = widget.NewRichText()
	hp.resultsList.Wrapping = fyne.TextWrapWord

	hp.scroll = container.NewVScroll(hp.resultsList)
	hp.scroll.SetMinSize(fyne.NewSize(600, 350))

	searchBar := container.NewBorder(
		nil, nil,
		nil, hp.searchBtn,
		hp.searchEntry,
	)

	hp.root = container.NewBorder(
		container.NewVBox(searchBar, hp.statusLbl), // top
		nil, nil, nil,
		hp.scroll, // center
	)

	return hp
}

// Container returns the root canvas object for this page.
func (hp *HistoryPage) Container() fyne.CanvasObject {
	return hp.root
}

// doSearch runs the search query and populates results.
func (hp *HistoryPage) doSearch() {
	query := strings.TrimSpace(hp.searchEntry.Text)
	if query == "" {
		hp.statusLbl.SetText("Enter a search term.")
		return
	}

	if hp.onSearch == nil {
		hp.statusLbl.SetText("Search not available.")
		return
	}

	entries, err := hp.onSearch(query, 100)
	if err != nil {
		hp.statusLbl.SetText("Search error: " + err.Error())
		return
	}

	if len(entries) == 0 {
		hp.statusLbl.SetText("No results found.")
		hp.resultsList.Segments = nil
		hp.resultsList.Refresh()
		return
	}

	hp.statusLbl.SetText(fmt.Sprintf("%d result(s)", len(entries)))

	var segments []widget.RichTextSegment
	for _, e := range entries {
		ts := e.Timestamp.Local().Format("2006-01-02 15:04:05")
		entryType := e.EntryType

		header := fmt.Sprintf("[%s] (%s)", ts, entryType)
		segments = append(segments, &widget.TextSegment{
			Text:  header + "\n",
			Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{Bold: true}},
		})

		content := e.Content
		if content == "" {
			content = "(no content)"
		}
		segments = append(segments, &widget.TextSegment{
			Text:  content + "\n\n",
			Style: widget.RichTextStyle{TextStyle: fyne.TextStyle{}},
		})
	}

	hp.resultsList.Segments = segments
	hp.resultsList.Refresh()
	hp.scroll.ScrollToTop()
}
