package audio

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log"
	"sync"

	"github.com/hajimehoshi/go-mp3"
)

// Decoder reads raw MP3 byte chunks from an input channel, decodes them to
// signed 16-bit stereo PCM, and outputs []int16 slices representing 2-second
// chunks of audio.
type Decoder struct {
	input      <-chan []byte
	output     chan []int16
	sampleRate int

	mu sync.RWMutex
}

// NewDecoder creates a Decoder that reads MP3 data from the given input channel.
func NewDecoder(input <-chan []byte) *Decoder {
	return &Decoder{
		input:  input,
		output: make(chan []int16, 32),
	}
}

// Start begins decoding in a background goroutine. It blocks briefly to
// initialize the MP3 decoder and detect the sample rate, then returns.
// Returns an error if the decoder cannot be initialized.
func (d *Decoder) Start(ctx context.Context) error {
	// chanReader bridges the []byte channel into an io.Reader that the
	// mp3 decoder can consume. It blocks on channel reads, providing
	// natural backpressure from the stream.
	reader := newChanReader(ctx, d.input)

	// go-mp3 needs to read some frames to initialize (detect sample rate, etc).
	decoder, err := mp3.NewDecoder(reader)
	if err != nil {
		return err
	}

	d.mu.Lock()
	d.sampleRate = decoder.SampleRate()
	d.mu.Unlock()

	go d.decodeLoop(ctx, decoder)
	return nil
}

// Output returns the channel of decoded PCM int16 slices.
func (d *Decoder) Output() <-chan []int16 {
	return d.output
}

// SampleRate returns the sample rate detected from the MP3 stream.
// Only valid after Start returns successfully.
func (d *Decoder) SampleRate() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.sampleRate
}

// decodeLoop reads decoded PCM from the mp3 decoder in 2-second chunks
// and sends them to the output channel.
func (d *Decoder) decodeLoop(ctx context.Context, decoder *mp3.Decoder) {
	defer close(d.output)

	sr := d.SampleRate()
	// 2 seconds of stereo 16-bit PCM.
	// samples per channel per second = sampleRate
	// stereo = 2 channels, 2 bytes per sample
	chunkBytes := sr * 2 * 2 * 2 // sampleRate * channels * bytesPerSample * seconds
	buf := make([]byte, chunkBytes)

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		n, err := io.ReadFull(decoder, buf)
		if n > 0 {
			// Convert raw bytes to int16 samples.
			// go-mp3 outputs little-endian signed 16-bit stereo interleaved.
			samples := bytesToInt16(buf[:n])
			select {
			case d.output <- samples:
			case <-ctx.Done():
				return
			}
		}
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				// Stream ended or partial read at the end.
				log.Printf("decoder: stream ended")
				return
			}
			log.Printf("decoder: read error: %v", err)
			return
		}
	}
}

// bytesToInt16 converts a byte slice of little-endian int16 pairs into []int16.
func bytesToInt16(data []byte) []int16 {
	// Ensure even number of bytes.
	n := len(data) / 2
	samples := make([]int16, n)
	reader := bytes.NewReader(data[:n*2])
	_ = binary.Read(reader, binary.LittleEndian, samples)
	return samples
}

// chanReader adapts a <-chan []byte into an io.Reader. It buffers partial
// reads from chunks that are larger than what the caller requested.
type chanReader struct {
	ctx    context.Context
	ch     <-chan []byte
	buf    []byte
	offset int
}

func newChanReader(ctx context.Context, ch <-chan []byte) *chanReader {
	return &chanReader{ctx: ctx, ch: ch}
}

func (r *chanReader) Read(p []byte) (int, error) {
	// If we have buffered data from a previous chunk, use it first.
	if r.offset < len(r.buf) {
		n := copy(p, r.buf[r.offset:])
		r.offset += n
		if r.offset >= len(r.buf) {
			r.buf = nil
			r.offset = 0
		}
		return n, nil
	}

	// Wait for the next chunk from the channel.
	select {
	case <-r.ctx.Done():
		return 0, r.ctx.Err()
	case chunk, ok := <-r.ch:
		if !ok {
			return 0, io.EOF
		}
		n := copy(p, chunk)
		if n < len(chunk) {
			// Buffer the remainder for the next Read call.
			r.buf = chunk
			r.offset = n
		}
		return n, nil
	}
}
