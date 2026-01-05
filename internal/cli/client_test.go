package cli

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/tessro/fab/internal/daemon"
)

func TestNewClient(t *testing.T) {
	// Reset socket path after test
	defer SetSocketPath("")

	t.Run("uses default path", func(t *testing.T) {
		SetSocketPath("")
		client := NewClient()
		if client.SocketPath() != daemon.DefaultSocketPath() {
			t.Errorf("expected default socket path, got %s", client.SocketPath())
		}
	})

	t.Run("uses custom path", func(t *testing.T) {
		SetSocketPath("/custom/path.sock")
		client := NewClient()
		if client.SocketPath() != "/custom/path.sock" {
			t.Errorf("expected /custom/path.sock, got %s", client.SocketPath())
		}
	})
}

func TestConnectClient(t *testing.T) {
	// Reset socket path after test
	defer SetSocketPath("")

	t.Run("returns error when daemon not running", func(t *testing.T) {
		SetSocketPath("/nonexistent/path/test.sock")
		_, err := ConnectClient()
		if err == nil {
			t.Fatal("expected error")
		}
		if !errors.Is(err, ErrDaemonNotRunning) {
			t.Errorf("expected ErrDaemonNotRunning, got %v", err)
		}
	})

	t.Run("connects to running daemon", func(t *testing.T) {
		tmpDir := t.TempDir()
		sockPath := filepath.Join(tmpDir, "test.sock")

		handler := daemon.HandlerFunc(func(ctx context.Context, req *daemon.Request) *daemon.Response {
			return &daemon.Response{Success: true}
		})

		srv := daemon.NewServer(sockPath, handler)
		if err := srv.Start(); err != nil {
			t.Fatalf("server start: %v", err)
		}
		defer func() { _ = srv.Stop() }()

		SetSocketPath(sockPath)
		client, err := ConnectClient()
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		defer func() { _ = client.Close() }()

		if !client.IsConnected() {
			t.Error("client should be connected")
		}
	})
}

func TestIsDaemonRunning(t *testing.T) {
	// Reset socket path after test
	defer SetSocketPath("")

	t.Run("returns false when daemon not running", func(t *testing.T) {
		SetSocketPath("/nonexistent/path/test.sock")
		if IsDaemonRunning() {
			t.Error("expected false when daemon not running")
		}
	})

	t.Run("returns true when daemon running", func(t *testing.T) {
		tmpDir := t.TempDir()
		sockPath := filepath.Join(tmpDir, "test.sock")

		handler := daemon.HandlerFunc(func(ctx context.Context, req *daemon.Request) *daemon.Response {
			return &daemon.Response{Success: true}
		})

		srv := daemon.NewServer(sockPath, handler)
		if err := srv.Start(); err != nil {
			t.Fatalf("server start: %v", err)
		}
		defer func() { _ = srv.Stop() }()

		SetSocketPath(sockPath)
		if !IsDaemonRunning() {
			t.Error("expected true when daemon running")
		}
	})
}
