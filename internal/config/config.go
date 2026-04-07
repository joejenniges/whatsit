package config

import (
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

const (
	appName    = "RadioTranscriber"
	configFile = "config.yaml"
)

// Config holds all user-configurable settings for RadioTranscriber.
type Config struct {
	StreamURL       string `yaml:"stream_url"`
	ModelSize       string `yaml:"model_size"`
	ModelPath       string `yaml:"model_path"`
	Language        string `yaml:"language"`
	BufferSecs      int    `yaml:"buffer_secs"`
	ClassifierTier    string `yaml:"classifier_tier"`    // basic, scheirer, mfcc
	ClassifierDebug   bool   `yaml:"classifier_debug"`   // log raw feature values
	TranscriptionMode string `yaml:"transcription_mode"` // segment or rolling
	WindowSizeSecs    int    `yaml:"window_size_secs"`   // rolling window size, default: 10
	WindowStepSecs    int    `yaml:"window_step_secs"`   // rolling window step, default: 3
	SaveAudio         bool   `yaml:"save_audio"`         // save pre-resampled stereo audio to WAV
	UseGPU            bool   `yaml:"use_gpu"`            // attempt Vulkan GPU acceleration for whisper
	ASREngine         string `yaml:"asr_engine"`         // "whisper" or "parakeet"

	// Fusion classifier tuning (applies live, no restart needed)
	RhythmMusicMin  float64 `yaml:"rhythm_music_min"`  // rhythm strength above this = "has beat" (default: 0.25)
	RhythmSpeechMax float64 `yaml:"rhythm_speech_max"` // rhythm strength below this = "no beat" (default: 0.15)
	CEDSpeechMin    float64 `yaml:"ced_speech_min"`    // CED speech score threshold (default: 0.3)
	CEDMusicMin     float64 `yaml:"ced_music_min"`     // CED music score threshold (default: 0.3)
	MinSpeechSecs   int     `yaml:"min_speech_secs"`   // minimum speech segment duration (default: 8)
}

// DefaultConfig returns a Config populated with default values.
func DefaultConfig() Config {
	return Config{
		StreamURL:      "",
		ModelSize:      "base",
		Language:       "en",
		BufferSecs:     10,
		ClassifierTier:    "fusion",
		TranscriptionMode: "segment",
		WindowSizeSecs:    10,
		WindowStepSecs:    3,
		UseGPU:          true,
		RhythmMusicMin:  0.25,
		RhythmSpeechMax: 0.15,
		CEDSpeechMin:    0.3,
		CEDMusicMin:     0.3,
		MinSpeechSecs:   8,
	}
}

// AppDir returns the path to the RadioTranscriber config directory
// under the OS-appropriate user config location (%APPDATA% on Windows).
func AppDir() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, appName), nil
}

// configPath returns the full path to the config.yaml file.
func configPath() (string, error) {
	dir, err := AppDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, configFile), nil
}

// ensureAppDir creates the RadioTranscriber directory structure if it
// does not already exist. This includes the models subdirectory.
func ensureAppDir() error {
	dir, err := AppDir()
	if err != nil {
		return err
	}
	return os.MkdirAll(filepath.Join(dir, "models"), 0o755)
}

// Load reads the config from disk. If the config file does not exist,
// it creates the app directory structure and writes a default config
// file, then returns the defaults.
func Load() (*Config, error) {
	if err := ensureAppDir(); err != nil {
		return nil, err
	}

	path, err := configPath()
	if err != nil {
		return nil, err
	}

	cfg := DefaultConfig()

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		// First run: write defaults to disk and return them.
		if err := save(&cfg, path); err != nil {
			return nil, err
		}
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Save writes the current config to disk.
func Save(cfg *Config) error {
	path, err := configPath()
	if err != nil {
		return err
	}
	return save(cfg, path)
}

func save(cfg *Config, path string) error {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// --- Test helpers ---

// LoadFrom reads config from a specific directory. Used for testing.
func LoadFrom(dir string) (*Config, error) {
	path := filepath.Join(dir, configFile)
	cfg := DefaultConfig()

	if err := os.MkdirAll(filepath.Join(dir, "models"), 0o755); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		if err := save(&cfg, path); err != nil {
			return nil, err
		}
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}

	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// SaveTo writes config to a specific directory. Used for testing.
func SaveTo(cfg *Config, dir string) error {
	path := filepath.Join(dir, configFile)
	return save(cfg, path)
}
