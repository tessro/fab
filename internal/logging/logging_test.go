package logging

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestParseLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  slog.Level
	}{
		{"debug lowercase", "debug", slog.LevelDebug},
		{"debug uppercase", "DEBUG", slog.LevelDebug},
		{"debug mixed", "Debug", slog.LevelDebug},
		{"info lowercase", "info", slog.LevelInfo},
		{"info uppercase", "INFO", slog.LevelInfo},
		{"warn lowercase", "warn", slog.LevelWarn},
		{"warn uppercase", "WARN", slog.LevelWarn},
		{"error lowercase", "error", slog.LevelError},
		{"error uppercase", "ERROR", slog.LevelError},
		{"empty string", "", slog.LevelInfo},
		{"invalid value", "invalid", slog.LevelInfo},
		{"trace returns info", "trace", slog.LevelInfo},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseLevel(tt.input); got != tt.want {
				t.Errorf("ParseLevel(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestTruncateForLog(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		maxLen int
		want   string
	}{
		{
			name:   "short string unchanged",
			input:  "hello",
			maxLen: 10,
			want:   "hello",
		},
		{
			name:   "exact length unchanged",
			input:  "hello",
			maxLen: 5,
			want:   "hello",
		},
		{
			name:   "long string truncated with ellipsis",
			input:  "hello world",
			maxLen: 8,
			want:   "hello...",
		},
		{
			name:   "very short maxLen",
			input:  "hello",
			maxLen: 2,
			want:   "he",
		},
		{
			name:   "maxLen of 3",
			input:  "hello",
			maxLen: 3,
			want:   "hel",
		},
		{
			name:   "maxLen of 4 allows ellipsis",
			input:  "hello",
			maxLen: 4,
			want:   "h...",
		},
		{
			name:   "empty string",
			input:  "",
			maxLen: 10,
			want:   "",
		},
		{
			name:   "typical log truncation",
			input:  "This is a very long tool input that contains a lot of data and should be truncated for logging purposes",
			maxLen: 50,
			want:   "This is a very long tool input that contains a ...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateForLog(tt.input, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateForLog(%q, %d) = %q, want %q", tt.input, tt.maxLen, got, tt.want)
			}
		})
	}
}

func TestRotatingWriter(t *testing.T) {
	t.Run("creates file on first write", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")

		w, err := newRotatingWriter(path, 1000)
		if err != nil {
			t.Fatalf("newRotatingWriter() error = %v", err)
		}
		defer w.Close()

		_, err = w.Write([]byte("hello\n"))
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile() error = %v", err)
		}
		if string(content) != "hello\n" {
			t.Errorf("file content = %q, want %q", content, "hello\n")
		}
	})

	t.Run("rotates when exceeding max size", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		backupPath := path + ".1"

		// Use small max size for testing
		w, err := newRotatingWriter(path, 20)
		if err != nil {
			t.Fatalf("newRotatingWriter() error = %v", err)
		}
		defer w.Close()

		// Write 15 bytes
		_, err = w.Write([]byte("first message!\n"))
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		// Write 16 more bytes - should trigger rotation
		_, err = w.Write([]byte("second message!\n"))
		if err != nil {
			t.Fatalf("Write() error = %v", err)
		}

		// Check backup file exists with first message
		backupContent, err := os.ReadFile(backupPath)
		if err != nil {
			t.Fatalf("ReadFile(backup) error = %v", err)
		}
		if string(backupContent) != "first message!\n" {
			t.Errorf("backup content = %q, want %q", backupContent, "first message!\n")
		}

		// Check current file has second message
		currentContent, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(current) error = %v", err)
		}
		if string(currentContent) != "second message!\n" {
			t.Errorf("current content = %q, want %q", currentContent, "second message!\n")
		}
	})

	t.Run("replaces old backup on rotation", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")
		backupPath := path + ".1"

		// Max size of 5, each message is 5 bytes
		// msg1 (5 bytes) -> file has 5 bytes
		// msg2 (5 bytes) -> would make 10, triggers rotation, msg1 goes to backup, msg2 written
		// msg3 (5 bytes) -> would make 10, triggers rotation, msg2 goes to backup, msg3 written
		w, err := newRotatingWriter(path, 5)
		if err != nil {
			t.Fatalf("newRotatingWriter() error = %v", err)
		}
		defer w.Close()

		// Write message 1 - fits exactly at limit
		_, _ = w.Write([]byte("msg1\n"))
		// Write message 2 - triggers rotation, msg1 goes to backup
		_, _ = w.Write([]byte("msg2\n"))
		// Write message 3 - triggers rotation, msg2 goes to backup (msg1 discarded)
		_, _ = w.Write([]byte("msg3\n"))

		backupContent, err := os.ReadFile(backupPath)
		if err != nil {
			t.Fatalf("ReadFile(backup) error = %v", err)
		}
		if string(backupContent) != "msg2\n" {
			t.Errorf("backup content = %q, want %q", backupContent, "msg2\n")
		}

		currentContent, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("ReadFile(current) error = %v", err)
		}
		if string(currentContent) != "msg3\n" {
			t.Errorf("current content = %q, want %q", currentContent, "msg3\n")
		}
	})

	t.Run("tracks size correctly", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")

		w, err := newRotatingWriter(path, 1000)
		if err != nil {
			t.Fatalf("newRotatingWriter() error = %v", err)
		}
		defer w.Close()

		_, _ = w.Write([]byte("hello"))
		if w.size != 5 {
			t.Errorf("size = %d, want 5", w.size)
		}

		_, _ = w.Write([]byte("world"))
		if w.size != 10 {
			t.Errorf("size = %d, want 10", w.size)
		}
	})

	t.Run("picks up existing file size", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "test.log")

		// Pre-create file with content
		if err := os.WriteFile(path, []byte("existing content\n"), 0600); err != nil {
			t.Fatalf("WriteFile() error = %v", err)
		}

		w, err := newRotatingWriter(path, 1000)
		if err != nil {
			t.Fatalf("newRotatingWriter() error = %v", err)
		}
		defer w.Close()

		// Size should reflect existing content
		if w.size != 17 {
			t.Errorf("size = %d, want 17", w.size)
		}
	})
}
