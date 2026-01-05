package cli

import (
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/tessro/fab/internal/daemon"
)

// ErrDaemonNotRunning indicates the daemon is not running.
var ErrDaemonNotRunning = errors.New("daemon is not running")

// socketPath is the path to the daemon socket (can be overridden for testing).
var socketPath string

// SetSocketPath overrides the default socket path.
// This is primarily useful for testing.
func SetSocketPath(path string) {
	socketPath = path
}

// getSocketPath returns the socket path to use.
func getSocketPath() string {
	if socketPath != "" {
		return socketPath
	}
	return daemon.DefaultSocketPath()
}

// NewClient creates a new daemon client with the configured socket path.
func NewClient() *daemon.Client {
	return daemon.NewClient(getSocketPath())
}

// ConnectClient creates and connects a daemon client.
// Returns ErrDaemonNotRunning if the daemon is not running.
func ConnectClient() (*daemon.Client, error) {
	client := NewClient()
	if err := client.Connect(); err != nil {
		// Check if this is a connection refused error
		var opErr *net.OpError
		if errors.As(err, &opErr) {
			if os.IsNotExist(opErr.Err) || isConnectionRefused(opErr) {
				return nil, ErrDaemonNotRunning
			}
		}
		return nil, fmt.Errorf("connect to daemon: %w", err)
	}
	return client, nil
}

// isConnectionRefused checks if the error is a connection refused error.
func isConnectionRefused(err *net.OpError) bool {
	// Check for ECONNREFUSED-like errors
	if err.Err != nil {
		errStr := err.Err.Error()
		return errStr == "connect: connection refused" ||
			errStr == "connect: no such file or directory"
	}
	return false
}

// MustConnect creates and connects a daemon client, exiting on failure.
// This is a convenience function for CLI commands that require a daemon connection.
func MustConnect() *daemon.Client {
	client, err := ConnectClient()
	if err != nil {
		if errors.Is(err, ErrDaemonNotRunning) {
			fmt.Fprintln(os.Stderr, "🚌 fab daemon is not running")
			fmt.Fprintln(os.Stderr, "   Start it with: fab server start")
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "🚌 Error connecting to daemon: %v\n", err)
		os.Exit(1)
	}
	return client
}

// IsDaemonRunning checks if the daemon is running without establishing a persistent connection.
func IsDaemonRunning() bool {
	client := NewClient()
	if err := client.Connect(); err != nil {
		return false
	}
	client.Close()
	return true
}
