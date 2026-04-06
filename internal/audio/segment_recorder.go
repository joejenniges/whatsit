package audio

import (
	"fmt"
	"sync"
	"time"
)

// SegmentRecorder manages recording audio segments to WAV files.
// It tracks the current segment type (speech/music) and automatically
// closes the previous segment when the type changes.
type SegmentRecorder struct {
	audioDir   string
	current    *WAVWriter
	segType    string // "speech" or "music"
	sampleRate int
	mu         sync.Mutex
}

// NewSegmentRecorder creates a SegmentRecorder that writes WAV files
// to audioDir using the given sample rate.
func NewSegmentRecorder(audioDir string, sampleRate int) *SegmentRecorder {
	return &SegmentRecorder{
		audioDir:   audioDir,
		sampleRate: sampleRate,
	}
}

// StartSegment begins recording a new segment. If a segment of a different
// type is already being recorded, it closes the old one first. If the same
// type is already active, this is a no-op and returns the current path.
// Returns the file path of the new (or existing) segment.
func (r *SegmentRecorder) StartSegment(segType string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Same type already recording -- keep going.
	if r.current != nil && r.segType == segType {
		return r.current.Path(), nil
	}

	// Different type or no current segment -- close the old one.
	if r.current != nil {
		if err := r.current.Close(); err != nil {
			return "", fmt.Errorf("close previous segment: %w", err)
		}
		r.current = nil
	}

	// Generate filename: segType_YYYY-MM-DD_HHMMSS.wav
	now := time.Now()
	filename := fmt.Sprintf("%s_%s.wav", segType, now.Format("2006-01-02_150405"))
	path := fmt.Sprintf("%s/%s", r.audioDir, filename)

	w, err := NewWAVWriter(path, r.sampleRate, 1)
	if err != nil {
		return "", fmt.Errorf("create segment wav: %w", err)
	}

	r.current = w
	r.segType = segType
	return path, nil
}

// Write appends samples to the current segment. Returns an error if no
// segment is active.
func (r *SegmentRecorder) Write(samples []float32) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.current == nil {
		return nil // no active segment, silently skip
	}
	return r.current.Write(samples)
}

// EndSegment closes the current segment file and returns its path.
// Returns ("", nil) if no segment is active.
func (r *SegmentRecorder) EndSegment() (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.current == nil {
		return "", nil
	}

	path := r.current.Path()
	if err := r.current.Close(); err != nil {
		return "", fmt.Errorf("close segment: %w", err)
	}
	r.current = nil
	r.segType = ""
	return path, nil
}

// CurrentPath returns the path of the current segment being recorded,
// or "" if no segment is active.
func (r *SegmentRecorder) CurrentPath() string {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.current == nil {
		return ""
	}
	return r.current.Path()
}
