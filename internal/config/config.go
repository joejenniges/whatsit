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
	AcoustIDKey     string `yaml:"acoustid_key"`
	Language        string `yaml:"language"`
	BufferSecs      int    `yaml:"buffer_secs"`
	ClassifierTier  string `yaml:"classifier_tier"`  // basic, scheirer, mfcc
	ClassifierDebug bool   `yaml:"classifier_debug"` // log raw feature values
	WindowSizeSecs  int    `yaml:"window_size_secs"` // rolling window size, default: 10
	WindowStepSecs  int    `yaml:"window_step_secs"` // rolling window step, default: 3
	SaveAudio       bool   `yaml:"save_audio"`       // save pre-resampled stereo audio to WAV
}

// DefaultConfig returns a Config populated with default values.
func DefaultConfig() Config {
	return Config{
		StreamURL:      "",
		ModelSize:      "base",
		Language:       "en",
		BufferSecs:     10,
		ClassifierTier: "scheirer",
		WindowSizeSecs: 10,
		WindowStepSecs: 3,
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
