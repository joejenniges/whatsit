package audio

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestWAVWriter_Float32_ValidHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 16000, 1, WAVFormatFloat32)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	samples := make([]float32, 16000) // 1 second of silence
	for i := range samples {
		samples[i] = float32(i) / float32(len(samples)) // ramp 0..1
	}

	if err := w.WriteFloat32(samples); err != nil {
		t.Fatalf("WriteFloat32: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

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

func TestWAVWriter_Int16_ValidHeader(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 48000, 2, WAVFormatInt16)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	// 1 second of stereo silence at 48kHz = 96000 samples
	samples := make([]int16, 96000)
	for i := range samples {
		samples[i] = int16(i % 32767)
	}

	if err := w.WriteInt16(samples); err != nil {
		t.Fatalf("WriteInt16: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	audioFormat := binary.LittleEndian.Uint16(data[20:22])
	if audioFormat != 1 { // PCM
		t.Errorf("expected format 1 (PCM), got %d", audioFormat)
	}
	channels := binary.LittleEndian.Uint16(data[22:24])
	if channels != 2 {
		t.Errorf("expected 2 channels, got %d", channels)
	}
	sampleRate := binary.LittleEndian.Uint32(data[24:28])
	if sampleRate != 48000 {
		t.Errorf("expected sample rate 48000, got %d", sampleRate)
	}
	bitsPerSample := binary.LittleEndian.Uint16(data[34:36])
	if bitsPerSample != 16 {
		t.Errorf("expected 16 bits per sample, got %d", bitsPerSample)
	}

	// byte rate = 48000 * 2 * 2 = 192000
	byteRate := binary.LittleEndian.Uint32(data[28:32])
	if byteRate != 192000 {
		t.Errorf("expected byte rate 192000, got %d", byteRate)
	}

	// block align = 2 * 2 = 4
	blockAlign := binary.LittleEndian.Uint16(data[32:34])
	if blockAlign != 4 {
		t.Errorf("expected block align 4, got %d", blockAlign)
	}

	dataSize := binary.LittleEndian.Uint32(data[40:44])
	expectedDataSize := uint32(len(samples) * 2)
	if dataSize != expectedDataSize {
		t.Errorf("expected data size %d, got %d", expectedDataSize, dataSize)
	}
}

func TestWAVWriter_Float32_FileSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 16000, 1, WAVFormatFloat32)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	numSamples := 32000 // 2 seconds
	samples := make([]float32, numSamples)
	if err := w.WriteFloat32(samples); err != nil {
		t.Fatalf("WriteFloat32: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	expectedSize := int64(44 + numSamples*4)
	if info.Size() != expectedSize {
		t.Errorf("expected file size %d, got %d", expectedSize, info.Size())
	}
}

func TestWAVWriter_Int16_FileSize(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 48000, 2, WAVFormatInt16)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	numSamples := 96000
	samples := make([]int16, numSamples)
	if err := w.WriteInt16(samples); err != nil {
		t.Fatalf("WriteInt16: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}

	expectedSize := int64(44 + numSamples*2)
	if info.Size() != expectedSize {
		t.Errorf("expected file size %d, got %d", expectedSize, info.Size())
	}
}

func TestWAVWriter_Float32_SampleValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 16000, 1, WAVFormatFloat32)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	samples := []float32{0.0, 0.5, -0.5, 1.0, -1.0}
	if err := w.WriteFloat32(samples); err != nil {
		t.Fatalf("WriteFloat32: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	for i, expected := range samples {
		offset := 44 + i*4
		bits := binary.LittleEndian.Uint32(data[offset : offset+4])
		got := math.Float32frombits(bits)
		if got != expected {
			t.Errorf("sample[%d]: expected %f, got %f", i, expected, got)
		}
	}
}

func TestWAVWriter_Int16_SampleValues(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 48000, 2, WAVFormatInt16)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	samples := []int16{0, 1000, -1000, 32767, -32768}
	if err := w.WriteInt16(samples); err != nil {
		t.Fatalf("WriteInt16: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	for i, expected := range samples {
		offset := 44 + i*2
		got := int16(binary.LittleEndian.Uint16(data[offset : offset+2]))
		if got != expected {
			t.Errorf("sample[%d]: expected %d, got %d", i, expected, got)
		}
	}
}

func TestWAVWriter_Float32_MultipleWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test.wav")

	w, err := NewWAVWriter(path, 16000, 1, WAVFormatFloat32)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}

	chunk1 := []float32{0.1, 0.2, 0.3}
	chunk2 := []float32{0.4, 0.5}
	if err := w.WriteFloat32(chunk1); err != nil {
		t.Fatalf("WriteFloat32 chunk1: %v", err)
	}
	if err := w.WriteFloat32(chunk2); err != nil {
		t.Fatalf("WriteFloat32 chunk2: %v", err)
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

	w, err := NewWAVWriter(path, 16000, 1, WAVFormatFloat32)
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

	w, err := NewWAVWriter(path, 16000, 1, WAVFormatFloat32)
	if err != nil {
		t.Fatalf("NewWAVWriter: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	err = w.WriteFloat32([]float32{1.0})
	if err == nil {
		t.Error("expected error writing after close")
	}
}

func TestWAVWriter_WrongFormatError(t *testing.T) {
	dir := t.TempDir()

	// Float32 writer should reject WriteInt16.
	path1 := filepath.Join(dir, "float.wav")
	w1, err := NewWAVWriter(path1, 16000, 1, WAVFormatFloat32)
	if err != nil {
		t.Fatalf("NewWAVWriter float32: %v", err)
	}
	defer w1.Close()
	if err := w1.WriteInt16([]int16{1}); err == nil {
		t.Error("expected error calling WriteInt16 on float32 writer")
	}

	// Int16 writer should reject WriteFloat32.
	path2 := filepath.Join(dir, "int16.wav")
	w2, err := NewWAVWriter(path2, 48000, 2, WAVFormatInt16)
	if err != nil {
		t.Fatalf("NewWAVWriter int16: %v", err)
	}
	defer w2.Close()
	if err := w2.WriteFloat32([]float32{1.0}); err == nil {
		t.Error("expected error calling WriteFloat32 on int16 writer")
	}
}
