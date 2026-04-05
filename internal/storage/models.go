package storage

import "time"

// LogEntry represents a single logged event from the radio stream — speech
// transcription, identified song, unidentified music, or silence.
type LogEntry struct {
	ID         int64
	Timestamp  time.Time
	EntryType  string  // "speech", "song", "music_unknown", "silence"
	Content    string  // transcription text or song description
	Artist     string  // populated for songs
	Title      string  // populated for songs
	Album      string  // populated for songs
	Confidence float64 // whisper confidence or acoustid score
	DurationMs int64   // segment duration in milliseconds
}
