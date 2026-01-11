package manager

import (
	"os/exec"
	"testing"

	"github.com/tessro/fab/internal/backend"
)

// mockBackend implements backend.Backend for testing.
type mockBackend struct {
	lastConfig backend.CommandConfig
}

func (m *mockBackend) Name() string { return "mock" }

func (m *mockBackend) BuildCommand(cfg backend.CommandConfig) (*exec.Cmd, error) {
	m.lastConfig = cfg
	return exec.Command("echo", "mock"), nil
}

func (m *mockBackend) ParseStreamMessage(line []byte) (*backend.StreamMessage, error) {
	return nil, nil
}

func (m *mockBackend) FormatInputMessage(content string, sessionID string) ([]byte, error) {
	return []byte(content), nil
}

func (m *mockBackend) HookSettings(fabPath string) map[string]any {
	return nil
}

var _ backend.Backend = (*mockBackend)(nil)

func TestConvertPatternToClaudeCode(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		want    string
	}{
		{
			name:    "prefix pattern",
			pattern: "fab:*",
			want:    "Bash(fab *)",
		},
		{
			name:    "git prefix",
			pattern: "git:*",
			want:    "Bash(git *)",
		},
		{
			name:    "exact match",
			pattern: "ls",
			want:    "Bash(ls)",
		},
		{
			name:    "catch-all",
			pattern: ":*",
			want:    "Bash(*)",
		},
		{
			name:    "empty",
			pattern: "",
			want:    "",
		},
		{
			name:    "npm run prefix",
			pattern: "npm run:*",
			want:    "Bash(npm run *)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertPatternToClaudeCode(tt.pattern)
			if got != tt.want {
				t.Errorf("convertPatternToClaudeCode(%q) = %q, want %q", tt.pattern, got, tt.want)
			}
		})
	}
}

func TestBuildAllowedTools(t *testing.T) {
	m := New("", "testproject", &mockBackend{}, []string{"fab:*", "git:*", "ls"})

	tools := m.buildAllowedTools()

	expected := []string{"Bash(fab *)", "Bash(git *)", "Bash(ls)"}
	if len(tools) != len(expected) {
		t.Fatalf("buildAllowedTools() len = %d, want %d", len(tools), len(expected))
	}
	for i, tool := range tools {
		if tool != expected[i] {
			t.Errorf("buildAllowedTools()[%d] = %q, want %q", i, tool, expected[i])
		}
	}
}

func TestBuildSettings(t *testing.T) {
	m := New("", "testproject", &mockBackend{}, []string{"fab:*"})

	settings := m.buildSettings()

	// Check structure
	perms, ok := settings["permissions"].(map[string]any)
	if !ok {
		t.Fatal("settings[permissions] not found or wrong type")
	}

	allow, ok := perms["allow"].([]string)
	if !ok {
		t.Fatal("permissions[allow] not found or wrong type")
	}

	if len(allow) != 1 || allow[0] != "Bash(fab *)" {
		t.Errorf("permissions.allow = %v, want [Bash(fab *)]", allow)
	}
}

func TestBackendInjection(t *testing.T) {
	b := &mockBackend{}
	m := New("/test/workdir", "testproject", b, []string{"fab:*"})

	// Call buildCommand to trigger backend usage
	_, err := m.buildCommand()
	if err != nil {
		t.Fatalf("buildCommand() failed: %v", err)
	}

	// Verify the backend was called with correct config
	if b.lastConfig.WorkDir != "/test/workdir" {
		t.Errorf("WorkDir = %q, want %q", b.lastConfig.WorkDir, "/test/workdir")
	}

	if b.lastConfig.AgentID != "manager:testproject" {
		t.Errorf("AgentID = %q, want %q", b.lastConfig.AgentID, "manager:testproject")
	}

	// Check that settings include permissions
	if b.lastConfig.Settings == nil {
		t.Fatal("Settings is nil")
	}
	perms, ok := b.lastConfig.Settings["permissions"].(map[string]any)
	if !ok {
		t.Fatal("Settings[permissions] not found or wrong type")
	}
	_, ok = perms["allow"]
	if !ok {
		t.Fatal("Settings[permissions][allow] not found")
	}

	// Check that FAB_MANAGER=1 is in Env
	foundEnv := false
	for _, env := range b.lastConfig.Env {
		if env == "FAB_MANAGER=1" {
			foundEnv = true
			break
		}
	}
	if !foundEnv {
		t.Error("FAB_MANAGER=1 not found in Env")
	}
}
