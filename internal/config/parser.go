package config

import (
	"fmt"
	"log/slog"
	"os"

	"gopkg.in/yaml.v3"
)

// Load reads a YAML config file, interpolates env vars, parses it,
// merges defaults, and validates the result.
func Load(path string, logger *slog.Logger) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	// Interpolate ${VAR_NAME} patterns from environment
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parsing config YAML: %w", err)
	}

	cfg.MergeDefaults()

	if err := cfg.Validate(logger); err != nil {
		return nil, err
	}

	return &cfg, nil
}
