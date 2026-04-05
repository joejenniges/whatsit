package logging

import (
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joe/radio-transcriber/internal/config"
)

const (
	logSubdir    = "logs"
	logPrefix    = "radio-transcriber-"
	logSuffix    = ".log"
	logRetention = 7 * 24 * time.Hour
)

// Setup configures the standard log package to write to a date-stamped
// log file under %APPDATA%/RadioTranscriber/logs/, while also teeing
// output to stderr for development visibility.
//
// The caller should defer the returned file's Close method.
// Old log files (>7 days) are cleaned up as a side effect.
func Setup() (*os.File, error) {
	appDir, err := config.AppDir()
	if err != nil {
		return nil, fmt.Errorf("logging: resolve app dir: %w", err)
	}

	logsDir := filepath.Join(appDir, logSubdir)
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		return nil, fmt.Errorf("logging: create logs dir: %w", err)
	}

	filename := logPrefix + time.Now().Format("2006-01-02") + logSuffix
	path := filepath.Join(logsDir, filename)

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("logging: open log file: %w", err)
	}

	log.SetOutput(io.MultiWriter(f, os.Stderr))
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Clean up old logs in the background so it doesn't slow startup.
	go cleanOldLogs(logsDir)

	return f, nil
}

// cleanOldLogs removes log files older than logRetention from dir.
// Errors are silently ignored -- failing to delete stale logs is not
// worth crashing over.
func cleanOldLogs(dir string) {
	cutoff := time.Now().Add(-logRetention)

	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		if !strings.HasPrefix(name, logPrefix) || !strings.HasSuffix(name, logSuffix) {
			continue
		}

		// Parse the date from the filename rather than using file
		// modtime. This way renaming/copying a file doesn't defeat
		// the retention policy.
		dateStr := strings.TrimPrefix(name, logPrefix)
		dateStr = strings.TrimSuffix(dateStr, logSuffix)

		fileDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			continue
		}

		if fileDate.Before(cutoff) {
			_ = os.Remove(filepath.Join(dir, name))
		}
	}
}
