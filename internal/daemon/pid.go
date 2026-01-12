// Package daemon provides the fab daemon server and IPC protocol.
package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/tessro/fab/internal/paths"
)

// DefaultPIDPath returns the default PID file path.
func DefaultPIDPath() string {
	return paths.PIDPath()
}

// WritePID writes the current process ID to the PID file.
// It creates the parent directory if it doesn't exist.
func WritePID(path string) error {
	if path == "" {
		path = DefaultPIDPath()
	}

	// Ensure parent directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create pid directory: %w", err)
	}

	// Write PID to file
	pid := os.Getpid()
	data := []byte(strconv.Itoa(pid) + "\n")
	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write pid file: %w", err)
	}

	return nil
}

// ReadPID reads the process ID from the PID file.
// Returns 0 and an error if the file doesn't exist or is invalid.
func ReadPID(path string) (int, error) {
	if path == "" {
		path = DefaultPIDPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, err
		}
		return 0, fmt.Errorf("read pid file: %w", err)
	}

	pidStr := strings.TrimSpace(string(data))
	pid, err := strconv.Atoi(pidStr)
	if err != nil {
		return 0, fmt.Errorf("parse pid: %w", err)
	}

	return pid, nil
}

// RemovePID removes the PID file.
// It returns nil if the file doesn't exist.
func RemovePID(path string) error {
	if path == "" {
		path = DefaultPIDPath()
	}

	err := os.Remove(path)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove pid file: %w", err)
	}
	return nil
}

// IsProcessRunning checks if a process with the given PID is running.
func IsProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}

	// Send signal 0 to check if process exists
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	err = process.Signal(syscall.Signal(0))
	if err != nil {
		// Process doesn't exist or we don't have permission
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return false
		}
		// EPERM means process exists but we can't signal it
		if errors.Is(err, syscall.EPERM) {
			return true
		}
		return false
	}

	return true
}

// IsDaemonRunning checks if the daemon is running by reading the PID file
// and verifying the process exists.
func IsDaemonRunning(pidPath string) (bool, int) {
	pid, err := ReadPID(pidPath)
	if err != nil {
		return false, 0
	}

	if IsProcessRunning(pid) {
		return true, pid
	}

	// Stale PID file - process not running
	return false, 0
}

// CleanStalePID removes the PID file if the process is not running.
// Returns true if a stale PID file was cleaned up.
func CleanStalePID(pidPath string) bool {
	running, _ := IsDaemonRunning(pidPath)
	if !running {
		_ = RemovePID(pidPath)
		return true
	}
	return false
}
