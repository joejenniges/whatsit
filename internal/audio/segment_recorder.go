package audio

import (
	"fmt"
	"sync"
	"time"
)

// SegmentRecorder manages recording audio segments to WAV files.
// It tracks the current segment type (speech/music) and automatically
// closes the previous segment when the type changes.
//
// WHY int16 stereo instead of float32 mono: The original recorder saved
// post-resampled 16kHz mono float32 (whisper-ready audio). That sounds
// terrible for music playback. Now we save the pre-resampled stereo int16
// at the stream's native sample rate (typically 48kHz) for listenable output.
type SegmentRecorder struct {
	audioDir   string
	sampleRate int
	channels   int
	current    *WAVWriter
	segType    string // "speech" or "music"
	mu         sync.Mutex
	enabled    bool
}

// NewSegmentRecorder creates a SegmentRecorder that writes stereo int16 WAV
// files to audioDir using the given sample rate and channel count.
// When enabled is false, all operations are no-ops.
func NewSegmentRecorder(audioDir string, sampleRate, channels int, enabled bool) *SegmentRecorder {
	return &SegmentRecorder{
		audioDir:   audioDir,
		sampleRate: sampleRate,
		channels:   channels,
		enabled:    enabled,
	}
}

// SetEnabled toggles recording on or off at runtime.
func (r *SegmentRecorder) SetEnabled(enabled bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.enabled = enabled
}

// StartSegment begins recording a new segment. If a segment of a different
// type is already being recorded, it closes the old one first. If the same
// type is already active, this is a no-op and returns the current path.
// Returns the file path of the new (or existing) segment.
func (r *SegmentRecorder) StartSegment(segType string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.enabled {
		return "", nil
	}

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

	w, err := NewWAVWriter(path, r.sampleRate, r.channels, WAVFormatInt16)
	if err != nil {
		return "", fmt.Errorf("create segment wav: %w", err)
	}

	r.current = w
	r.segType = segType
	return path, nil
}

// WriteInt16 appends int16 samples to the current segment. Returns nil if
// no segment is active or recording is disabled.
func (r *SegmentRecorder) WriteInt16(samples []int16) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.enabled || r.current == nil {
		return nil // no active segment or disabled, silently skip
	}
	return r.current.WriteInt16(samples)
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
