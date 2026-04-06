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
	crashPrefix  = "crash-"
	logRetention = 7 * 24 * time.Hour
)

// SetupResult holds the log file and directory path returned by Setup.
type SetupResult struct {
	File    *os.File
	LogsDir string
}

// Setup configures the standard log package to write to a date-stamped
// log file under %APPDATA%/RadioTranscriber/logs/. Also redirects stderr
// to the same file so that Go runtime panics (which write directly to fd 2)
// are captured instead of vanishing with -H windowsgui.
//
// The caller should defer result.File.Close().
// Old log files (>7 days) are cleaned up as a side effect.
func Setup() (*SetupResult, error) {
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

	// Redirect stderr to the log file BEFORE setting up log output.
	// This captures Go runtime panics from any goroutine.
	if err := redirectStderr(f); err != nil {
		// Non-fatal: we still get log.* output, just not raw panics.
		fmt.Fprintf(f, "logging: redirect stderr failed: %v\n", err)
	}

	log.SetOutput(io.MultiWriter(f, os.Stderr))
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)

	// Clean up old logs in the background so it doesn't slow startup.
	go cleanOldLogs(logsDir)

	return &SetupResult{File: f, LogsDir: logsDir}, nil
}

// WriteCrashReport writes a crash report file with the given error and stack
// trace. Returns the path to the crash file.
func WriteCrashReport(logsDir string, panicVal interface{}, stack []byte) string {
	ts := time.Now().Format("2006-01-02-150405")
	filename := crashPrefix + ts + ".log"
	path := filepath.Join(logsDir, filename)

	content := fmt.Sprintf("RadioTranscriber Crash Report\n"+
		"Time: %s\n"+
		"Panic: %v\n"+
		"\nStack trace:\n%s\n",
		time.Now().Format(time.RFC3339),
		panicVal,
		string(stack),
	)

	_ = os.WriteFile(path, []byte(content), 0o644)
	return path
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
