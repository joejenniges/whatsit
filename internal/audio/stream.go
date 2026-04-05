package audio

import (
	"context"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Streamer connects to an MP3 stream URL via HTTP and continuously reads
// MP3 frame data into an output channel. It handles reconnection with
// exponential backoff and parses ICY metadata if the server supports it.
type Streamer struct {
	url    string
	ctx    context.Context
	cancel context.CancelFunc
	output chan []byte

	// OnMetadata is called when ICY metadata (e.g. stream title) is received.
	// May be called from a background goroutine.
	OnMetadata func(title string)

	once sync.Once
}

// NewStreamer creates a Streamer that will connect to the given MP3 stream URL.
// The provided context controls the lifetime of the streamer.
func NewStreamer(ctx context.Context, url string) *Streamer {
	ctx, cancel := context.WithCancel(ctx)
	return &Streamer{
		url:    url,
		ctx:    ctx,
		cancel: cancel,
		output: make(chan []byte, 64),
	}
}

// Start begins streaming in a background goroutine. It returns an error only
// if the initial connection fails. Subsequent disconnections are handled
// internally with exponential backoff reconnection.
func (s *Streamer) Start() error {
	resp, err := s.connect()
	if err != nil {
		return fmt.Errorf("initial connection failed: %w", err)
	}

	go s.readLoop(resp)
	return nil
}

// Output returns the channel of raw MP3 byte chunks read from the stream.
func (s *Streamer) Output() <-chan []byte {
	return s.output
}

// Stop cancels the streaming context and closes the output channel.
func (s *Streamer) Stop() {
	s.cancel()
	// output channel is closed by readLoop when ctx is done
}

func (s *Streamer) connect() (*http.Response, error) {
	req, err := http.NewRequestWithContext(s.ctx, http.MethodGet, s.url, nil)
	if err != nil {
		return nil, err
	}
	// Request ICY metadata from Shoutcast/Icecast servers.
	req.Header.Set("Icy-MetaData", "1")
	req.Header.Set("User-Agent", "RadioTranscriber/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("unexpected status: %d", resp.StatusCode)
	}
	return resp, nil
}

// readLoop continuously reads from the HTTP response body and pushes MP3 data
// to the output channel. On error it reconnects with exponential backoff.
func (s *Streamer) readLoop(resp *http.Response) {
	defer func() {
		s.once.Do(func() { close(s.output) })
	}()

	icyMetaInt := parseIcyMetaInt(resp)
	s.streamBody(resp.Body, icyMetaInt)
	resp.Body.Close()

	// Reconnect loop with exponential backoff.
	backoff := 1 * time.Second
	const maxBackoff = 30 * time.Second

	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		log.Printf("stream: reconnecting in %v", backoff)
		select {
		case <-s.ctx.Done():
			return
		case <-time.After(backoff):
		}

		resp, err := s.connect()
		if err != nil {
			log.Printf("stream: reconnect failed: %v", err)
			backoff = time.Duration(math.Min(float64(backoff*2), float64(maxBackoff)))
			continue
		}

		// Reset backoff on successful connection.
		backoff = 1 * time.Second
		icyMetaInt = parseIcyMetaInt(resp)
		s.streamBody(resp.Body, icyMetaInt)
		resp.Body.Close()
	}
}

// streamBody reads from the body, handling ICY metadata interleaving if
// icyMetaInt > 0. It returns when the body errors or the context is cancelled.
func (s *Streamer) streamBody(body io.Reader, icyMetaInt int) {
	// Read buffer size: 4096 bytes is a reasonable chunk for MP3 frames.
	const readSize = 4096

	if icyMetaInt <= 0 {
		// No ICY metadata, just read raw MP3 data.
		buf := make([]byte, readSize)
		for {
			select {
			case <-s.ctx.Done():
				return
			default:
			}
			n, err := body.Read(buf)
			if n > 0 {
				chunk := make([]byte, n)
				copy(chunk, buf[:n])
				select {
				case s.output <- chunk:
				case <-s.ctx.Done():
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("stream: read error: %v", err)
				}
				return
			}
		}
	}

	// ICY metadata interleaving: every icyMetaInt bytes of audio data,
	// there is a metadata block. The first byte of the metadata block
	// is the length divided by 16.
	audioRemaining := icyMetaInt
	buf := make([]byte, readSize)
	for {
		select {
		case <-s.ctx.Done():
			return
		default:
		}

		toRead := audioRemaining
		if toRead > readSize {
			toRead = readSize
		}

		n, err := body.Read(buf[:toRead])
		if n > 0 {
			audioRemaining -= n
			chunk := make([]byte, n)
			copy(chunk, buf[:n])
			select {
			case s.output <- chunk:
			case <-s.ctx.Done():
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				log.Printf("stream: read error: %v", err)
			}
			return
		}

		// If we've read all audio bytes before the next metadata block,
		// read the metadata.
		if audioRemaining <= 0 {
			s.readIcyMetadata(body)
			audioRemaining = icyMetaInt
		}
	}
}

// readIcyMetadata reads a single ICY metadata block from the stream.
func (s *Streamer) readIcyMetadata(r io.Reader) {
	// First byte: metadata length = value * 16
	lenBuf := make([]byte, 1)
	if _, err := io.ReadFull(r, lenBuf); err != nil {
		return
	}
	metaLen := int(lenBuf[0]) * 16
	if metaLen == 0 {
		return
	}

	metaBuf := make([]byte, metaLen)
	if _, err := io.ReadFull(r, metaBuf); err != nil {
		return
	}

	// Parse StreamTitle from the metadata.
	// Format: StreamTitle='Artist - Title';StreamUrl='...';
	meta := string(metaBuf)
	meta = strings.TrimRight(meta, "\x00")
	if title := parseStreamTitle(meta); title != "" && s.OnMetadata != nil {
		s.OnMetadata(title)
	}
}

// parseIcyMetaInt extracts the icy-metaint value from the HTTP response headers.
// Returns 0 if not present.
func parseIcyMetaInt(resp *http.Response) int {
	val := resp.Header.Get("Icy-Metaint")
	if val == "" {
		// Some servers use lowercase.
		val = resp.Header.Get("icy-metaint")
	}
	if val == "" {
		return 0
	}
	n, err := strconv.Atoi(val)
	if err != nil {
		return 0
	}
	return n
}

// parseStreamTitle extracts the StreamTitle value from an ICY metadata string.
func parseStreamTitle(meta string) string {
	const prefix = "StreamTitle='"
	idx := strings.Index(meta, prefix)
	if idx < 0 {
		return ""
	}
	start := idx + len(prefix)
	end := strings.Index(meta[start:], "';")
	if end < 0 {
		// Try without semicolon (end of metadata).
		end = strings.Index(meta[start:], "'")
		if end < 0 {
			return ""
		}
	}
	return meta[start : start+end]
}
