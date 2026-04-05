package transcriber

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestModelInfo(t *testing.T) {
	tests := []struct {
		size     string
		wantFile string
		wantURL  string
		wantErr  bool
	}{
		{"tiny", "ggml-tiny.en.bin", "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-tiny.en.bin", false},
		{"base", "ggml-base.en.bin", "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin", false},
		{"small", "ggml-small.en.bin", "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-small.en.bin", false},
		{"medium", "ggml-medium.en.bin", "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-medium.en.bin", false},
		{"large", "", "", true},
		{"", "", "", true},
		{"invalid", "", "", true},
	}

	dir := "/tmp/models"
	for _, tt := range tests {
		t.Run(tt.size, func(t *testing.T) {
			fp, url, err := ModelInfo(dir, tt.size)
			if tt.wantErr {
				if err == nil {
					t.Fatal("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			wantPath := filepath.Join(dir, tt.wantFile)
			if fp != wantPath {
				t.Errorf("filePath = %q, want %q", fp, wantPath)
			}
			if url != tt.wantURL {
				t.Errorf("url = %q, want %q", url, tt.wantURL)
			}
		})
	}
}

func TestEnsureModel_Missing(t *testing.T) {
	dir := t.TempDir()
	exists, fp, err := EnsureModel(dir, "tiny")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("expected exists=false for missing model")
	}
	want := filepath.Join(dir, "ggml-tiny.en.bin")
	if fp != want {
		t.Errorf("filePath = %q, want %q", fp, want)
	}
}

func TestEnsureModel_Exists(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "ggml-base.en.bin")
	if err := os.WriteFile(fp, []byte("fake model data"), 0o644); err != nil {
		t.Fatal(err)
	}

	exists, got, err := EnsureModel(dir, "base")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !exists {
		t.Fatal("expected exists=true for existing model")
	}
	if got != fp {
		t.Errorf("filePath = %q, want %q", got, fp)
	}
}

func TestEnsureModel_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	fp := filepath.Join(dir, "ggml-small.en.bin")
	if err := os.WriteFile(fp, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	exists, _, err := EnsureModel(dir, "small")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists {
		t.Fatal("expected exists=false for empty file")
	}
}

func TestEnsureModel_InvalidSize(t *testing.T) {
	dir := t.TempDir()
	_, _, err := EnsureModel(dir, "huge")
	if err == nil {
		t.Fatal("expected error for invalid size")
	}
}

func TestDownloadModel(t *testing.T) {
	// Fake model content.
	content := strings.Repeat("model-data-chunk-", 100)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(content))
	}))
	defer srv.Close()

	// Override the download function to hit our test server.
	origDownload := downloadURL
	downloadURL = func(ctx context.Context, url string) (*http.Response, error) {
		// Rewrite the URL to point at our test server.
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/model", nil)
		if err != nil {
			return nil, err
		}
		return http.DefaultClient.Do(req)
	}
	defer func() { downloadURL = origDownload }()

	dir := t.TempDir()
	var progressCalls int64

	fp, err := DownloadModel(context.Background(), dir, "tiny", func(downloaded, total int64) {
		atomic.AddInt64(&progressCalls, 1)
		if total != int64(len(content)) {
			t.Errorf("total = %d, want %d", total, len(content))
		}
	})
	if err != nil {
		t.Fatalf("DownloadModel failed: %v", err)
	}

	// Verify file path.
	want := filepath.Join(dir, "ggml-tiny.en.bin")
	if fp != want {
		t.Errorf("filePath = %q, want %q", fp, want)
	}

	// Verify file content.
	data, err := os.ReadFile(fp)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(data) != content {
		t.Errorf("file content mismatch: got %d bytes, want %d", len(data), len(content))
	}

	// Verify progress was called.
	if atomic.LoadInt64(&progressCalls) == 0 {
		t.Error("progress callback was never called")
	}

	// Verify .tmp file is cleaned up.
	tmpPath := fp + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error(".tmp file still exists after successful download")
	}
}

func TestDownloadModel_Cancellation(t *testing.T) {
	// Server that writes slowly so we can cancel mid-download.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "100000")
		w.WriteHeader(http.StatusOK)
		// Write one small chunk then stall.
		w.Write([]byte("partial"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Block until the client disconnects.
		<-r.Context().Done()
	}))
	defer srv.Close()

	origDownload := downloadURL
	downloadURL = func(ctx context.Context, url string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/model", nil)
		if err != nil {
			return nil, err
		}
		return http.DefaultClient.Do(req)
	}
	defer func() { downloadURL = origDownload }()

	dir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay.
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	_, err := DownloadModel(ctx, dir, "base", nil)
	if err == nil {
		t.Fatal("expected error from cancelled download")
	}

	// Verify .tmp file is cleaned up.
	tmpPath := filepath.Join(dir, "ggml-base.en.bin.tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error(".tmp file still exists after cancelled download")
	}
}

func TestDownloadModel_InvalidSize(t *testing.T) {
	_, err := DownloadModel(context.Background(), t.TempDir(), "huge", nil)
	if err == nil {
		t.Fatal("expected error for invalid size")
	}
}

func TestDownloadModel_CreatesDir(t *testing.T) {
	content := "model-bytes"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", len(content)))
		w.Write([]byte(content))
	}))
	defer srv.Close()

	origDownload := downloadURL
	downloadURL = func(ctx context.Context, url string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/model", nil)
		if err != nil {
			return nil, err
		}
		return http.DefaultClient.Do(req)
	}
	defer func() { downloadURL = origDownload }()

	// Use a nested dir that doesn't exist yet.
	dir := filepath.Join(t.TempDir(), "nested", "models")

	fp, err := DownloadModel(context.Background(), dir, "medium", nil)
	if err != nil {
		t.Fatalf("DownloadModel failed: %v", err)
	}

	if _, err := os.Stat(fp); err != nil {
		t.Fatalf("downloaded file missing: %v", err)
	}
}

func TestDownloadModel_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	origDownload := downloadURL
	downloadURL = func(ctx context.Context, url string) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/model", nil)
		if err != nil {
			return nil, err
		}
		return http.DefaultClient.Do(req)
	}
	defer func() { downloadURL = origDownload }()

	_, err := DownloadModel(context.Background(), t.TempDir(), "tiny", nil)
	if err == nil {
		t.Fatal("expected error for HTTP 404")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Errorf("error should mention 404, got: %v", err)
	}
}
