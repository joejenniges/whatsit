package storage

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// newTestDB creates a temporary SQLite database for testing and returns the
// Database handle plus a cleanup function.
func newTestDB(t *testing.T) *Database {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("New(%q): %v", dbPath, err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestNewCreatesFileAndSchema(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "sub", "test.db")

	db, err := New(dbPath)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer db.Close()

	// File should exist on disk.
	if _, err := os.Stat(dbPath); err != nil {
		t.Fatalf("database file not created: %v", err)
	}

	// Table should exist — a simple query against it should not error.
	var count int
	if err := db.db.QueryRow("SELECT COUNT(*) FROM log_entries").Scan(&count); err != nil {
		t.Fatalf("schema not applied: %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 rows in fresh db, got %d", count)
	}
}

func TestNewIdempotentMigrations(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	db1, err := New(dbPath)
	if err != nil {
		t.Fatalf("first New: %v", err)
	}
	db1.Close()

	// Opening the same database again should not fail.
	db2, err := New(dbPath)
	if err != nil {
		t.Fatalf("second New: %v", err)
	}
	db2.Close()
}

func TestInsertAndGetRecentEntries(t *testing.T) {
	db := newTestDB(t)

	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)

	entries := []LogEntry{
		{
			Timestamp:  now,
			EntryType:  "speech",
			Content:    "Hello world",
			Confidence: 0.95,
			DurationMs: 5000,
		},
		{
			Timestamp:  now.Add(10 * time.Second),
			EntryType:  "song",
			Content:    "Bohemian Rhapsody",
			Artist:     "Queen",
			Title:      "Bohemian Rhapsody",
			Album:      "A Night at the Opera",
			Confidence: 0.88,
			DurationMs: 15000,
		},
		{
			Timestamp:  now.Add(30 * time.Second),
			EntryType:  "speech",
			Content:    "And now the news",
			Confidence: 0.92,
			DurationMs: 3000,
		},
	}

	for i := range entries {
		if err := db.InsertEntry(&entries[i]); err != nil {
			t.Fatalf("InsertEntry[%d]: %v", i, err)
		}
		if entries[i].ID == 0 {
			t.Fatalf("InsertEntry[%d]: ID not set", i)
		}
	}

	// Get all 3 back, newest first.
	got, err := db.GetRecentEntries(10)
	if err != nil {
		t.Fatalf("GetRecentEntries: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(got))
	}

	// Newest first.
	if got[0].Content != "And now the news" {
		t.Errorf("first entry content = %q, want %q", got[0].Content, "And now the news")
	}
	if got[1].Artist != "Queen" {
		t.Errorf("second entry artist = %q, want %q", got[1].Artist, "Queen")
	}

	// Limit works.
	got, err = db.GetRecentEntries(1)
	if err != nil {
		t.Fatalf("GetRecentEntries(1): %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
}

func TestGetEntriesByType(t *testing.T) {
	db := newTestDB(t)

	now := time.Date(2026, 4, 5, 12, 0, 0, 0, time.UTC)

	for i, e := range []LogEntry{
		{Timestamp: now, EntryType: "speech", Content: "first speech"},
		{Timestamp: now.Add(5 * time.Second), EntryType: "song", Content: "a song"},
		{Timestamp: now.Add(10 * time.Second), EntryType: "speech", Content: "second speech"},
		{Timestamp: now.Add(15 * time.Second), EntryType: "silence"},
		{Timestamp: now.Add(20 * time.Second), EntryType: "speech", Content: "third speech"},
	} {
		entry := e
		if err := db.InsertEntry(&entry); err != nil {
			t.Fatalf("InsertEntry[%d]: %v", i, err)
		}
	}

	speech, err := db.GetEntriesByType("speech", 10)
	if err != nil {
		t.Fatalf("GetEntriesByType(speech): %v", err)
	}
	if len(speech) != 3 {
		t.Fatalf("expected 3 speech entries, got %d", len(speech))
	}
	for _, e := range speech {
		if e.EntryType != "speech" {
			t.Errorf("unexpected entry_type %q in speech results", e.EntryType)
		}
	}

	songs, err := db.GetEntriesByType("song", 10)
	if err != nil {
		t.Fatalf("GetEntriesByType(song): %v", err)
	}
	if len(songs) != 1 {
		t.Fatalf("expected 1 song entry, got %d", len(songs))
	}

	// Limit respected.
	speech, err = db.GetEntriesByType("speech", 2)
	if err != nil {
		t.Fatalf("GetEntriesByType(speech, 2): %v", err)
	}
	if len(speech) != 2 {
		t.Fatalf("expected 2 speech entries with limit, got %d", len(speech))
	}
}

func TestGetEntriesBetween(t *testing.T) {
	db := newTestDB(t)

	base := time.Date(2026, 4, 5, 10, 0, 0, 0, time.UTC)

	timestamps := []time.Duration{
		0,
		30 * time.Minute,
		1 * time.Hour,
		90 * time.Minute,
		2 * time.Hour,
	}

	for i, offset := range timestamps {
		entry := LogEntry{
			Timestamp:  base.Add(offset),
			EntryType:  "speech",
			Content:    "entry",
			DurationMs: 5000,
		}
		if err := db.InsertEntry(&entry); err != nil {
			t.Fatalf("InsertEntry[%d]: %v", i, err)
		}
	}

	// Query for the middle window: 10:30 to 11:30 (should match entries at 10:30, 11:00, 11:30).
	start := base.Add(30 * time.Minute)
	end := base.Add(90 * time.Minute)

	got, err := db.GetEntriesBetween(start, end)
	if err != nil {
		t.Fatalf("GetEntriesBetween: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 entries in range, got %d", len(got))
	}

	// Should be chronological (oldest first).
	if !got[0].Timestamp.Before(got[1].Timestamp) {
		t.Error("results not in chronological order")
	}

	// Narrow window: just the exact hour mark.
	exact := base.Add(1 * time.Hour)
	got, err = db.GetEntriesBetween(exact, exact)
	if err != nil {
		t.Fatalf("GetEntriesBetween exact: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry for exact timestamp, got %d", len(got))
	}
}

func TestInsertEntryZeroTimestamp(t *testing.T) {
	db := newTestDB(t)

	entry := LogEntry{
		EntryType:  "silence",
		DurationMs: 2000,
	}
	if err := db.InsertEntry(&entry); err != nil {
		t.Fatalf("InsertEntry with zero timestamp: %v", err)
	}
	if entry.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := db.GetRecentEntries(1)
	if err != nil {
		t.Fatalf("GetRecentEntries: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(got))
	}
	if got[0].Timestamp.IsZero() {
		t.Error("expected database to fill in timestamp via DEFAULT, got zero")
	}
}
