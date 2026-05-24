// Package health provides systemd watchdog integration and graceful shutdown.
package health

import (
	"context"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"
)

// NotifyReady sends READY=1 to the systemd notify socket.
// No-op if NOTIFY_SOCKET is not set.
func NotifyReady() {
	sdNotify("READY=1")
}

// StartWatchdog starts a goroutine that sends WATCHDOG=1 to systemd
// at intervalSec/2 intervals. Stops when ctx is cancelled.
func StartWatchdog(ctx context.Context, intervalSec int) {
	if os.Getenv("NOTIFY_SOCKET") == "" {
		return
	}
	interval := time.Duration(intervalSec/2) * time.Second
	if interval <= 0 {
		interval = 15 * time.Second
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sdNotify("WATCHDOG=1")
			}
		}
	}()
}

// GracefulShutdown listens for SIGTERM or SIGINT, closes the listener,
// then waits up to timeout for in-flight connections to finish.
func GracefulShutdown(ln net.Listener, wg *sync.WaitGroup, timeout time.Duration) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	<-sigCh

	// Stop accepting new connections
	ln.Close()

	// Wait for in-flight connections with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(timeout):
	}
}

// sdNotify sends a message to the systemd notify socket.
func sdNotify(state string) {
	notifySocket := os.Getenv("NOTIFY_SOCKET")
	if notifySocket == "" {
		return
	}

	conn, err := net.Dial("unixgram", notifySocket)
	if err != nil {
		return
	}
	defer conn.Close()
	conn.Write([]byte(state)) //nolint:errcheck
}
