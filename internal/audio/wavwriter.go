package audio

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
)

// WAVFormat selects the sample encoding for a WAV file.
type WAVFormat int

const (
	// WAVFormatFloat32 writes IEEE 754 float32 samples (format code 3).
	WAVFormatFloat32 WAVFormat = iota
	// WAVFormatInt16 writes signed 16-bit PCM samples (format code 1).
	WAVFormatInt16
)

// WAVWriter writes PCM audio samples to a WAV file incrementally.
// The header is written with a placeholder data size on creation, then
// finalized with the correct size on Close().
type WAVWriter struct {
	file       *os.File
	dataSize   int64
	sampleRate int
	channels   int
	format     WAVFormat
}

// NewWAVWriter creates a new WAV file at path and writes the initial header.
// The caller must call Close() to finalize the header with the correct data size.
func NewWAVWriter(path string, sampleRate, channels int, format WAVFormat) (*WAVWriter, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create wav directory: %w", err)
	}

	f, err := os.Create(path)
	if err != nil {
		return nil, fmt.Errorf("create wav file: %w", err)
	}

	w := &WAVWriter{
		file:       f,
		sampleRate: sampleRate,
		channels:   channels,
		format:     format,
	}

	if err := w.writeHeader(); err != nil {
		f.Close()
		os.Remove(path)
		return nil, fmt.Errorf("write wav header: %w", err)
	}

	return w, nil
}

// WriteFloat32 appends float32 PCM samples to the WAV file.
// The writer must have been created with WAVFormatFloat32.
func (w *WAVWriter) WriteFloat32(samples []float32) error {
	if w.file == nil {
		return fmt.Errorf("wav writer is closed")
	}
	if w.format != WAVFormatFloat32 {
		return fmt.Errorf("WriteFloat32 called on int16 writer")
	}

	buf := make([]byte, len(samples)*4)
	for i, s := range samples {
		bits := math.Float32bits(s)
		binary.LittleEndian.PutUint32(buf[i*4:], bits)
	}

	n, err := w.file.Write(buf)
	if err != nil {
		return fmt.Errorf("write wav samples: %w", err)
	}
	w.dataSize += int64(n)
	return nil
}

// WriteInt16 appends int16 PCM samples to the WAV file.
// The writer must have been created with WAVFormatInt16.
func (w *WAVWriter) WriteInt16(samples []int16) error {
	if w.file == nil {
		return fmt.Errorf("wav writer is closed")
	}
	if w.format != WAVFormatInt16 {
		return fmt.Errorf("WriteInt16 called on float32 writer")
	}

	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}

	n, err := w.file.Write(buf)
	if err != nil {
		return fmt.Errorf("write wav samples: %w", err)
	}
	w.dataSize += int64(n)
	return nil
}

// Close finalizes the WAV header with the correct data size and closes the file.
func (w *WAVWriter) Close() error {
	if w.file == nil {
		return nil
	}

	// Update RIFF chunk size (file size - 8).
	riffSize := uint32(36 + w.dataSize)
	if _, err := w.file.Seek(4, 0); err != nil {
		return fmt.Errorf("seek riff size: %w", err)
	}
	if err := binary.Write(w.file, binary.LittleEndian, riffSize); err != nil {
		return fmt.Errorf("write riff size: %w", err)
	}

	// Update data chunk size.
	if _, err := w.file.Seek(40, 0); err != nil {
		return fmt.Errorf("seek data size: %w", err)
	}
	if err := binary.Write(w.file, binary.LittleEndian, uint32(w.dataSize)); err != nil {
		return fmt.Errorf("write data size: %w", err)
	}

	err := w.file.Close()
	w.file = nil
	return err
}

// Path returns the file path of the WAV file.
func (w *WAVWriter) Path() string {
	if w.file == nil {
		return ""
	}
	return w.file.Name()
}

// writeHeader writes a 44-byte WAV header with placeholder sizes.
func (w *WAVWriter) writeHeader() error {
	var formatCode uint16
	var bitsPerSample uint16
	var bytesPerSample int

	switch w.format {
	case WAVFormatFloat32:
		formatCode = 3 // IEEE_FLOAT
		bitsPerSample = 32
		bytesPerSample = 4
	case WAVFormatInt16:
		formatCode = 1 // PCM
		bitsPerSample = 16
		bytesPerSample = 2
	}

	byteRate := uint32(w.sampleRate * w.channels * bytesPerSample)
	blockAlign := uint16(w.channels * bytesPerSample)

	header := make([]byte, 44)

	// RIFF chunk descriptor
	copy(header[0:4], "RIFF")
	binary.LittleEndian.PutUint32(header[4:8], 0) // placeholder, updated on Close
	copy(header[8:12], "WAVE")

	// fmt sub-chunk
	copy(header[12:16], "fmt ")
	binary.LittleEndian.PutUint32(header[16:20], 16)                     // sub-chunk size
	binary.LittleEndian.PutUint16(header[20:22], formatCode)             // audio format
	binary.LittleEndian.PutUint16(header[22:24], uint16(w.channels))     // channels
	binary.LittleEndian.PutUint32(header[24:28], uint32(w.sampleRate))   // sample rate
	binary.LittleEndian.PutUint32(header[28:32], byteRate)               // byte rate
	binary.LittleEndian.PutUint16(header[32:34], blockAlign)             // block align
	binary.LittleEndian.PutUint16(header[34:36], bitsPerSample)          // bits per sample

	// data sub-chunk
	copy(header[36:40], "data")
	binary.LittleEndian.PutUint32(header[40:44], 0) // placeholder, updated on Close

	_, err := w.file.Write(header)
	return err
}
