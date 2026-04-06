package transcriber

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

// DownloadProgress is called with progress updates during model download.
type DownloadProgress func(bytesDownloaded, totalBytes int64)

var validSizes = map[string]bool{
	"tiny":   true,
	"base":   true,
	"small":  true,
	"medium": true,
}

const huggingFaceBase = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main"

// ModelInfo returns the expected file path and download URL for a model size.
// Size must be one of: tiny, base, small, medium.
func ModelInfo(modelsDir, size string) (filePath, url string, err error) {
	if !validSizes[size] {
		return "", "", fmt.Errorf("invalid model size %q: must be tiny, base, small, or medium", size)
	}
	filename := fmt.Sprintf("ggml-%s.en.bin", size)
	filePath = filepath.Join(modelsDir, filename)
	url = fmt.Sprintf("%s/%s", huggingFaceBase, filename)
	return filePath, url, nil
}

// EnsureModel checks if the model file exists and is non-zero size.
// Returns (true, filePath, nil) if the model is ready to use,
// or (false, filePath, nil) if the caller should download it.
func EnsureModel(modelsDir, size string) (bool, string, error) {
	filePath, _, err := ModelInfo(modelsDir, size)
	if err != nil {
		return false, "", err
	}

	info, err := os.Stat(filePath)
	if errors.Is(err, os.ErrNotExist) {
		return false, filePath, nil
	}
	if err != nil {
		return false, filePath, fmt.Errorf("stat model file: %w", err)
	}
	if info.Size() == 0 {
		return false, filePath, nil
	}

	return true, filePath, nil
}

// DownloadModel downloads the whisper model with progress reporting.
// It downloads to a temporary file first, then renames on success.
// On cancellation or error, the temporary file is cleaned up.
func DownloadModel(ctx context.Context, modelsDir, size string, progress DownloadProgress) (string, error) {
	filePath, url, err := ModelInfo(modelsDir, size)
	if err != nil {
		return "", err
	}

	// Create models directory if it doesn't exist.
	if err := os.MkdirAll(modelsDir, 0o755); err != nil {
		return "", fmt.Errorf("create models directory: %w", err)
	}

	tmpPath := filePath + ".tmp"

	// Clean up tmp file on any failure path.
	success := false
	defer func() {
		if !success {
			os.Remove(tmpPath)
		}
	}()

	return filePath, downloadTo(ctx, url, filePath, tmpPath, progress, &success)
}

// downloadURL is the HTTP client function, swappable for testing.
// WHY: Allows tests to inject an httptest server without changing the public API.
var downloadURL = defaultDownloadURL

func defaultDownloadURL(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// --- Parakeet model download ---

const parakeetHFBase = "https://huggingface.co/istupakov/parakeet-ctc-0.6b-onnx/resolve/main"

// parakeetFiles lists the files needed for the Parakeet CTC model.
// WHY model.onnx + model.onnx.data: The ONNX model is split into a small
// graph file and a large external data file. Both are required.
var parakeetFiles = []struct {
	name string
	url  string
}{
	{"model.onnx", parakeetHFBase + "/model.onnx"},
	{"model.onnx.data", parakeetHFBase + "/model.onnx.data"},
	{"vocab.txt", parakeetHFBase + "/vocab.txt"},
}

// ParakeetModelInfo returns expected file paths for the Parakeet model.
func ParakeetModelInfo(modelsDir string) (modelPath, vocabPath string) {
	dir := filepath.Join(modelsDir, "parakeet-ctc-0.6b")
	return filepath.Join(dir, "model.onnx"), filepath.Join(dir, "vocab.txt")
}

// EnsureParakeetModel checks if all Parakeet model files exist.
func EnsureParakeetModel(modelsDir string) (bool, string, string, error) {
	dir := filepath.Join(modelsDir, "parakeet-ctc-0.6b")
	modelPath := filepath.Join(dir, "model.onnx")
	vocabPath := filepath.Join(dir, "vocab.txt")

	for _, f := range parakeetFiles {
		p := filepath.Join(dir, f.name)
		info, err := os.Stat(p)
		if err != nil || info.Size() == 0 {
			return false, modelPath, vocabPath, nil
		}
	}
	return true, modelPath, vocabPath, nil
}

// DownloadParakeetModel downloads all Parakeet model files with progress.
// Progress reports cumulative bytes across all files.
func DownloadParakeetModel(ctx context.Context, modelsDir string, progress DownloadProgress) (modelPath, vocabPath string, err error) {
	dir := filepath.Join(modelsDir, "parakeet-ctc-0.6b")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", "", fmt.Errorf("create parakeet model directory: %w", err)
	}

	modelPath = filepath.Join(dir, "model.onnx")
	vocabPath = filepath.Join(dir, "vocab.txt")

	for _, f := range parakeetFiles {
		filePath := filepath.Join(dir, f.name)
		tmpPath := filePath + ".tmp"

		// Skip if already downloaded.
		if info, statErr := os.Stat(filePath); statErr == nil && info.Size() > 0 {
			continue
		}

		success := false
		defer func(tmp string) {
			if !success {
				os.Remove(tmp)
			}
		}(tmpPath)

		if dlErr := downloadTo(ctx, f.url, filePath, tmpPath, progress, &success); dlErr != nil {
			return "", "", fmt.Errorf("download %s: %w", f.name, dlErr)
		}
	}

	return modelPath, vocabPath, nil
}

func downloadTo(ctx context.Context, url, filePath, tmpPath string, progress DownloadProgress, success *bool) error {
	resp, err := downloadURL(ctx, url)
	if err != nil {
		return fmt.Errorf("download request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download failed: HTTP %d", resp.StatusCode)
	}

	totalBytes := resp.ContentLength

	out, err := os.Create(tmpPath)
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}
	defer out.Close()

	var written int64
	buf := make([]byte, 32*1024)

	for {
		// Check for cancellation before each read.
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			nw, writeErr := out.Write(buf[:n])
			if writeErr != nil {
				return fmt.Errorf("write temp file: %w", writeErr)
			}
			written += int64(nw)
			if progress != nil {
				progress(written, totalBytes)
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return fmt.Errorf("read response body: %w", readErr)
		}
	}

	// Close before rename so the file handle is released (matters on Windows).
	if err := out.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}

	if written == 0 {
		return fmt.Errorf("downloaded file is empty")
	}

	// Atomic-ish rename. On Windows, os.Rename fails if dest exists,
	// so remove it first.
	os.Remove(filePath)
	if err := os.Rename(tmpPath, filePath); err != nil {
		return fmt.Errorf("rename temp to final: %w", err)
	}

	*success = true
	return nil
}
