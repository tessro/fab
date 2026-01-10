// Package logging provides slog-based logging for the fab daemon.
package logging

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DefaultLogPath returns the default log file path (~/.fab/fab.log).
func DefaultLogPath() string {
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
func Setup(path string, level slog.Level) (cleanup func(), err error) {
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
		Level: level,
	})

	// Set as default logger
	slog.SetDefault(slog.New(handler))

	return func() { f.Close() }, nil
}

// SetupMulti initializes logging to both file and an additional writer (e.g., stderr).
// Useful for development when you want console output too.
// The level parameter controls logging verbosity (use ParseLevel to convert from string).
func SetupMulti(path string, extra io.Writer, level slog.Level) (cleanup func(), err error) {
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
		Level: level,
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
