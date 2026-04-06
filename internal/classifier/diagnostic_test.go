//go:build diagnostic

package classifier

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/hajimehoshi/go-mp3"
)

// TestDiagnostic connects to a live radio stream, decodes + resamples to
// 16 kHz mono, then classifies 30 seconds of 2-second chunks. It prints
// every feature value so we can see what real compressed radio audio looks
// like and tune thresholds accordingly.
//
// WHY self-contained (no audio package import): the audio package has a
// listen.go with an oto dependency that may not compile on all setups.
// Inlining the decode/resample here avoids that build issue.
func TestDiagnostic(t *testing.T) {
	const streamURL = "https://a6.asurahosting.com:8210/radio.mp3"
	const duration = 30 * time.Second
	const chunkSamples = 32000 // 2s at 16 kHz
	const targetRate = 16000

	ctx, cancel := context.WithTimeout(context.Background(), duration+15*time.Second)
	defer cancel()

	// Connect to stream
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, streamURL, nil)
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	req.Header.Set("User-Agent", "RadioTranscriber/diagnostic")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	// Decode MP3
	decoder, err := mp3.NewDecoder(resp.Body)
	if err != nil {
		t.Fatalf("mp3 decoder: %v", err)
	}

	srcRate := decoder.SampleRate()
	log.Printf("Stream sample rate: %d Hz", srcRate)

	// Read, decode, resample, classify
	c := NewLegacyClassifier()
	c.SetDebounce(1) // no debounce for raw feature analysis

	fmt.Println()
	fmt.Printf("%-6s %-8s %-8s %-10s %-12s %-14s %-10s %-10s\n",
		"Chunk", "RMS", "ZCR", "Centroid", "Flux", "EnergyVar", "Score", "Class")
	fmt.Println("------ -------- -------- ---------- ------------ -------------- ---------- ----------")

	startTime := time.Now()
	chunkNum := 0

	// We need enough stereo int16 data to produce chunkSamples of 16kHz mono.
	// stereo frames at srcRate that produce chunkSamples at targetRate:
	stereoFramesNeeded := int(float64(chunkSamples) * float64(srcRate) / float64(targetRate))
	readBytes := stereoFramesNeeded * 2 * 2 // stereo * 2 bytes per sample
	rawBuf := make([]byte, readBytes)

	for {
		if time.Since(startTime) >= duration {
			fmt.Println("\n30 seconds captured.")
			return
		}

		n, err := io.ReadFull(decoder, rawBuf)
		if n > 0 {
			// Convert bytes to int16
			stereoSamples := make([]int16, n/2)
			reader := bytes.NewReader(rawBuf[:n])
			_ = binary.Read(reader, binary.LittleEndian, stereoSamples)

			// Stereo to mono float32
			numFrames := len(stereoSamples) / 2
			mono := make([]float32, numFrames)
			for i := 0; i < numFrames; i++ {
				left := float32(stereoSamples[i*2])
				right := float32(stereoSamples[i*2+1])
				mono[i] = (left + right) / 2.0 / 32768.0
			}

			// Resample to 16kHz
			resampled := resampleLinearDiag(mono, srcRate, targetRate)

			// Trim or pad to exact chunk size
			if len(resampled) > chunkSamples {
				resampled = resampled[:chunkSamples]
			}
			if len(resampled) < chunkSamples {
				continue // not enough data
			}

			chunkNum++
			chunk := resampled

			// Compute features
			rms := RMSEnergy(chunk)
			zcr := ZeroCrossingRate(chunk)
			centroid := SpectralCentroid(chunk, targetRate)
			spectrum := MagnitudeSpectrum(chunk)

			var flux float64
			if c.prevSpectrum != nil {
				flux = SpectralFlux(spectrum, c.prevSpectrum)
			}
			energyVar := EnergyVariance(chunk, 1600)

			// Compute score manually (mirrors Classify logic)
			var score float64
			if zcr >= c.SpeechZCRMin {
				score += 0.5
			} else {
				score -= 0.25
			}
			if centroid >= c.SpeechCentroidMin && centroid <= c.SpeechCentroidMax {
				score += 0.5
			} else if centroid < c.SpeechCentroidMin {
				score -= 0.5
			} else {
				score -= 0.5
			}
			if flux > c.MusicFluxMax {
				score += 0.5
			} else if c.prevSpectrum != nil {
				score -= 1.0
			}
			if energyVar > c.EnergyVarThreshold {
				score += 3.0
			} else {
				score -= 2.0
			}

			class := "music"
			if rms < c.SilenceThreshold {
				class = "silence"
				score = 0
			} else if score > 0 {
				class = "speech"
			}

			// Store spectrum for next chunk
			c.prevSpectrum = spectrum

			fmt.Printf("%-6d %-8.5f %-8.4f %-10.1f %-12.1f %-14.6f %-10.2f %-10s\n",
				chunkNum, rms, zcr, centroid, flux, energyVar, score, class)
		}
		if err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				fmt.Println("Stream ended.")
				return
			}
			t.Fatalf("read error: %v", err)
		}
	}
}

// resampleLinearDiag resamples mono float32 audio using linear interpolation.
// Duplicated here to avoid importing the audio package (which has build issues).
func resampleLinearDiag(samples []float32, srcRate, dstRate int) []float32 {
	if len(samples) == 0 || srcRate == dstRate {
		return samples
	}
	ratio := float64(srcRate) / float64(dstRate)
	outLen := int(float64(len(samples)) / ratio)
	if outLen == 0 {
		return nil
	}
	out := make([]float32, outLen)
	for i := 0; i < outLen; i++ {
		srcPos := float64(i) * ratio
		srcIdx := int(srcPos)
		frac := float32(srcPos - float64(srcIdx))
		if srcIdx+1 < len(samples) {
			out[i] = samples[srcIdx]*(1.0-frac) + samples[srcIdx+1]*frac
		} else if srcIdx < len(samples) {
			out[i] = samples[srcIdx]
		}
	}
	return out
}
