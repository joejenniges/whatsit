package audio

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestWAVWriter_ValidHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 16000, 1)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	samples := make([]float32, 16000) // 1 second of silence
	for i := range samples {
		samples[i] = float32(i) / float32(len(samples)) // ramp 0..1
	}

	if err := w.Write(samples); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Read the file back and verify header.
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// RIFF header
	if string(data[0:4]) != "RIFF" {
		t.Errorf("expected RIFF, got %q", data[0:4])
	}
	if string(data[8:12]) != "WAVE" {
		t.Errorf("expected WAVE, got %q", data[8:12])
	}

	// fmt chunk
	if string(data[12:16]) != "fmt " {
		t.Errorf("expected 'fmt ', got %q", data[12:16])
	}
	fmtSize := binary.LittleEndian.Uint32(data[16:20])
	if fmtSize != 16 {
		t.Errorf("expected fmt size 16, got %d", fmtSize)
	}
	audioFormat := binary.LittleEndian.Uint16(data[20:22])
	if audioFormat != 3 { // IEEE_FLOAT
		t.Errorf("expected format 3 (IEEE_FLOAT), got %d", audioFormat)
	}
	channels := binary.LittleEndian.Uint16(data[22:24])
	if channels != 1 {
		t.Errorf("expected 1 channel, got %d", channels)
	}
	sampleRate := binary.LittleEndian.Uint32(data[24:28])
	if sampleRate != 16000 {
		t.Errorf("expected sample rate 16000, got %d", sampleRate)
	}
	bitsPerSample := binary.LittleEndian.Uint16(data[34:36])
	if bitsPerSample != 32 {
		t.Errorf("expected 32 bits per sample, got %d", bitsPerSample)
	}

	// data chunk
	if string(data[36:40]) != "data" {
		t.Errorf("expected 'data', got %q", data[36:40])
	}
	dataSize := binary.LittleEndian.Uint32(data[40:44])
	expectedDataSize := uint32(len(samples) * 4)
	if dataSize != expectedDataSize {
		t.Errorf("expected data size %d, got %d", expectedDataSize, dataSize)
	}

	// RIFF size = 36 + data size
	riffSize := binary.LittleEndian.Uint32(data[4:8])
	if riffSize != 36+expectedDataSize {
		t.Errorf("expected RIFF size %d, got %d", 36+expectedDataSize, riffSize)
	}
}

func TestWAVWriter_FileSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 16000, 1)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	numSamples := 32000 // 2 seconds
	samples := make([]float32, numSamples)
	if err := w.Write(samples); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	expectedSize := int64(44 + numSamples*4) // header + samples
	if info.Size() != expectedSize {
		t.Errorf("expected file size %d, got %d", expectedSize, info.Size())
	}
}

func TestWAVWriter_SampleValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 16000, 1)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	// Write known values.
	samples := []float32{0.0, 0.5, -0.5, 1.0, -1.0}
	if err := w.Write(samples); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	// Read samples back from after the 44-byte header.
	for i, expected := range samples {
		offset := 44 + i*4
		bits := binary.LittleEndian.Uint32(data[offset : offset+4])
		got := math.Float32frombits(bits)
		if got != expected {
			t.Errorf("sample[%d]: expected %f, got %f", i, expected, got)
		}
	}
}

func TestWAVWriter_MultipleWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 16000, 1)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	// Write in two chunks.
	chunk1 := []float32{0.1, 0.2, 0.3}
	chunk2 := []float32{0.4, 0.5}
	if err := w.Write(chunk1); err != nil {
		t.Fatalf("Write chunk1: %v", err)
	}
	if err := w.Write(chunk2); err != nil {
		t.Fatalf("Write chunk2: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	allSamples := append(chunk1, chunk2...)
	dataSize := binary.LittleEndian.Uint32(data[40:44])
	if dataSize != uint32(len(allSamples)*4) {
		t.Errorf("expected data size %d, got %d", len(allSamples)*4, dataSize)
	}

	// Verify all sample values.
	for i, expected := range allSamples {
		offset := 44 + i*4
		bits := binary.LittleEndian.Uint32(data[offset : offset+4])
		got := math.Float32frombits(bits)
		if got != expected {
			t.Errorf("sample[%d]: expected %f, got %f", i, expected, got)
		}
	}
}

func TestWAVWriter_Path(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 16000, 1)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}
	defer w.Close()

	got := w.Path()
	if got != path {
		t.Errorf("expected path %q, got %q", path, got)
	}
}

func TestWAVWriter_WriteAfterClose(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 16000, 1)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = w.Write([]float32{1.0})
	if err == nil {
		t.Error("expected error writing after close")
	}
}
