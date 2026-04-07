package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesDefaultConfig(t *testing.T) {
	dir := t.TempDir()

	cfg, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}

	// Verify defaults
	if cfg.StreamURL != "" {
		t.Errorf("expected empty StreamURL, got %q", cfg.StreamURL)
	}
	if cfg.ModelSize != "base" {
		t.Errorf("expected ModelSize 'base', got %q", cfg.ModelSize)
	}
	if cfg.Language != "en" {
		t.Errorf("expected Language 'en', got %q", cfg.Language)
	}
	if cfg.BufferSecs != 10 {
		t.Errorf("expected BufferSecs 10, got %d", cfg.BufferSecs)
	}

	// Verify the file was actually written to disk
	path := filepath.Join(dir, configFile)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("config.yaml was not created on disk")
	}
}

func TestLoadCreatesModelsDir(t *testing.T) {
	dir := t.TempDir()

	_, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}

	modelsDir := filepath.Join(dir, "models")
	info, err := os.Stat(modelsDir)
	if os.IsNotExist(err) {
		t.Fatal("models/ directory was not created")
	}
	if !info.IsDir() {
		t.Fatal("models/ exists but is not a directory")
	}
}

func TestSaveAndLoad(t *testing.T) {
	dir := t.TempDir()

	original := &Config{
		StreamURL:  "http://example.com/stream.mp3",
		ModelSize:  "small",
		ModelPath:  "/some/path/model.bin",
		Language:   "es",
		BufferSecs: 20,
	}

	if err := SaveTo(original, dir); err != nil {
		t.Fatalf("SaveTo failed: %v", err)
	}

	loaded, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}

	if loaded.StreamURL != original.StreamURL {
		t.Errorf("StreamURL: got %q, want %q", loaded.StreamURL, original.StreamURL)
	}
	if loaded.ModelSize != original.ModelSize {
		t.Errorf("ModelSize: got %q, want %q", loaded.ModelSize, original.ModelSize)
	}
	if loaded.ModelPath != original.ModelPath {
		t.Errorf("ModelPath: got %q, want %q", loaded.ModelPath, original.ModelPath)
	}
	if loaded.Language != original.Language {
		t.Errorf("Language: got %q, want %q", loaded.Language, original.Language)
	}
	if loaded.BufferSecs != original.BufferSecs {
		t.Errorf("BufferSecs: got %d, want %d", loaded.BufferSecs, original.BufferSecs)
	}
}

func TestLoadExistingConfig(t *testing.T) {
	dir := t.TempDir()

	// Write a config file manually with partial fields.
	// Unspecified fields should get zero values (not defaults) since
	// yaml.Unmarshal overwrites the struct.
	yamlContent := []byte("stream_url: http://radio.example.com/live\nbuffer_secs: 30\n")
	path := filepath.Join(dir, configFile)
	if err := os.WriteFile(path, yamlContent, 0o644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	cfg, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}

	if cfg.StreamURL != "http://radio.example.com/live" {
		t.Errorf("StreamURL: got %q, want %q", cfg.StreamURL, "http://radio.example.com/live")
	}
	if cfg.BufferSecs != 30 {
		t.Errorf("BufferSecs: got %d, want 30", cfg.BufferSecs)
	}
}

func TestSaveOverwritesExisting(t *testing.T) {
	dir := t.TempDir()

	cfg1 := &Config{StreamURL: "http://first.example.com", ModelSize: "tiny", Language: "en", BufferSecs: 5}
	if err := SaveTo(cfg1, dir); err != nil {
		t.Fatalf("first SaveTo failed: %v", err)
	}

	cfg2 := &Config{StreamURL: "http://second.example.com", ModelSize: "medium", Language: "fr", BufferSecs: 15}
	if err := SaveTo(cfg2, dir); err != nil {
		t.Fatalf("second SaveTo failed: %v", err)
	}

	loaded, err := LoadFrom(dir)
	if err != nil {
		t.Fatalf("LoadFrom failed: %v", err)
	}

	if loaded.StreamURL != "http://second.example.com" {
		t.Errorf("StreamURL: got %q, want %q", loaded.StreamURL, "http://second.example.com")
	}
	if loaded.ModelSize != "medium" {
		t.Errorf("ModelSize: got %q, want %q", loaded.ModelSize, "medium")
	}
}

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.StreamURL != "" {
		t.Errorf("default StreamURL should be empty, got %q", cfg.StreamURL)
	}
	if cfg.ModelSize != "base" {
		t.Errorf("default ModelSize should be 'base', got %q", cfg.ModelSize)
	}
	if cfg.Language != "en" {
		t.Errorf("default Language should be 'en', got %q", cfg.Language)
	}
	if cfg.BufferSecs != 10 {
		t.Errorf("default BufferSecs should be 10, got %d", cfg.BufferSecs)
	}
}
