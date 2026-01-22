// Package e2e provides end-to-end tests for fab CLI commands.
package e2e

import (
	"strings"
	"testing"
	"time"
)

// TestDirectorLifecycle tests the director agent start/stop/restart lifecycle.
func TestDirectorLifecycle(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create temp directory with short path for Unix socket
	fabDir, cleanup := shortTempDir(t)
	defer cleanup()

	// Build fab binary
	binary := buildFab(t, fabDir)

	// Create git repo for testing (director needs at least one project)
	localRepo, _ := createGitRepo(t, fabDir, "director-test")

	// Track server process for cleanup
	var serverCmd *serverProcess
	defer func() {
		if serverCmd != nil {
			serverCmd.stop()
		}
	}()

	// Start server
	serverCmd = startServer(t, binary, fabDir)

	// Add a project so the director has something to coordinate
	_, stderr, err := runFab(t, binary, fabDir, "project", "add", localRepo, "--name", "director-test")
	if err != nil {
		t.Fatalf("failed to add project: %v\nstderr: %s", err, stderr)
	}

	t.Run("initial_status_stopped", func(t *testing.T) {
		stdout, stderr, err := runFab(t, binary, fabDir, "director", "status")
		if err != nil {
			t.Fatalf("fab director status failed: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stdout, "stopped") {
			t.Errorf("expected director to be stopped initially, got: %s", stdout)
		}
	})

	t.Run("director_start", func(t *testing.T) {
		stdout, stderr, err := runFab(t, binary, fabDir, "director", "start")
		if err != nil {
			t.Fatalf("fab director start failed: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stdout, "started") {
			t.Errorf("expected 'started' in output, got: %s", stdout)
		}
	})

	t.Run("status_running", func(t *testing.T) {
		// Wait briefly for the director to reach running state
		time.Sleep(500 * time.Millisecond)

		stdout, stderr, err := runFab(t, binary, fabDir, "director", "status")
		if err != nil {
			t.Fatalf("fab director status failed: %v\nstderr: %s", err, stderr)
		}
		// Director should show running state (either "running" or "starting")
		if !strings.Contains(stdout, "running") && !strings.Contains(stdout, "starting") {
			t.Errorf("expected director to be running, got: %s", stdout)
		}
	})

	t.Run("start_when_already_running", func(t *testing.T) {
		// Starting a running director should error
		_, stderr, err := runFab(t, binary, fabDir, "director", "start")
		if err == nil {
			t.Error("expected error when starting already running director")
		}
		if !strings.Contains(stderr, "already running") {
			t.Errorf("expected 'already running' error, got: %s", stderr)
		}
	})

	t.Run("director_stop", func(t *testing.T) {
		stdout, stderr, err := runFab(t, binary, fabDir, "director", "stop")
		if err != nil {
			t.Fatalf("fab director stop failed: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stdout, "stopped") {
			t.Errorf("expected 'stopped' in output, got: %s", stdout)
		}
	})

	t.Run("status_stopped_after_stop", func(t *testing.T) {
		stdout, stderr, err := runFab(t, binary, fabDir, "director", "status")
		if err != nil {
			t.Fatalf("fab director status failed: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stdout, "stopped") {
			t.Errorf("expected director to be stopped, got: %s", stdout)
		}
	})

	t.Run("stop_when_not_running", func(t *testing.T) {
		// Stopping a stopped director should error
		_, stderr, err := runFab(t, binary, fabDir, "director", "stop")
		if err == nil {
			t.Error("expected error when stopping already stopped director")
		}
		if !strings.Contains(stderr, "not running") {
			t.Errorf("expected 'not running' error, got: %s", stderr)
		}
	})

	t.Run("director_restart", func(t *testing.T) {
		// Start the director again
		stdout, stderr, err := runFab(t, binary, fabDir, "director", "start")
		if err != nil {
			t.Fatalf("fab director start failed on restart: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stdout, "started") {
			t.Errorf("expected 'started' in output, got: %s", stdout)
		}

		// Verify it's running
		time.Sleep(500 * time.Millisecond)
		stdout, stderr, err = runFab(t, binary, fabDir, "director", "status")
		if err != nil {
			t.Fatalf("fab director status failed: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stdout, "running") && !strings.Contains(stdout, "starting") {
			t.Errorf("expected director to be running after restart, got: %s", stdout)
		}

		// Stop it for cleanup
		_, _, _ = runFab(t, binary, fabDir, "director", "stop")
	})
}

// TestDirectorClearHistory tests the director's clear history command.
func TestDirectorClearHistory(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create temp directory with short path for Unix socket
	fabDir, cleanup := shortTempDir(t)
	defer cleanup()

	// Build fab binary
	binary := buildFab(t, fabDir)

	// Create git repo for testing
	localRepo, _ := createGitRepo(t, fabDir, "director-clear-test")

	// Track server process for cleanup
	var serverCmd *serverProcess
	defer func() {
		if serverCmd != nil {
			serverCmd.stop()
		}
	}()

	// Start server
	serverCmd = startServer(t, binary, fabDir)

	// Add a project
	_, stderr, err := runFab(t, binary, fabDir, "project", "add", localRepo, "--name", "director-clear-test")
	if err != nil {
		t.Fatalf("failed to add project: %v\nstderr: %s", err, stderr)
	}

	t.Run("clear_when_not_running", func(t *testing.T) {
		// Clearing when director not started should error
		_, stderr, err := runFab(t, binary, fabDir, "director", "clear")
		if err == nil {
			t.Error("expected error when clearing non-running director")
		}
		if !strings.Contains(stderr, "not running") {
			t.Errorf("expected 'not running' error, got: %s", stderr)
		}
	})

	t.Run("clear_when_running", func(t *testing.T) {
		// Start director
		_, stderr, err := runFab(t, binary, fabDir, "director", "start")
		if err != nil {
			t.Fatalf("fab director start failed: %v\nstderr: %s", err, stderr)
		}

		// Wait for it to be running
		time.Sleep(500 * time.Millisecond)

		// Clear should succeed
		stdout, stderr, err := runFab(t, binary, fabDir, "director", "clear")
		if err != nil {
			t.Fatalf("fab director clear failed: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stdout, "cleared") {
			t.Errorf("expected 'cleared' in output, got: %s", stdout)
		}

		// Stop for cleanup
		_, _, _ = runFab(t, binary, fabDir, "director", "stop")
	})
}

// serverProcess wraps a server subprocess for easier cleanup.
type serverProcess struct {
	cmd    *execCmd
	binary string
	fabDir string
}

// execCmd wraps exec.Cmd for testing.
type execCmd struct {
	process interface{ Kill() error }
	wait    func() error
}

// startServer starts the fab server in foreground mode and waits for it to be ready.
func startServer(t *testing.T, binary, fabDir string) *serverProcess {
	t.Helper()

	serverCmd := fabCmd(binary, fabDir, "server", "start", "--foreground")
	if err := serverCmd.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}

	if err := waitForServer(t, binary, fabDir, 10*time.Second); err != nil {
		_ = serverCmd.Process.Kill()
		_ = serverCmd.Wait()
		t.Fatalf("server did not start: %v", err)
	}

	return &serverProcess{
		cmd: &execCmd{
			process: serverCmd.Process,
			wait:    serverCmd.Wait,
		},
		binary: binary,
		fabDir: fabDir,
	}
}

// stop gracefully stops the server process.
func (s *serverProcess) stop() {
	if s.cmd == nil {
		return
	}

	// Try graceful shutdown first
	stopCmd := fabCmd(s.binary, s.fabDir, "server", "stop")
	_ = stopCmd.Run()

	// Force kill if still running
	time.Sleep(500 * time.Millisecond)
	_ = s.cmd.process.Kill()
	_ = s.cmd.wait()
}
