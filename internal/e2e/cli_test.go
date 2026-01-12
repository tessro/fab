// Package e2e provides end-to-end tests for fab CLI commands.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// shortTempDir creates a temp directory with a short path for socket tests.
// Unix sockets have a path limit (~104 chars on macOS), and t.TempDir()
// includes the full test name which can exceed this limit.
func shortTempDir(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "fab-e2e-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	return dir, func() { os.RemoveAll(dir) }
}

// buildFab builds the fab binary into the given directory.
func buildFab(t *testing.T, dir string) string {
	t.Helper()
	binary := filepath.Join(dir, "fab")

	// Get the module root (parent of internal/)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	// Navigate up from internal/e2e to module root
	moduleRoot := filepath.Dir(filepath.Dir(wd))

	cmd := exec.Command("go", "build", "-o", binary, "./cmd/fab")
	cmd.Dir = moduleRoot
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to build fab: %v", err)
	}

	return binary
}

// fabCmd creates a command to run fab with the given FAB_DIR.
func fabCmd(binary, fabDir string, args ...string) *exec.Cmd {
	cmd := exec.Command(binary, args...)
	cmd.Env = append(os.Environ(), "FAB_DIR="+fabDir)
	return cmd
}

// runFab runs fab with the given args and FAB_DIR, returning stdout and stderr.
func runFab(t *testing.T, binary, fabDir string, args ...string) (string, string, error) {
	t.Helper()
	cmd := fabCmd(binary, fabDir, args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

// waitForServer polls fab status until the server is ready or timeout.
func waitForServer(t *testing.T, binary, fabDir string, timeout time.Duration) error {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout waiting for server to start")
		case <-ticker.C:
			cmd := fabCmd(binary, fabDir, "status")
			if err := cmd.Run(); err == nil {
				return nil
			}
		}
	}
}

// createGitRepo creates a local git repo with a bare remote for testing.
// Returns the local repo path and remote URL (in file:// format).
func createGitRepo(t *testing.T, baseDir, name string) (string, string) {
	t.Helper()

	// Create bare remote
	remoteDir := filepath.Join(baseDir, name+"-remote.git")
	if err := os.MkdirAll(remoteDir, 0755); err != nil {
		t.Fatalf("failed to create remote dir: %v", err)
	}
	runGit(t, remoteDir, "init", "--bare")

	// Use file:// URL format for the remote (required by fab's URL validation)
	remoteURL := "file://" + remoteDir

	// Create local repo
	localDir := filepath.Join(baseDir, name)
	if err := os.MkdirAll(localDir, 0755); err != nil {
		t.Fatalf("failed to create local dir: %v", err)
	}
	runGit(t, localDir, "init")
	runGit(t, localDir, "config", "user.email", "test@example.com")
	runGit(t, localDir, "config", "user.name", "Test User")
	runGit(t, localDir, "remote", "add", "origin", remoteURL)

	// Create initial commit
	readmePath := filepath.Join(localDir, "README.md")
	if err := os.WriteFile(readmePath, []byte("# Test Project\n"), 0644); err != nil {
		t.Fatalf("failed to write readme: %v", err)
	}
	runGit(t, localDir, "add", ".")
	runGit(t, localDir, "commit", "-m", "Initial commit")
	runGit(t, localDir, "push", "-u", "origin", "main")

	return localDir, remoteURL
}

// runGit runs a git command in the given directory.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, stderr.String())
	}
}

