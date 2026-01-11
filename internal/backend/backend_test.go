package backend_test

import (
	"os/exec"
	"testing"

	"github.com/tessro/fab/internal/backend"
)

// mockBackend implements backend.Backend for testing.
type mockBackend struct{}

func (m *mockBackend) Name() string { return "mock" }

func (m *mockBackend) BuildCommand(cfg backend.CommandConfig) (*exec.Cmd, error) {
	cmd := exec.Command("echo", "test")
	cmd.Dir = cfg.WorkDir
	return cmd, nil
}

func (m *mockBackend) ParseStreamMessage(line []byte) (*backend.StreamMessage, error) {
	if len(line) == 0 {
		return nil, nil
	}
	return &backend.StreamMessage{Type: "test"}, nil
}

func (m *mockBackend) FormatInputMessage(content string, sessionID string) ([]byte, error) {
	return []byte(content), nil
}

func (m *mockBackend) HookSettings(fabPath string) map[string]any {
	return map[string]any{"test": true}
}

// Compile-time check that mockBackend implements backend.Backend.
var _ backend.Backend = (*mockBackend)(nil)

func TestBackendInterface(t *testing.T) {
	var b backend.Backend = &mockBackend{}

	t.Run("Name", func(t *testing.T) {
		if got := b.Name(); got != "mock" {
			t.Errorf("Name() = %q, want %q", got, "mock")
		}
	})

	t.Run("BuildCommand", func(t *testing.T) {
		cfg := backend.CommandConfig{
			WorkDir:   "/tmp",
			AgentID:   "test-agent",
			PluginDir: "/plugins",
		}
		cmd, err := b.BuildCommand(cfg)
		if err != nil {
			t.Fatalf("BuildCommand() error = %v", err)
		}
		if cmd == nil {
			t.Fatal("BuildCommand() returned nil command")
		}
		if cmd.Dir != "/tmp" {
			t.Errorf("BuildCommand() Dir = %q, want %q", cmd.Dir, "/tmp")
		}
	})

	t.Run("ParseStreamMessage", func(t *testing.T) {
		msg, err := b.ParseStreamMessage([]byte(`{"type":"test"}`))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil message")
		}
		if msg.Type != "test" {
			t.Errorf("ParseStreamMessage() Type = %q, want %q", msg.Type, "test")
		}
	})

	t.Run("ParseStreamMessage_empty", func(t *testing.T) {
		msg, err := b.ParseStreamMessage([]byte{})
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg != nil {
			t.Errorf("ParseStreamMessage() = %v, want nil for empty line", msg)
		}
	})

	t.Run("FormatInputMessage", func(t *testing.T) {
		data, err := b.FormatInputMessage("hello", "session-1")
		if err != nil {
			t.Fatalf("FormatInputMessage() error = %v", err)
		}
		if string(data) != "hello" {
			t.Errorf("FormatInputMessage() = %q, want %q", string(data), "hello")
		}
	})

	t.Run("HookSettings", func(t *testing.T) {
		settings := b.HookSettings("/usr/bin/fab")
		if settings == nil {
			t.Fatal("HookSettings() returned nil")
		}
		if settings["test"] != true {
			t.Errorf("HookSettings() missing expected key")
		}
	})
}
