// Command cogmemory is the Cog memory service daemon.
// It provides centralized, concurrent-safe memory file operations
// over a Unix Domain Socket using JSON-RPC 2.0.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/bketelsen/cogmemory/config"
	"github.com/bketelsen/cogmemory/health"
	"github.com/bketelsen/cogmemory/rbac"
	"github.com/bketelsen/cogmemory/rpc"
	"github.com/bketelsen/cogmemory/store"
)

func defaultConfigPath() string {
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, "cogmemory", "config.yml")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/etc/cogmemory/config.yml"
	}
	return filepath.Join(home, ".config", "cogmemory", "config.yml")
}

func main() {
	configPath := flag.String("config", defaultConfigPath(), "Path to memory service config file")
	flag.Parse()

	cfg, err := config.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cogmemory: config error: %v\n", err)
		os.Exit(1)
	}

	s, err := store.New(cfg.MemoryRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cogmemory: store error: %v\n", err)
		os.Exit(1)
	}

	r := rbac.New(cfg.RBAC)
	srv := rpc.New(s, r)

	ln, err := rpc.Listen(cfg.SocketPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "cogmemory: listen error: %v\n", err)
		os.Exit(1)
	}

	log.Printf("cogmemory: listening on %s", cfg.SocketPath)

	// Start systemd watchdog
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	health.StartWatchdog(ctx, cfg.WatchdogSec)

	// Notify systemd we are ready
	health.NotifyReady()

	// Serve in background and wait for shutdown signal
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		srv.Serve(ln)
	}()

	health.GracefulShutdown(ln, &sync.WaitGroup{}, 30*time.Second)
	cancel()
	wg.Wait()

	log.Printf("cogmemory: shutdown complete")
}
