package storage

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

// Database wraps a sql.DB connection to the local SQLite transcript store.
type Database struct {
	db *sql.DB
}

// New opens (or creates) a SQLite database at dbPath, ensures the parent
// directory exists, and runs schema migrations. The caller is responsible for
// resolving the full path (e.g. %APPDATA%/RadioTranscriber/transcripts.db).
func New(dbPath string) (*Database, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}

	sqlDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// Quick sanity check before running migrations.
	if err := sqlDB.Ping(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	d := &Database{db: sqlDB}
	if err := d.migrate(); err != nil {
		sqlDB.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	return d, nil
}

// Close releases the underlying database connection.
func (d *Database) Close() error {
	return d.db.Close()
}

// migrate applies the schema. All statements use IF NOT EXISTS so this is
// safe to run on every startup.
func (d *Database) migrate() error {
	const schema = `
CREATE TABLE IF NOT EXISTS log_entries (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp   DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    entry_type  TEXT NOT NULL,
    content     TEXT,
    artist      TEXT,
    title       TEXT,
    album       TEXT,
    confidence  REAL,
    duration_ms INTEGER
);

CREATE INDEX IF NOT EXISTS idx_log_timestamp ON log_entries(timestamp);
CREATE INDEX IF NOT EXISTS idx_log_type ON log_entries(entry_type);
`
	if _, err := d.db.Exec(schema); err != nil {
		return err
	}

	// WHY: SQLite ALTER TABLE ADD COLUMN fails if the column already exists,
	// and there is no IF NOT EXISTS syntax for columns. We just attempt the
	// alter and ignore the "duplicate column" error.
	_, err := d.db.Exec(`ALTER TABLE log_entries ADD COLUMN audio_path TEXT`)
	if err != nil && !isDuplicateColumnErr(err) {
		return fmt.Errorf("add audio_path column: %w", err)
	}

	return nil
}

// isDuplicateColumnErr returns true if the error is SQLite's
// "duplicate column name" error from ALTER TABLE ADD COLUMN.
func isDuplicateColumnErr(err error) bool {
	if err == nil {
		return false
	}
	// modernc.org/sqlite returns this message.
	msg := err.Error()
	return len(msg) > 0 && (contains(msg, "duplicate column name") || contains(msg, "duplicate column"))
}

// contains is a simple substring check to avoid importing strings just for this.
func contains(s, substr string) bool {
	return len(substr) <= len(s) && searchSubstring(s, substr)
}

func searchSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
