package core

import (
	"errors"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Status describes one position in the status cycle.
type Status struct {
	Name   string `yaml:"name"`
	Prefix string `yaml:"prefix"`
}

// Config contains the ordered workflow statuses. Status zero is always none.
type Config struct {
	Statuses []Status `yaml:"statuses"`
}

// DefaultConfig returns a fresh copy of the built-in workflow configuration.
func DefaultConfig() Config {
	return Config{Statuses: []Status{
		{Name: "none", Prefix: ""},
		{Name: "wip", Prefix: "🔨 "},
		{Name: "review", Prefix: "👀 "},
		{Name: "test", Prefix: "🧪 "},
		{Name: "done", Prefix: "✅ "},
		{Name: "blocked", Prefix: "🚫 "},
	}}
}

// LoadConfig loads an explicit override when supplied. Otherwise it checks the
// canonical path followed by legacy paths. Missing discovered files are
// skipped; malformed or invalid files are returned as actionable errors.
func LoadConfig(canonical string, legacy []string, override string) (Config, error) {
	if override != "" {
		return loadConfigFile(override)
	}

	paths := make([]string, 0, 1+len(legacy))
	if canonical != "" {
		paths = append(paths, canonical)
	}
	paths = append(paths, legacy...)
	for _, path := range paths {
		if path == "" {
			continue
		}
		cfg, err := loadConfigFile(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return Config{}, err
		}
		return cfg, nil
	}
	return DefaultConfig(), nil
}

func loadConfigFile(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, fmt.Errorf("read config %q: %w", path, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse config %q: %w", path, err)
	}
	if err := validateConfig(cfg); err != nil {
		return Config{}, fmt.Errorf("invalid config %q: %w", path, err)
	}
	return cfg, nil
}

func validateConfig(cfg Config) error {
	if len(cfg.Statuses) == 0 {
		return errors.New("statuses must not be empty")
	}
	if cfg.Statuses[0].Name != "none" || cfg.Statuses[0].Prefix != "" {
		return errors.New("status zero must be named none and have an empty prefix")
	}
	names := make(map[string]struct{}, len(cfg.Statuses))
	prefixes := make(map[string]struct{}, len(cfg.Statuses))
	for i, status := range cfg.Statuses {
		if status.Name == "" {
			return fmt.Errorf("status %d has an empty name", i)
		}
		if _, exists := names[status.Name]; exists {
			return fmt.Errorf("duplicate status name %q", status.Name)
		}
		names[status.Name] = struct{}{}
		if _, exists := prefixes[status.Prefix]; exists {
			return fmt.Errorf("duplicate status prefix %q", status.Prefix)
		}
		prefixes[status.Prefix] = struct{}{}
	}
	return nil
}
