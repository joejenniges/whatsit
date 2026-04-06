package audio

import (
	"sync"
	"time"
)

// Buffer is a thread-safe ring buffer that accumulates PCM float32 samples.
// When the buffer exceeds maxSize, the oldest samples are discarded.
type Buffer struct {
	mu      sync.Mutex
	data    []float32
	maxSize int
}

// NewBuffer creates a Buffer with the given maximum capacity in samples.
// If maxSize is 0 or negative, the buffer grows without limit.
func NewBuffer(maxSize int) *Buffer {
	return &Buffer{
		maxSize: maxSize,
		data:    make([]float32, 0, maxSize),
	}
}

// Write appends samples to the buffer. If the buffer would exceed maxSize,
// the oldest samples are dropped to make room.
func (b *Buffer) Write(samples []float32) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.data = append(b.data, samples...)

	// Trim from the front if we exceed maxSize.
	if b.maxSize > 0 && len(b.data) > b.maxSize {
		excess := len(b.data) - b.maxSize
		b.data = b.data[excess:]
	}
}

// Read returns and removes up to n samples from the front of the buffer.
// If fewer than n samples are available, all available samples are returned.
func (b *Buffer) Read(n int) []float32 {
	b.mu.Lock()
	defer b.mu.Unlock()

	if n > len(b.data) {
		n = len(b.data)
	}
	if n == 0 {
		return nil
	}

	out := make([]float32, n)
	copy(out, b.data[:n])
	b.data = b.data[n:]
	return out
}

// ReadAll returns and removes all samples from the buffer.
func (b *Buffer) ReadAll() []float32 {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.data) == 0 {
		return nil
	}

	out := make([]float32, len(b.data))
	copy(out, b.data)
	b.data = b.data[:0]
	return out
}

// Len returns the number of samples currently in the buffer.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.data)
}

// TrimFront removes the first n samples from the buffer.
// If n >= Len(), the buffer is cleared entirely.
func (b *Buffer) TrimFront(n int) {
	b.mu.Lock()
	defer b.mu.Unlock()

	if n >= len(b.data) {
		b.data = b.data[:0]
		return
	}
	b.data = b.data[n:]
}

// Clear removes all samples from the buffer.
func (b *Buffer) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = b.data[:0]
}

// Duration returns how much audio is in the buffer based on the given sample rate.
func (b *Buffer) Duration(sampleRate int) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()

	if sampleRate <= 0 {
		return 0
	}
	seconds := float64(len(b.data)) / float64(sampleRate)
	return time.Duration(seconds * float64(time.Second))
}
