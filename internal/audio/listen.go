package audio

import (
	"encoding/binary"
	"io"
	"log"
	"sync"

	"github.com/ebitengine/oto/v3"
)

// Listener plays decoded stereo int16 PCM audio through the system speakers
// using oto. It can be toggled on/off at any time. When off, Write calls are
// no-ops. When on, samples are written to the oto player for playback.
type Listener struct {
	mu      sync.Mutex
	enabled bool

	otoCtx *oto.Context
	player *oto.Player
	feeder *int16Feeder

	sampleRate int
	channels   int
}

// NewListener creates a Listener for the given sample rate and channel count.
// The oto context is initialized eagerly so that toggling on is fast.
func NewListener(sampleRate, channels int) (*Listener, error) {
	op := &oto.NewContextOptions{
		SampleRate:   sampleRate,
		ChannelCount: channels,
		Format:       oto.FormatSignedInt16LE,
	}

	otoCtx, readyChan, err := oto.NewContext(op)
	if err != nil {
		return nil, err
	}
	// Wait for oto to be ready.
	<-readyChan

	return &Listener{
		otoCtx:     otoCtx,
		sampleRate: sampleRate,
		channels:   channels,
	}, nil
}

// SetEnabled toggles audio playback. When switching from enabled to disabled,
// the current player is paused and reset. When switching to enabled, a new
// feeder and player are created.
func (l *Listener) SetEnabled(enabled bool) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.enabled == enabled {
		return
	}
	l.enabled = enabled

	if enabled {
		// Create a fresh feeder and player each time listening is enabled.
		// WHY: Reusing a paused player can have stale buffered audio from
		// the last listen session, causing a burst of old audio on re-enable.
		l.feeder = newInt16Reader()
		l.player = l.otoCtx.NewPlayer(l.feeder)
		l.player.Play()
	} else if l.player != nil {
		// Signal the feeder to unblock its Read() before closing the player.
		// WHY: oto's player goroutine may be blocked in feeder.Read(). If we
		// close without unblocking, it deadlocks or panics.
		l.feeder.Stop()
		l.player.Pause()
		if err := l.player.Close(); err != nil {
			log.Printf("listener: close player: %v", err)
		}
		l.player = nil
		l.feeder = nil
	}
}

// Enabled returns whether listening is currently on.
func (l *Listener) Enabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled
}

// Write sends stereo int16 samples to the audio output. If listening is
// disabled, this is a no-op. The samples slice is not modified.
func (l *Listener) Write(samples []int16) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if !l.enabled || l.feeder == nil {
		return
	}

	// Convert int16 samples to little-endian bytes for oto.
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}

	l.feeder.Feed(buf)
}

// Close releases oto resources.
func (l *Listener) Close() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.player != nil {
		if l.feeder != nil {
			l.feeder.Stop()
		}
		l.player.Pause()
		if err := l.player.Close(); err != nil {
			log.Printf("listener: close player: %v", err)
		}
		l.player = nil
		l.feeder = nil
	}
}

// int16Feeder is an io.Reader that is fed byte slices by the audio pipeline.
// The oto player reads from it in its own goroutine.
type int16Feeder struct {
	mu   sync.Mutex
	cond *sync.Cond
	buf  []byte
	done bool
}

func newInt16Reader() *int16Feeder {
	f := &int16Feeder{}
	f.cond = sync.NewCond(&f.mu)
	return f
}

// Feed appends audio bytes to the internal buffer.
func (f *int16Feeder) Feed(data []byte) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.done {
		return
	}
	f.buf = append(f.buf, data...)
	f.cond.Signal()
}

// Stop signals the feeder to unblock any Read() calls and stop accepting data.
func (f *int16Feeder) Stop() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.done = true
	f.cond.Broadcast()
}

// Read implements io.Reader for oto's player.
func (f *int16Feeder) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for len(f.buf) == 0 && !f.done {
		f.cond.Wait()
	}

	if len(f.buf) == 0 && f.done {
		return 0, io.EOF
	}

	n := copy(p, f.buf)
	f.buf = f.buf[n:]
	return n, nil
}
