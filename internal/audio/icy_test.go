//go:build integration

package audio

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestICYMetadata connects to the live stream and probes ICY metadata support.
// It logs all response headers, then reads for ~30 seconds, reporting every
// metadata change. Run with:
//
//	go test -tags integration -run TestICYMetadata -v -timeout 60s ./internal/audio/
func TestICYMetadata(t *testing.T) {
	const streamURL = "https://a6.asurahosting.com:8210/radio.mp3"
	const listenDuration = 30 * time.Second

	ctx, cancel := context.WithTimeout(context.Background(), listenDuration+10*time.Second)
	defer cancel()

	// --- Step 1: Connect with ICY header and dump ALL response headers ---
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("Icy-MetaData", "1")
	req.Header.Set("User-Agent", "ICYProbe/1.0")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	t.Logf("HTTP Status: %d %s", resp.StatusCode, resp.Status)
	t.Log("--- ALL RESPONSE HEADERS ---")
	for k, vals := range resp.Header {
		for _, v := range vals {
			t.Logf("  %s: %s", k, v)
		}
	}
	t.Log("--- END HEADERS ---")

	// Extract ICY-specific info
	icyName := resp.Header.Get("Icy-Name")
	if icyName == "" {
		icyName = resp.Header.Get("icy-name")
	}
	icyGenre := resp.Header.Get("Icy-Genre")
	if icyGenre == "" {
		icyGenre = resp.Header.Get("icy-genre")
	}
	icyBr := resp.Header.Get("Icy-Br")
	if icyBr == "" {
		icyBr = resp.Header.Get("icy-br")
	}
	icyMetaInt := parseIcyMetaInt(resp)

	t.Logf("Station Name: %q", icyName)
	t.Logf("Genre: %q", icyGenre)
	t.Logf("Bitrate: %s kbps", icyBr)
	t.Logf("MetaInt: %d bytes", icyMetaInt)

	if icyMetaInt <= 0 {
		t.Log("Server did NOT return icy-metaint. No inline metadata to parse.")
		t.Log("Reading 5 seconds of audio to confirm stream works...")
		buf := make([]byte, 4096)
		total := 0
		deadline := time.After(5 * time.Second)
		for {
			select {
			case <-deadline:
				t.Logf("Read %d bytes of audio data (no metadata available)", total)
				return
			default:
			}
			n, readErr := resp.Body.Read(buf)
			total += n
			if readErr != nil {
				t.Logf("Read error after %d bytes: %v", total, readErr)
				return
			}
		}
	}

	// --- Step 2: Read stream with ICY metadata interleaving for ~30s ---
	t.Logf("Listening for %v, metadata every %d audio bytes...", listenDuration, icyMetaInt)

	type metaEvent struct {
		at    time.Duration
		title string
		raw   string
	}
	var events []metaEvent
	start := time.Now()
	lastTitle := ""
	audioBytes := 0
	metaBlocks := 0
	emptyMetaBlocks := 0

	audioRemaining := icyMetaInt
	buf := make([]byte, 4096)

	deadline := time.After(listenDuration)

readLoop:
	for {
		select {
		case <-deadline:
			break readLoop
		case <-ctx.Done():
			break readLoop
		default:
		}

		// Read audio chunk
		toRead := audioRemaining
		if toRead > len(buf) {
			toRead = len(buf)
		}
		n, readErr := resp.Body.Read(buf[:toRead])
		audioBytes += n
		audioRemaining -= n

		if readErr != nil && readErr != io.EOF {
			t.Logf("Read error at %v: %v", time.Since(start), readErr)
			break
		}

		// Time for a metadata block?
		if audioRemaining <= 0 {
			metaBlocks++

			// Read length byte
			lenBuf := make([]byte, 1)
			if _, err := io.ReadFull(resp.Body, lenBuf); err != nil {
				t.Logf("Failed to read meta length byte: %v", err)
				break
			}
			metaLen := int(lenBuf[0]) * 16

			if metaLen == 0 {
				emptyMetaBlocks++
				audioRemaining = icyMetaInt
				continue
			}

			// Read metadata payload
			metaBuf := make([]byte, metaLen)
			if _, err := io.ReadFull(resp.Body, metaBuf); err != nil {
				t.Logf("Failed to read meta payload: %v", err)
				break
			}

			raw := string(metaBuf)
			raw = strings.TrimRight(raw, "\x00")
			title := parseStreamTitle(raw)

			// Always log non-empty metadata blocks for debugging
			t.Logf("[%6.1fs] META BLOCK (len=%d): %q", time.Since(start).Seconds(), metaLen, raw)
			t.Logf("         Parsed title: %q", title)

			if title != lastTitle {
				elapsed := time.Since(start)
				events = append(events, metaEvent{at: elapsed, title: title, raw: raw})
				t.Logf("[%6.1fs] METADATA CHANGE: %q", elapsed.Seconds(), title)
				t.Logf("         Raw: %q", raw)
				lastTitle = title
			}

			audioRemaining = icyMetaInt
		}

		if readErr == io.EOF {
			t.Log("Stream EOF")
			break
		}
	}

	// --- Step 3: Summary ---
	elapsed := time.Since(start)
	t.Log("")
	t.Log("========== ICY METADATA REPORT ==========")
	t.Logf("Stream URL:       %s", streamURL)
	t.Logf("Listen duration:  %v", elapsed.Round(time.Second))
	t.Logf("Audio bytes read: %d (%.0f kbps est.)", audioBytes, float64(audioBytes)*8/elapsed.Seconds()/1000)
	t.Logf("Station name:     %q", icyName)
	t.Logf("Genre:            %q", icyGenre)
	t.Logf("Bitrate (header): %s kbps", icyBr)
	t.Logf("MetaInt:          %d bytes", icyMetaInt)
	t.Logf("Meta blocks read: %d total (%d empty, %d with data)", metaBlocks, emptyMetaBlocks, metaBlocks-emptyMetaBlocks)
	t.Logf("Title changes:    %d", len(events))
	t.Log("")
	if len(events) == 0 {
		t.Log("No StreamTitle changes observed in the listen window.")
		t.Log("This could mean: stream sends no title, or title didn't change during the window.")
	} else {
		t.Log("Title change timeline:")
		for _, e := range events {
			t.Logf("  [%6.1fs] %q", e.at.Seconds(), e.title)
		}
	}
	t.Log("")

	// --- Step 4: Validate existing parseStreamTitle ---
	t.Log("=== parseStreamTitle validation ===")
	testCases := []struct {
		input    string
		expected string
	}{
		{"StreamTitle='Artist - Song';", "Artist - Song"},
		{"StreamTitle='';", ""},
		{"StreamTitle='Test';StreamUrl='http://example.com';", "Test"},
		{"StreamTitle='It\\'s a test';", "It\\'s a test"}, // edge case: escaped quote
	}
	for _, tc := range testCases {
		got := parseStreamTitle(tc.input)
		status := "OK"
		if got != tc.expected {
			status = fmt.Sprintf("FAIL (got %q)", got)
		}
		t.Logf("  parseStreamTitle(%q) = %q  [%s]", tc.input, got, status)
	}
	t.Log("==========================================")
}
