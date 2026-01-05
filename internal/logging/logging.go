// Package logging provides slog-based logging for the fab daemon.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
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

	return func() { _ = f.Close() }, nil
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

	return func() { _ = f.Close() }, nil
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
