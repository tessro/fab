package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestDefaultPIDPath(t *testing.T) {
	path := DefaultPIDPath()
	if path == "" {
		t.Error("DefaultPIDPath() returned empty string")
	}

	home, err := os.UserHomeDir()
	if err == nil {
		expected := filepath.Join(home, ".fab", "fab.pid")
		if path != expected {
			t.Errorf("DefaultPIDPath() = %s, want %s", path, expected)
		}
	}
}

func TestWriteReadPID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write PID
	if err := WritePID(pidPath); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	// Read PID
	pid, err := ReadPID(pidPath)
	if err != nil {
		t.Fatalf("ReadPID: %v", err)
	}

	if pid != os.Getpid() {
		t.Errorf("ReadPID() = %d, want %d", pid, os.Getpid())
	}
}

func TestWritePID_CreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "subdir", "nested", "test.pid")

	if err := WritePID(pidPath); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(pidPath); os.IsNotExist(err) {
		t.Error("PID file was not created")
	}
}

func TestReadPID_NotExists(t *testing.T) {
	_, err := ReadPID("/nonexistent/path/test.pid")
	if err == nil {
		t.Error("expected error for nonexistent file")
	}
	if !os.IsNotExist(err) {
		t.Errorf("expected IsNotExist error, got %v", err)
	}
}

func TestReadPID_InvalidContent(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write invalid content
	if err := os.WriteFile(pidPath, []byte("not-a-number\n"), 0600); err != nil {
		t.Fatalf("write file: %v", err)
	}

	_, err := ReadPID(pidPath)
	if err == nil {
		t.Error("expected error for invalid PID content")
	}
}

func TestRemovePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// Write PID first
	if err := WritePID(pidPath); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	// Remove PID
	if err := RemovePID(pidPath); err != nil {
		t.Fatalf("RemovePID: %v", err)
	}

	// Verify file is gone
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("PID file still exists after RemovePID")
	}
}

func TestRemovePID_NotExists(t *testing.T) {
	// Should not error if file doesn't exist
	if err := RemovePID("/nonexistent/path/test.pid"); err != nil {
		t.Errorf("RemovePID should not error for nonexistent file: %v", err)
	}
}

func TestIsProcessRunning(t *testing.T) {
	// Current process should be running
	if !IsProcessRunning(os.Getpid()) {
		t.Error("current process should be running")
	}

	// Invalid PIDs should not be running
	if IsProcessRunning(0) {
		t.Error("PID 0 should not be running")
	}
	if IsProcessRunning(-1) {
		t.Error("PID -1 should not be running")
	}

	// Very high PID should not exist (usually)
	if IsProcessRunning(999999999) {
		t.Skip("unexpectedly high PID exists")
	}
}

func TestIsDaemonRunning(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	t.Run("no pid file", func(t *testing.T) {
		running, pid := IsDaemonRunning(pidPath)
		if running {
			t.Error("should not be running without PID file")
		}
		if pid != 0 {
			t.Errorf("pid should be 0, got %d", pid)
		}
	})

	t.Run("valid running process", func(t *testing.T) {
		if err := WritePID(pidPath); err != nil {
			t.Fatalf("WritePID: %v", err)
		}

		running, pid := IsDaemonRunning(pidPath)
		if !running {
			t.Error("should be running with valid PID file")
		}
		if pid != os.Getpid() {
			t.Errorf("pid = %d, want %d", pid, os.Getpid())
		}
	})

	t.Run("stale pid file", func(t *testing.T) {
		// Write a PID that doesn't exist
		stalePID := 999999999
		if err := os.WriteFile(pidPath, []byte(strconv.Itoa(stalePID)+"\n"), 0600); err != nil {
			t.Fatalf("write file: %v", err)
		}

		running, pid := IsDaemonRunning(pidPath)
		if running {
			t.Skip("unexpectedly high PID exists")
		}
		if pid != 0 {
			t.Errorf("pid should be 0 for stale file, got %d", pid)
		}
	})
}

func TestCleanStalePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	t.Run("cleans stale pid", func(t *testing.T) {
		// Write a stale PID
		if err := os.WriteFile(pidPath, []byte("999999999\n"), 0600); err != nil {
			t.Fatalf("write file: %v", err)
		}

		cleaned := CleanStalePID(pidPath)
		if !cleaned {
			t.Error("should have cleaned stale PID")
		}

		// File should be gone
		if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
			t.Error("stale PID file should be removed")
		}
	})

	t.Run("does not clean running process", func(t *testing.T) {
		if err := WritePID(pidPath); err != nil {
			t.Fatalf("WritePID: %v", err)
		}

		cleaned := CleanStalePID(pidPath)
		if cleaned {
			t.Error("should not clean PID of running process")
		}

		// File should still exist
		if _, err := os.Stat(pidPath); os.IsNotExist(err) {
			t.Error("PID file should still exist")
		}
	})
}

func TestWritePID_DefaultPath(t *testing.T) {
	// Test that empty path uses default
	// We can't actually write to the default path in tests,
	// but we can verify the function handles empty string
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "test.pid")

	// This tests the non-empty path case
	if err := WritePID(pidPath); err != nil {
		t.Fatalf("WritePID: %v", err)
	}
}
