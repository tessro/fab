// Package logging provides slog-based logging for the fab daemon.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// DefaultLogPath returns the default log file path (~/.fab/fab.log).
func DefaultLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/fab.log"
	}
	return filepath.Join(home, ".fab", "fab.log")
}

// Setup initializes the global slog logger to write to the specified path.
// If path is empty, uses DefaultLogPath().
// Returns a cleanup function to close the log file.
func Setup(path string) (cleanup func(), err error) {
	if path == "" {
		path = DefaultLogPath()
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	// Open log file (append mode, create if not exists)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}

	// Create JSON handler for structured logging
	handler := slog.NewJSONHandler(f, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	// Set as default logger
	slog.SetDefault(slog.New(handler))

	return func() { f.Close() }, nil
}

// SetupMulti initializes logging to both file and an additional writer (e.g., stderr).
// Useful for development when you want console output too.
func SetupMulti(path string, extra io.Writer) (cleanup func(), err error) {
	if path == "" {
		path = DefaultLogPath()
	}

	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}

	// Open log file
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}

	// Create multi-writer
	w := io.MultiWriter(f, extra)

	handler := slog.NewJSONHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})

	slog.SetDefault(slog.New(handler))

	return func() { f.Close() }, nil
}

// SetupTest configures logging for tests (writes to provided writer, text format).
func SetupTest(w io.Writer) {
	handler := slog.NewTextHandler(w, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	})
	slog.SetDefault(slog.New(handler))
}
