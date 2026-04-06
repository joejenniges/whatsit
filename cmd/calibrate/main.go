// Command calibrate computes all classifier features across all tiers for
// raw PCM audio files or a live MP3 stream.
//
// Usage:
//
//	go run ./cmd/calibrate/ --dir testdata/
//	go run ./cmd/calibrate/ --stream https://url --duration 30
//
// For files: reads .raw files (16kHz mono float32 little-endian), splits into
// 2-second chunks, computes features, outputs CSV to stdout.
//
// For streams: connects to an MP3 stream, decodes, resamples to 16kHz mono,
// runs for N seconds, outputs CSV to stdout.
//
// CSV columns:
//
//	source,chunk,zcr,rms,flatness,centroid_var,flux_mean,low_energy_pct,mfcc_var_mean,delta_mean,rolloff,basic_class,scheirer_class,mfcc_class
package main

import (
	"context"
	"encoding/binary"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/joe/radio-transcriber/internal/audio"
	"github.com/joe/radio-transcriber/internal/classifier"
)

const sampleRate = 16000
const chunkSeconds = 2
const chunkSamples = sampleRate * chunkSeconds

func main() {
	dir := flag.String("dir", "", "directory containing .raw PCM files")
	stream := flag.String("stream", "", "MP3 stream URL for live calibration")
	duration := flag.Int("duration", 30, "seconds to record from stream")
	flag.Parse()

	if *dir == "" && *stream == "" {
		fmt.Fprintf(os.Stderr, "usage: calibrate --dir testdata/  OR  calibrate --stream URL --duration N\n")
		os.Exit(1)
	}

	// CSV header
	fmt.Println("source,chunk,zcr,rms,flatness,centroid_var,flux_mean,low_energy_pct,mfcc_var_mean,delta_mean,rolloff,basic_class,scheirer_class,mfcc_class")

	if *dir != "" {
		processDir(*dir)
	}
	if *stream != "" {
		processStream(*stream, *duration)
	}
}

func processDir(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Fatalf("read dir: %v", err)
	}

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".raw" {
			continue
		}

		path := filepath.Join(dir, entry.Name())
		samples, err := readRawFile(path)
		if err != nil {
			log.Printf("skip %s: %v", entry.Name(), err)
			continue
		}

		processChunks(entry.Name(), samples)
	}
}

func readRawFile(path string) ([]float32, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	if len(data)%4 != 0 {
		return nil, fmt.Errorf("file size %d not a multiple of 4 (float32)", len(data))
	}

	samples := make([]float32, len(data)/4)
	for i := range samples {
		bits := binary.LittleEndian.Uint32(data[i*4 : (i+1)*4])
		samples[i] = math.Float32frombits(bits)
	}
	return samples, nil
}

func processStream(url string, durationSecs int) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(durationSecs)*time.Second+5*time.Second)
	defer cancel()

	streamer := audio.NewStreamer(ctx, url)
	if err := streamer.Start(); err != nil {
		log.Fatalf("connect to stream: %v", err)
	}

	decoder := audio.NewDecoder(streamer.Output())
	if err := decoder.Start(ctx); err != nil {
		log.Fatalf("start decoder: %v", err)
	}

	inputRate := decoder.SampleRate()
	if inputRate == 0 {
		// Decoder hasn't decoded a frame yet; wait briefly.
		time.Sleep(500 * time.Millisecond)
		inputRate = decoder.SampleRate()
		if inputRate == 0 {
			inputRate = 44100 // reasonable fallback
		}
	}

	resampler := audio.NewResampler(decoder.Output(), inputRate, sampleRate)
	resampler.Start(ctx)

	deadline := time.After(time.Duration(durationSecs) * time.Second)
	var allSamples []float32

	log.Printf("calibrate: streaming for %ds from %s (input rate: %d Hz)", durationSecs, url, inputRate)

collect:
	for {
		select {
		case chunk, ok := <-resampler.Output():
			if !ok {
				break collect
			}
			allSamples = append(allSamples, chunk...)
		case <-deadline:
			break collect
		}
	}

	cancel() // stop the pipeline
	log.Printf("calibrate: collected %d samples (%.1fs)", len(allSamples), float64(len(allSamples))/sampleRate)
	processChunks("stream", allSamples)
}

func processChunks(source string, samples []float32) {
	// Create one classifier instance per tier so debounce state is independent.
	basicC := classifier.NewBasicClassifier()
	scheirerC := classifier.NewScheirerClassifier(sampleRate)
	mfccC := classifier.NewMFCCClassifier(sampleRate)

	numChunks := len(samples) / chunkSamples
	for i := 0; i < numChunks; i++ {
		chunk := samples[i*chunkSamples : (i+1)*chunkSamples]

		// Compute features used across tiers.
		zcr := classifier.ZeroCrossingRate(chunk)
		rms := classifier.RMSEnergy(chunk)
		flatness := classifier.SpectralFlatness(chunk)

		scheirerFeats := classifier.ComputeScheirerFeatures(chunk, sampleRate)

		// MFCC features: variance mean and delta mean.
		cfg := classifier.DefaultMFCCConfig()
		cfg.SampleRate = sampleRate
		extractor := classifier.NewMFCCExtractor(cfg)
		mfccs := extractor.ComputeChunk(chunk)

		var mfccVarMean, deltaMean float64
		if len(mfccs) >= 2 {
			variances := classifier.MFCCVariance(mfccs)
			if len(variances) > 1 {
				var sum float64
				for j := 1; j < len(variances); j++ {
					sum += variances[j]
				}
				mfccVarMean = sum / float64(len(variances)-1)
			}

			deltas := classifier.DeltaMFCC(mfccs)
			if len(deltas) > 0 {
				var dSum float64
				count := 0
				for _, d := range deltas {
					for _, v := range d {
						dSum += math.Abs(v)
						count++
					}
				}
				if count > 0 {
					deltaMean = dSum / float64(count)
				}
			}
		}

		// Spectral rolloff (0.85).
		f64 := make([]float64, len(chunk))
		for j, s := range chunk {
			f64[j] = float64(s)
		}
		ps := classifier.PowerSpectrum(f64)
		rolloff := classifier.SpectralRolloff(ps, sampleRate, 0.85)

		// Classify with each tier.
		basicClass := basicC.Classify(chunk)
		scheirerClass := scheirerC.Classify(chunk)
		mfccClass := mfccC.Classify(chunk)

		fmt.Printf("%s,%d,%.4f,%.4f,%.4f,%.1f,%.4f,%.4f,%.2f,%.3f,%.0f,%s,%s,%s\n",
			source, i,
			zcr, rms, flatness,
			scheirerFeats.SpectralCentroidVariance,
			scheirerFeats.SpectralFluxMean,
			scheirerFeats.LowEnergyPercent,
			mfccVarMean, deltaMean, rolloff,
			basicClass, scheirerClass, mfccClass,
		)
	}
}