// TestFabCLI runs an end-to-end test of core fab commands.
// It builds the fab binary, starts a server in an isolated FAB_DIR,
// and tests project add/list/remove operations.
func TestFabCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create temp directory with short path for Unix socket
	fabDir, cleanup := shortTempDir(t)
	defer cleanup()

	// Build fab binary
	binary := buildFab(t, fabDir)

	// Create git repo for testing
	// localRepo has origin pointing to remoteRepo (a bare repo)
	localRepo, _ := createGitRepo(t, fabDir, "e2e-project")

	// Track server process for cleanup
	var serverCmd *exec.Cmd
	defer func() {
		if serverCmd != nil && serverCmd.Process != nil {
			// Try graceful shutdown first
			stopCmd := fabCmd(binary, fabDir, "server", "stop")
			_ = stopCmd.Run() // Ignore errors

			// Force kill if still running
			time.Sleep(500 * time.Millisecond)
			_ = serverCmd.Process.Kill()
			_ = serverCmd.Wait()
		}
	}()

	// Test sequence
	t.Run("server_start", func(t *testing.T) {
		// Start server in foreground as a subprocess
		serverCmd = fabCmd(binary, fabDir, "server", "start", "--foreground")
		if err := serverCmd.Start(); err != nil {
			t.Fatalf("failed to start server: %v", err)
		}

		// Wait for server to be ready
		if err := waitForServer(t, binary, fabDir, 10*time.Second); err != nil {
			t.Fatalf("server did not start: %v", err)
		}
	})

	t.Run("status", func(t *testing.T) {
		stdout, stderr, err := runFab(t, binary, fabDir, "status")
		if err != nil {
			t.Fatalf("fab status failed: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stdout, "fab daemon") {
			t.Errorf("unexpected status output: %s", stdout)
		}
	})

	t.Run("project_add", func(t *testing.T) {
		// Use localRepo which has origin configured (pointing to the bare remote)
		stdout, stderr, err := runFab(t, binary, fabDir, "project", "add", localRepo, "--name", "e2e")
		if err != nil {
			t.Fatalf("fab project add failed: %v\nstderr: %s\nstdout: %s", err, stderr, stdout)
		}
		if !strings.Contains(stdout, "e2e") && !strings.Contains(stderr, "e2e") {
			t.Errorf("expected project name in output, got stdout: %s, stderr: %s", stdout, stderr)
		}
	})

	t.Run("project_list", func(t *testing.T) {
		stdout, stderr, err := runFab(t, binary, fabDir, "project", "list")
		if err != nil {
			t.Fatalf("fab project list failed: %v\nstderr: %s", err, stderr)
		}
		if !strings.Contains(stdout, "e2e") {
			t.Errorf("project 'e2e' not found in list output: %s", stdout)
		}
	})

	t.Run("project_remove", func(t *testing.T) {
		stdout, stderr, err := runFab(t, binary, fabDir, "project", "remove", "e2e", "--force", "--delete-worktrees")
		if err != nil {
			t.Fatalf("fab project remove failed: %v\nstderr: %s", err, stderr)
		}
		_ = stdout // Success is enough

		// Verify project is gone
		stdout, stderr, err = runFab(t, binary, fabDir, "project", "list")
		if err != nil {
			t.Fatalf("fab project list failed: %v\nstderr: %s", err, stderr)
		}
		if strings.Contains(stdout, "e2e") {
			t.Errorf("project 'e2e' should be removed, but found in list: %s", stdout)
		}
	})

	t.Run("server_stop", func(t *testing.T) {
		stdout, stderr, err := runFab(t, binary, fabDir, "server", "stop")
		if err != nil {
			t.Fatalf("fab server stop failed: %v\nstderr: %s", err, stderr)
		}
		_ = stdout // Success is enough

		// Wait for process to exit
		done := make(chan error, 1)
		go func() {
			done <- serverCmd.Wait()
		}()

		select {
		case <-done:
			serverCmd = nil // Clear so defer doesn't try to kill
		case <-time.After(5 * time.Second):
			t.Error("server did not stop within timeout")
		}
	})

	t.Run("isolation_verified", func(t *testing.T) {
		// Verify no files were created in ~/.fab or ~/.config/fab
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatalf("failed to get home dir: %v", err)
		}

		fabHome := filepath.Join(home, ".fab")
		configFab := filepath.Join(home, ".config", "fab")

		// Check that we didn't touch the user's real fab directory
		// by verifying our test project doesn't exist there
		configPath := filepath.Join(configFab, "config.toml")
		if _, err := os.Stat(configPath); err == nil {
			data, _ := os.ReadFile(configPath)
			if strings.Contains(string(data), "e2e-project") {
				t.Error("test project found in user's ~/.config/fab - isolation failed")
			}
		}

		projectsDir := filepath.Join(fabHome, "projects", "e2e")
		if _, err := os.Stat(projectsDir); err == nil {
			t.Error("test project found in user's ~/.fab/projects - isolation failed")
		}
	})
}

// TestFabCLIIssues tests issue commands in isolation.
// Note: This requires a project with tk backend to be set up.
func TestFabCLIIssues(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}

	// Create temp directory with short path for Unix socket
	fabDir, cleanup := shortTempDir(t)
	defer cleanup()

	// Build fab binary
	binary := buildFab(t, fabDir)

	// Create git repo for testing
	localRepo, _ := createGitRepo(t, fabDir, "e2e-issues")

	// Track server process for cleanup
	var serverCmd *exec.Cmd
	defer func() {
		if serverCmd != nil && serverCmd.Process != nil {
			stopCmd := fabCmd(binary, fabDir, "server", "stop")
			_ = stopCmd.Run()
			time.Sleep(500 * time.Millisecond)
			_ = serverCmd.Process.Kill()
			_ = serverCmd.Wait()
		}
	}()

	// Start server
	serverCmd = fabCmd(binary, fabDir, "server", "start", "--foreground")
	if err := serverCmd.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	if err := waitForServer(t, binary, fabDir, 10*time.Second); err != nil {
		t.Fatalf("server did not start: %v", err)
	}

	// Add project using localRepo (which has origin configured)
	_, stderr, err := runFab(t, binary, fabDir, "project", "add", localRepo, "--name", "e2e-issues")
	if err != nil {
		t.Fatalf("failed to add project: %v\nstderr: %s", err, stderr)
	}

	t.Run("issue_create", func(t *testing.T) {
		// Create an issue using the tk backend
		// Need to run from the project's repo directory
		cmd := fabCmd(binary, fabDir, "issue", "create", "Test issue", "--project", "e2e-issues")
		cmd.Dir = localRepo

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			t.Fatalf("fab issue create failed: %v\nstderr: %s", err, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Created issue") {
			t.Errorf("expected 'Created issue' in output, got: %s", stdout.String())
		}
	})

	t.Run("issue_list", func(t *testing.T) {
		cmd := fabCmd(binary, fabDir, "issue", "list", "--project", "e2e-issues")
		cmd.Dir = localRepo

		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		err := cmd.Run()
		if err != nil {
			t.Fatalf("fab issue list failed: %v\nstderr: %s", err, stderr.String())
		}
		if !strings.Contains(stdout.String(), "Test issue") {
			t.Errorf("expected 'Test issue' in list output, got: %s", stdout.String())
		}
	})

	// Cleanup
	_, _, _ = runFab(t, binary, fabDir, "server", "stop")
	_ = serverCmd.Wait()
	serverCmd = nil
}
