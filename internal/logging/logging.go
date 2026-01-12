// Package logging provides slog-based logging for the fab daemon.
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
)

// MaxLogSize is the maximum size in bytes before log rotation (5MB).
const MaxLogSize = 5 * 1024 * 1024

// DefaultLogPath returns the default log file path.
// If FAB_DIR is set, uses $FAB_DIR/fab.log.
// Otherwise uses ~/.fab/fab.log.
func DefaultLogPath() string {
	if fabDir := os.Getenv("FAB_DIR"); fabDir != "" {
		return filepath.Join(fabDir, "fab.log")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/fab.log"
	}
	return filepath.Join(home, ".fab", "fab.log")
}

// ParseLevel converts a log level string to slog.Level.
// Valid values: "debug", "info", "warn", "error" (case-insensitive).
// Returns slog.LevelInfo for unrecognized values.
func ParseLevel(level string) slog.Level {
	switch strings.ToLower(level) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Setup initializes the global slog logger to write to the specified path.
// If path is empty, uses DefaultLogPath().
// The level parameter controls logging verbosity (use ParseLevel to convert from string).
// Returns a cleanup function to close the log file.
// The log file is automatically rotated when it exceeds MaxLogSize.
func Setup(path string, level slog.Level) (cleanup func(), err error) {
	if path == "" {
		path = DefaultLogPath()
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	// Open rotating log file
	w, err := newRotatingWriter(path, MaxLogSize)
	if err != nil {
		return nil, err
	}

	// Create JSON handler for structured logging
	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	})

	// Set as default logger
	slog.SetDefault(slog.New(handler))

	return func() { w.Close() }, nil
}

// SetupMulti initializes logging to both file and an additional writer (e.g., stderr).
// Useful for development when you want console output too.
// The level parameter controls logging verbosity (use ParseLevel to convert from string).
// The log file is automatically rotated when it exceeds MaxLogSize.
func SetupMulti(path string, extra io.Writer, level slog.Level) (cleanup func(), err error) {
	if path == "" {
		path = DefaultLogPath()
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	// Open rotating log file
	rw, err := newRotatingWriter(path, MaxLogSize)
	if err != nil {
		return nil, err
	}

	// Create multi-writer
	w := io.MultiWriter(rw, extra)

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: level,
	})

	slog.SetDefault(slog.New(handler))

	return func() { rw.Close() }, nil
}

// SetupTest configures logging for tests (writes to provided writer, text format).
func SetupTest(w io.Writer) {
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	slog.SetDefault(slog.New(handler))
}

// LogPanic logs a panic with stack trace and context.
// Use in a defer at the start of goroutines:
//
//	defer logging.LogPanic("goroutine-name", nil)
//
// Or with a recovery callback:
//
//	defer logging.LogPanic("goroutine-name", func(r any) { cleanup() })
func LogPanic(name string, onRecover func(any)) {
	if r := recover(); r != nil {
		slog.Error("panic recovered",
			"goroutine", name,
			"panic", r,
			"stack", string(captureStack()),
		)
		if onRecover != nil {
			onRecover(r)
		}
	}
}

// captureStack returns the current goroutine's stack trace.
func captureStack() []byte {
	buf := make([]byte, 4096)
	for {
		n := runtime.Stack(buf, false)
		if n < len(buf) {
			return buf[:n]
		}
		buf = make([]byte, len(buf)*2)
	}
}

// TruncateForLog truncates a string for logging, adding "..." if truncated.
// Useful for preventing log bloat from large tool inputs/outputs.
func TruncateForLog(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// rotatingWriter wraps a file and rotates it when it exceeds maxSize.
// It keeps at most one backup file (path + ".1").
type rotatingWriter struct {
	mu      sync.Mutex
	path    string
	maxSize int64
	file    *os.File
	size    int64
}

// newRotatingWriter creates a writer that rotates at maxSize bytes.
func newRotatingWriter(path string, maxSize int64) (*rotatingWriter, error) {
	w := &rotatingWriter{
		path:    path,
		maxSize: maxSize,
	}
	if err := w.openFile(); err != nil {
		return nil, err
	}
	return w, nil
}

// openFile opens the log file and records its current size.
func (w *rotatingWriter) openFile() error {
	f, err := os.OpenFile(w.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	w.file = f
	w.size = info.Size()
	return nil
}

// Write implements io.Writer with size-based rotation.
func (w *rotatingWriter) Write(p []byte) (n int, err error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	// Check if rotation is needed before writing
	if w.size+int64(len(p)) > w.maxSize {
		if err := w.rotate(); err != nil {
			// Can't use slog here (circular), write to stderr as last resort.
			// Continue writing to avoid losing data; file may grow beyond maxSize.
			fmt.Fprintf(os.Stderr, "fab: log rotation failed: %v\n", err)
		}
	}

	n, err = w.file.Write(p)
	w.size += int64(n)
	return n, err
}

// rotate closes current file, renames it to .1 (removing old .1), and opens new file.
func (w *rotatingWriter) rotate() error {
	if err := w.file.Close(); err != nil {
		return err
	}

	backupPath := w.path + ".1"
	// Remove old backup if exists
	os.Remove(backupPath)
	// Rename current to backup
	if err := os.Rename(w.path, backupPath); err != nil {
		// If rename fails, try to reopen original
		return w.openFile()
	}

	return w.openFile()
}

// Close closes the underlying file.
func (w *rotatingWriter) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.file.Close()
}
