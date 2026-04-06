package audio

import (
	"log"
	"os"
	"path/filepath"
	"time"
)

// CleanupOldAudio deletes WAV files older than maxAge from audioDir.
// Errors on individual files are logged but do not stop the cleanup.
func CleanupOldAudio(audioDir string, maxAge time.Duration) {
	cutoff := time.Now().Add(-maxAge)

	entries, err := os.ReadDir(audioDir)
	if err != nil {
		if os.IsNotExist(err) {
			return // nothing to clean
		}
		log.Printf("audio: cleanup read dir: %v", err)
		return
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".wav" {
			continue
		}

		info, err := entry.Info()
		if err != nil {
			log.Printf("audio: cleanup stat %s: %v", entry.Name(), err)
			continue
		}

		if info.ModTime().Before(cutoff) {
			path := filepath.Join(audioDir, entry.Name())
			if err := os.Remove(path); err != nil {
				log.Printf("audio: cleanup remove %s: %v", entry.Name(), err)
			} else {
				log.Printf("audio: cleaned up old file: %s", entry.Name())
			}
		}
	}
}
