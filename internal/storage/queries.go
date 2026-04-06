package storage

import (
	"database/sql"
	"fmt"
	"time"
)

// InsertEntry writes a new log entry to the database. If entry.Timestamp is
// zero the database DEFAULT (CURRENT_TIMESTAMP) is used. On success the
// entry's ID field is populated with the auto-generated row id.
func (d *Database) InsertEntry(entry *LogEntry) error {
	var (
		res sql.Result
		err error
	)

	if entry.Timestamp.IsZero() {
		res, err = d.db.Exec(`
			INSERT INTO log_entries (entry_type, content, artist, title, album, confidence, duration_ms, audio_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.EntryType, entry.Content, entry.Artist, entry.Title,
			entry.Album, entry.Confidence, entry.DurationMs, nullString(entry.AudioPath),
		)
	} else {
		res, err = d.db.Exec(`
			INSERT INTO log_entries (timestamp, entry_type, content, artist, title, album, confidence, duration_ms, audio_path)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			entry.Timestamp.UTC().Format(time.DateTime), entry.EntryType, entry.Content,
			entry.Artist, entry.Title, entry.Album, entry.Confidence, entry.DurationMs,
			nullString(entry.AudioPath),
		)
	}

	if err != nil {
		return fmt.Errorf("insert log entry: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return fmt.Errorf("get last insert id: %w", err)
	}
	entry.ID = id
	return nil
}

// GetRecentEntries returns the most recent entries ordered newest-first.
func (d *Database) GetRecentEntries(limit int) ([]LogEntry, error) {
	rows, err := d.db.Query(`
		SELECT id, timestamp, entry_type, content, artist, title, album, confidence, duration_ms, audio_path
		FROM log_entries
		ORDER BY timestamp DESC, id DESC
		LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("query recent entries: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// GetEntriesByType returns entries matching the given type, newest-first.
func (d *Database) GetEntriesByType(entryType string, limit int) ([]LogEntry, error) {
	rows, err := d.db.Query(`
		SELECT id, timestamp, entry_type, content, artist, title, album, confidence, duration_ms, audio_path
		FROM log_entries
		WHERE entry_type = ?
		ORDER BY timestamp DESC, id DESC
		LIMIT ?`, entryType, limit)
	if err != nil {
		return nil, fmt.Errorf("query entries by type: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// GetEntriesBetween returns entries whose timestamp falls within [start, end],
// ordered oldest-first (chronological).
func (d *Database) GetEntriesBetween(start, end time.Time) ([]LogEntry, error) {
	rows, err := d.db.Query(`
		SELECT id, timestamp, entry_type, content, artist, title, album, confidence, duration_ms, audio_path
		FROM log_entries
		WHERE timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC, id ASC`,
		start.UTC().Format(time.DateTime),
		end.UTC().Format(time.DateTime))
	if err != nil {
		return nil, fmt.Errorf("query entries between dates: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// scanEntries reads all rows into a slice of LogEntry. Nullable text columns
// are handled with sql.NullString / sql.NullFloat64 / sql.NullInt64.
func scanEntries(rows *sql.Rows) ([]LogEntry, error) {
	var entries []LogEntry
	for rows.Next() {
		var (
			e          LogEntry
			ts         string
			content    sql.NullString
			artist     sql.NullString
			title      sql.NullString
			album      sql.NullString
			confidence sql.NullFloat64
			durationMs sql.NullInt64
			audioPath  sql.NullString
		)
		if err := rows.Scan(&e.ID, &ts, &e.EntryType, &content, &artist,
			&title, &album, &confidence, &durationMs, &audioPath); err != nil {
			return nil, fmt.Errorf("scan log entry: %w", err)
		}

		// SQLite stores timestamps as text. Parse the two common formats
		// that CURRENT_TIMESTAMP and our explicit inserts produce.
		var err error
		e.Timestamp, err = parseTimestamp(ts)
		if err != nil {
			return nil, fmt.Errorf("parse timestamp %q: %w", ts, err)
		}

		e.Content = content.String
		e.Artist = artist.String
		e.Title = title.String
		e.Album = album.String
		e.Confidence = confidence.Float64
		e.DurationMs = durationMs.Int64
		e.AudioPath = audioPath.String

		entries = append(entries, e)
	}
	return entries, rows.Err()
}

// SearchEntries returns entries whose content matches the query string
// (case-insensitive LIKE), ordered newest-first, limited to limit rows.
func (d *Database) SearchEntries(query string, limit int) ([]LogEntry, error) {
	rows, err := d.db.Query(`
		SELECT id, timestamp, entry_type, content, artist, title, album, confidence, duration_ms, audio_path
		FROM log_entries
		WHERE content LIKE '%' || ? || '%'
		ORDER BY timestamp DESC, id DESC
		LIMIT ?`, query, limit)
	if err != nil {
		return nil, fmt.Errorf("search entries: %w", err)
	}
	defer rows.Close()
	return scanEntries(rows)
}

// UpdateEntryContent updates the content column of an existing log entry by ID.
func (d *Database) UpdateEntryContent(id int64, content string) error {
	_, err := d.db.Exec(`UPDATE log_entries SET content = ? WHERE id = ?`, content, id)
	if err != nil {
		return fmt.Errorf("update entry content: %w", err)
	}
	return nil
}

// UpdateEntrySong updates the title, artist, and content of a song entry by ID.
func (d *Database) UpdateEntrySong(id int64, title, artist string) error {
	content := title
	if artist != "" {
		content = title + " - " + artist
	}
	_, err := d.db.Exec(`UPDATE log_entries SET content = ?, title = ?, artist = ? WHERE id = ?`,
		content, title, artist, id)
	if err != nil {
		return fmt.Errorf("update song entry: %w", err)
	}
	return nil
}

// nullString returns a sql.NullString that is valid only if s is non-empty.
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// parseTimestamp tries common SQLite datetime formats.
func parseTimestamp(s string) (time.Time, error) {
	for _, layout := range []string{
		time.DateTime,        // "2006-01-02 15:04:05"
		"2006-01-02T15:04:05", // ISO 8601 without zone
		time.RFC3339,
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized timestamp format: %s", s)
}
