package backend_test

import (
	"os/exec"
	"slices"
	"testing"

	"github.com/tessro/fab/internal/backend"
)

// testBackend implements backend.Backend for registry testing.
type testBackend struct {
	name string
}

func (b *testBackend) Name() string { return b.name }

func (b *testBackend) BuildCommand(cfg backend.CommandConfig) (*exec.Cmd, error) {
	return exec.Command("echo", "test"), nil
}

func (b *testBackend) ParseStreamMessage(line []byte) (*backend.StreamMessage, error) {
	return nil, nil
}

func (b *testBackend) FormatInputMessage(content string, sessionID string) ([]byte, error) {
	return []byte(content), nil
}

func (b *testBackend) HookSettings(fabPath string) map[string]any {
	return nil
}

func TestRegistry(t *testing.T) {
	t.Run("Register and Get", func(t *testing.T) {
		testB := &testBackend{name: "test-backend"}
		backend.Register("test", testB)

		got, err := backend.Get("test")
		if err != nil {
			t.Fatalf("Get() error = %v", err)
		}
		if got != testB {
			t.Errorf("Get() = %v, want %v", got, testB)
		}
	})

	t.Run("Get unknown backend returns error", func(t *testing.T) {
		_, err := backend.Get("nonexistent")
		if err == nil {
			t.Fatal("Get() expected error for unknown backend")
		}
		if err.Error() != "unknown backend: nonexistent" {
			t.Errorf("Get() error = %q, want %q", err.Error(), "unknown backend: nonexistent")
		}
	})

	t.Run("List returns registered backends", func(t *testing.T) {
		backend.Register("alpha", &testBackend{name: "alpha"})
		backend.Register("beta", &testBackend{name: "beta"})

		names := backend.List()
		if !slices.Contains(names, "alpha") {
			t.Errorf("List() missing 'alpha': %v", names)
		}
		if !slices.Contains(names, "beta") {
			t.Errorf("List() missing 'beta': %v", names)
		}
	})

	t.Run("List returns sorted names", func(t *testing.T) {
		names := backend.List()
		if !slices.IsSorted(names) {
			t.Errorf("List() not sorted: %v", names)
		}
	})
}
