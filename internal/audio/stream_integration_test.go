//go:build integration

package audio

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

const testStreamURL = "https://a6.asurahosting.com:8210/radio.mp3"

// TestStreamIntegration connects to a live MP3 stream and verifies it produces
// valid audio data with ICY metadata support.
func TestStreamIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// --- Sub-test 1: Raw HTTP probe for headers ---
	t.Run("headers", func(t *testing.T) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, testStreamURL, nil)
		if err != nil {
			t.Fatalf("failed to create request: %v", err)
		}
		req.Header.Set("Icy-MetaData", "1")
		req.Header.Set("User-Agent", "RadioTranscriber/1.0-test")

		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("failed to connect to stream: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected status code: %d", resp.StatusCode)
		}

		// Dump interesting headers
		for _, key := range []string{
			"Content-Type",
			"Icy-Metaint",
			"icy-metaint",
			"Icy-Name",
			"icy-name",
			"Icy-Genre",
			"icy-genre",
			"Icy-Br",
			"icy-br",
			"Icy-Sr",
			"icy-sr",
			"Icy-Description",
			"icy-description",
			"Ice-Audio-Info",
			"ice-audio-info",
		} {
			if v := resp.Header.Get(key); v != "" {
				t.Logf("Header %s: %s", key, v)
			}
		}

		// Also log all headers with icy/ice prefix for discovery
		for k, v := range resp.Header {
			lower := strings.ToLower(k)
			if strings.HasPrefix(lower, "icy") || strings.HasPrefix(lower, "ice") {
				t.Logf("  [all] %s: %s", k, strings.Join(v, ", "))
			}
		}

		contentType := resp.Header.Get("Content-Type")
		if contentType == "" {
			t.Log("WARNING: no Content-Type header")
		} else {
			t.Logf("Content-Type: %s", contentType)
		}

		icyMetaInt := parseIcyMetaInt(resp)
		if icyMetaInt > 0 {
			t.Logf("ICY metadata interval: %d bytes", icyMetaInt)
		} else {
			t.Log("No ICY metadata interval reported by server")
		}

		// Read a small amount of data to verify the body is producing bytes
		buf := make([]byte, 8192)
		n, err := resp.Body.Read(buf)
		if err != nil {
			t.Fatalf("failed to read from stream body: %v", err)
		}
		if n == 0 {
			t.Fatal("read zero bytes from stream")
		}
		t.Logf("Initial read: %d bytes", n)

		// Check for MP3 sync word (0xFF 0xFB, 0xFF 0xFA, or 0xFF 0xF3 etc.)
		// The first byte of an MP3 frame is always 0xFF, second has top 3 bits set.
		foundSync := false
		for i := 0; i < n-1; i++ {
			if buf[i] == 0xFF && (buf[i+1]&0xE0) == 0xE0 {
				foundSync = true
				t.Logf("Found MP3 sync word at offset %d (bytes: 0x%02X 0x%02X)", i, buf[i], buf[i+1])
				// Decode MPEG version and layer from the sync header
				version := (buf[i+1] >> 3) & 0x03
				layer := (buf[i+1] >> 1) & 0x03
				versionStr := map[byte]string{0: "MPEG2.5", 2: "MPEG2", 3: "MPEG1"}[version]
				layerStr := map[byte]string{1: "Layer III", 2: "Layer II", 3: "Layer I"}[layer]
				t.Logf("Detected: %s %s", versionStr, layerStr)
				break
			}
		}
		if !foundSync {
			t.Error("no MP3 sync word found in initial data")
		}
	})

	// --- Sub-test 2: Streamer API produces data and captures metadata ---
	t.Run("streamer", func(t *testing.T) {
		streamerCtx, streamerCancel := context.WithTimeout(ctx, 8*time.Second)
		defer streamerCancel()

		s := NewStreamer(streamerCtx, testStreamURL)

		var metadataTitles []string
		s.OnMetadata = func(title string) {
			metadataTitles = append(metadataTitles, title)
		}

		if err := s.Start(); err != nil {
			t.Fatalf("Streamer.Start() failed: %v", err)
		}
		defer s.Stop()

		// Read for ~3 seconds and count bytes
		readCtx, readCancel := context.WithTimeout(streamerCtx, 3*time.Second)
		defer readCancel()

		totalBytes := 0
		chunks := 0
		for {
			select {
			case data, ok := <-s.Output():
				if !ok {
					t.Log("Output channel closed")
					goto done
				}
				totalBytes += len(data)
				chunks++
			case <-readCtx.Done():
				goto done
			}
		}
	done:

		t.Logf("Read %d bytes in %d chunks over ~3 seconds", totalBytes, chunks)

		if totalBytes == 0 {
			t.Fatal("Streamer produced zero bytes")
		}

		// At 128kbps, 3 seconds = ~48KB. Allow wide margin.
		expectedMin := 10_000 // very conservative lower bound
		if totalBytes < expectedMin {
			t.Errorf("expected at least %d bytes, got %d", expectedMin, totalBytes)
		}

		// Estimate bitrate
		bitrateKbps := float64(totalBytes) * 8 / 3000 // bits per ms -> kbps
		t.Logf("Estimated bitrate: %.0f kbps", bitrateKbps)

		if len(metadataTitles) > 0 {
			t.Logf("ICY metadata received (%d titles):", len(metadataTitles))
			for i, title := range metadataTitles {
				t.Logf("  [%d] %s", i, title)
			}
		} else {
			t.Log("No ICY metadata titles received during test window (may need longer stream time)")
		}

		fmt.Printf("\n=== STREAM SUMMARY ===\n")
		fmt.Printf("URL: %s\n", testStreamURL)
		fmt.Printf("Total bytes (3s): %d\n", totalBytes)
		fmt.Printf("Estimated bitrate: %.0f kbps\n", bitrateKbps)
		fmt.Printf("Metadata titles: %d\n", len(metadataTitles))
		if len(metadataTitles) > 0 {
			fmt.Printf("Current title: %s\n", metadataTitles[len(metadataTitles)-1])
		}
		fmt.Printf("======================\n")
	})
}
