package audio

import (
	"encoding/binary"
	"io"
	"log"
	"runtime"
	"sync"

	"github.com/ebitengine/oto/v3"
)

// Listener plays decoded stereo int16 PCM audio through the system speakers
// using oto. It can be toggled on/off at any time. When off, Write calls are
// no-ops. When on, samples are written to the oto player for playback.
//
// WHY dedicated goroutine: oto uses WASAPI on Windows which requires COM
// initialization on the thread that creates the context. Wails/WebView2 also
// uses COM on the main thread. Running oto on its own OS-locked goroutine
// avoids conflicts.
type Listener struct {
	mu      sync.Mutex
	enabled bool
	feeder  *int16Feeder

	// Commands sent to the oto goroutine.
	cmdCh chan listenCmd
	done  chan struct{}
}

type listenCmdType int

const (
	cmdEnable listenCmdType = iota
	cmdDisable
	cmdClose
)

type listenCmd struct {
	typ listenCmdType
}

// NewListener creates a Listener. The oto context and player are managed on
// a dedicated OS-locked goroutine to avoid COM threading conflicts with WebView2.
func NewListener(sampleRate, channels int) (*Listener, error) {
	l := &Listener{
		cmdCh: make(chan listenCmd, 8),
		done:  make(chan struct{}),
	}

	errCh := make(chan error, 1)

	go func() {
		// Lock this goroutine to a single OS thread for COM/WASAPI.
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		op := &oto.NewContextOptions{
			SampleRate:   sampleRate,
			ChannelCount: channels,
			Format:       oto.FormatSignedInt16LE,
		}

		otoCtx, readyChan, err := oto.NewContext(op)
		if err != nil {
			errCh <- err
			return
		}
		<-readyChan
		log.Printf("listener: oto context created on dedicated thread, sampleRate=%d channels=%d", sampleRate, channels)
		errCh <- nil

		var player *oto.Player

		defer func() {
			if player != nil {
				player.Close()
			}
			close(l.done)
		}()

		for cmd := range l.cmdCh {
			switch cmd.typ {
			case cmdEnable:
				if player != nil {
					player.Close()
					player = nil
				}
				l.mu.Lock()
				feeder := l.feeder
				l.mu.Unlock()
				if feeder == nil {
					continue
				}
				player = otoCtx.NewPlayer(feeder)
				player.Play()
				log.Printf("listener: player created and playing")

			case cmdDisable:
				if player != nil {
					player.Pause()
					player.Close()
					player = nil
					log.Printf("listener: player stopped")
				}

			case cmdClose:
				return
			}
		}
	}()

	// Wait for oto initialization.
	if err := <-errCh; err != nil {
		return nil, err
	}

	return l, nil
}

// SetEnabled toggles audio playback.
func (l *Listener) SetEnabled(enabled bool) {
	l.mu.Lock()
	if l.enabled == enabled {
		l.mu.Unlock()
		return
	}
	l.enabled = enabled

	if enabled {
		l.feeder = newInt16Reader()
		l.mu.Unlock()
		log.Printf("listener: SetEnabled(true)")
		l.cmdCh <- listenCmd{typ: cmdEnable}
	} else {
		feeder := l.feeder
		l.feeder = nil
		l.mu.Unlock()
		log.Printf("listener: SetEnabled(false)")
		if feeder != nil {
			feeder.Stop()
		}
		l.cmdCh <- listenCmd{typ: cmdDisable}
	}
}

// Enabled returns whether listening is currently on.
func (l *Listener) Enabled() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.enabled
}

// Write sends stereo int16 samples to the audio output. If listening is
// disabled, this is a no-op.
func (l *Listener) Write(samples []int16) {
	l.mu.Lock()
	feeder := l.feeder
	enabled := l.enabled
	l.mu.Unlock()

	if !enabled || feeder == nil {
		return
	}

	// Convert int16 samples to little-endian bytes for oto.
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}

	feeder.Feed(buf)
}

// Close releases oto resources and shuts down the oto goroutine.
func (l *Listener) Close() {
	l.mu.Lock()
	if l.feeder != nil {
		l.feeder.Stop()
		l.feeder = nil
	}
	l.enabled = false
	l.mu.Unlock()

	l.cmdCh <- listenCmd{typ: cmdClose}
	<-l.done // wait for oto goroutine to exit
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
