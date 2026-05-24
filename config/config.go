// Package config provides configuration loading for the Cog memory service.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Rule defines a path-pattern permission rule for RBAC.
type Rule struct {
	Pattern string `yaml:"pattern"`
	Read    bool   `yaml:"read"`
	Write   bool   `yaml:"write"`
}

// RBACConfig maps role names to ordered permission rules.
type RBACConfig struct {
	Roles map[string][]Rule `yaml:"roles"`
}

// Config holds all configuration for the memory service.
type Config struct {
	SocketPath  string     `yaml:"socket_path"`
	MemoryRoot  string     `yaml:"memory_root"`
	LogLevel    string     `yaml:"log_level"`
	WatchdogSec int        `yaml:"watchdog_sec"`
	RBAC        RBACConfig `yaml:"rbac"`
}

// LoadConfig reads and parses a YAML config file from the given path.
// Tilde in path fields is expanded to the user's home directory.
// When config_path itself starts with ~, it is expanded before opening.
func LoadConfig(configPath string) (*Config, error) {
	configPath = expandTilde(configPath)

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("config: read %q: %w", configPath, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: parse %q: %w", configPath, err)
	}

	// Expand tildes in path fields.
	cfg.SocketPath = expandTilde(cfg.SocketPath)
	cfg.MemoryRoot = expandTilde(cfg.MemoryRoot)

	// Apply defaults.
	if cfg.SocketPath == "" {
		if stateHome := os.Getenv("STATE_HOME"); stateHome != "" {
			cfg.SocketPath = filepath.Join(stateHome, "memory.sock")
		} else {
			cfg.SocketPath = "/tmp/cogmemory.sock"
		}
	}
	if cfg.LogLevel == "" {
		cfg.LogLevel = "info"
	}
	if cfg.WatchdogSec == 0 {
		cfg.WatchdogSec = 30
	}

	// Validate.
	if cfg.MemoryRoot == "" {
		return nil, fmt.Errorf("config: memory_root is required")
	}
	if !filepath.IsAbs(cfg.SocketPath) {
		return nil, fmt.Errorf("config: socket_path must be absolute, got %q", cfg.SocketPath)
	}
	if !filepath.IsAbs(cfg.MemoryRoot) {
		return nil, fmt.Errorf("config: memory_root must be absolute, got %q", cfg.MemoryRoot)
	}

	return &cfg, nil
}

// expandTilde replaces a leading ~ with the current user's home directory.
func expandTilde(p string) string {
	if !strings.HasPrefix(p, "~") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}
