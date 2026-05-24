package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bketelsen/cogmemory/config"
)

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "memory-service-*.yml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	return f.Name()
}

func TestLoadConfig_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")
	memRoot := filepath.Join(dir, "memory")

	content := `
socket_path: ` + socketPath + `
memory_root: ` + memRoot + `
log_level: debug
watchdog_sec: 60
`
	path := writeConfig(t, content)
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SocketPath != socketPath {
		t.Errorf("SocketPath = %q, want %q", cfg.SocketPath, socketPath)
	}
	if cfg.MemoryRoot != memRoot {
		t.Errorf("MemoryRoot = %q, want %q", cfg.MemoryRoot, memRoot)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("LogLevel = %q, want %q", cfg.LogLevel, "debug")
	}
	if cfg.WatchdogSec != 60 {
		t.Errorf("WatchdogSec = %d, want 60", cfg.WatchdogSec)
	}
}

func TestLoadConfig_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("cannot determine home dir")
	}
	dir := t.TempDir()
	memRoot := filepath.Join(dir, "memory")

	// Use ~ in socket_path
	content := `
socket_path: ~/test-cogmemory.sock
memory_root: ` + memRoot + `
`
	path := writeConfig(t, content)
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(home, "test-cogmemory.sock")
	if cfg.SocketPath != want {
		t.Errorf("SocketPath = %q, want %q", cfg.SocketPath, want)
	}
}

func TestLoadConfig_DefaultSocketPath_StateHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("STATE_HOME", dir)
	memRoot := filepath.Join(dir, "memory")

	content := `memory_root: ` + memRoot + `
`
	path := writeConfig(t, content)
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(dir, "memory.sock")
	if cfg.SocketPath != want {
		t.Errorf("SocketPath = %q, want %q", cfg.SocketPath, want)
	}
}

func TestLoadConfig_DefaultSocketPath_Fallback(t *testing.T) {
	t.Setenv("STATE_HOME", "")
	dir := t.TempDir()
	memRoot := filepath.Join(dir, "memory")

	content := `memory_root: ` + memRoot + `
`
	path := writeConfig(t, content)
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.SocketPath != "/tmp/cogmemory.sock" {
		t.Errorf("SocketPath = %q, want /tmp/cogmemory.sock", cfg.SocketPath)
	}
}

func TestLoadConfig_MissingMemoryRoot(t *testing.T) {
	dir := t.TempDir()
	socketPath := filepath.Join(dir, "test.sock")
	content := `socket_path: ` + socketPath + `
`
	path := writeConfig(t, content)
	_, err := config.LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing memory_root")
	}
	if !strings.Contains(err.Error(), "memory_root") {
		t.Errorf("error should mention memory_root, got: %v", err)
	}
}

func TestLoadConfig_RelativeSocketPathRejected(t *testing.T) {
	dir := t.TempDir()
	content := `
socket_path: relative/path.sock
memory_root: ` + dir + `
`
	path := writeConfig(t, content)
	_, err := config.LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for relative socket_path")
	}
	if !strings.Contains(err.Error(), "socket_path") {
		t.Errorf("error should mention socket_path, got: %v", err)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("STATE_HOME", "")
	dir := t.TempDir()
	content := `memory_root: ` + dir + `
`
	path := writeConfig(t, content)
	cfg, err := config.LoadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("default LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.WatchdogSec != 30 {
		t.Errorf("default WatchdogSec = %d, want 30", cfg.WatchdogSec)
	}
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	_, err := config.LoadConfig("/nonexistent/path/config.yml")
	if err == nil {
		t.Fatal("expected error for nonexistent config file")
	}
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	path := writeConfig(t, "{ invalid yaml: [missing")
	_, err := config.LoadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}
