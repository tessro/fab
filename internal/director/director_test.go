package director

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/tessro/fab/internal/backend"
	"github.com/tessro/fab/internal/project"
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
	d := New("", &mockBackend{}, []string{"fab:*", "git:*", "ls"}, nil)

	tools := d.buildAllowedTools()

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
	d := New("", &mockBackend{}, []string{"fab:*"}, nil)

	settings := d.buildSettings()

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
	d := New("/test/workdir", b, []string{"fab:*"}, nil)

	// Call buildCommand to trigger backend usage
	_, err := d.buildCommand()
	if err != nil {
		t.Fatalf("buildCommand() failed: %v", err)
	}

	// Verify the backend was called with correct config
	if b.lastConfig.WorkDir != "/test/workdir" {
		t.Errorf("WorkDir = %q, want %q", b.lastConfig.WorkDir, "/test/workdir")
	}

	if b.lastConfig.AgentID != "director" {
		t.Errorf("AgentID = %q, want %q", b.lastConfig.AgentID, "director")
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

	// Check that FAB_DIRECTOR=1 is in Env
	foundEnv := false
	for _, env := range b.lastConfig.Env {
		if env == "FAB_DIRECTOR=1" {
			foundEnv = true
			break
		}
	}
	if !foundEnv {
		t.Error("FAB_DIRECTOR=1 not found in Env")
	}
}

func TestDirectorSystemPrompt(t *testing.T) {
	// Test with nil projects
	prompt := buildDirectorSystemPrompt(nil)

	// Check that key elements are present
	checks := []string{
		"director agent",
		"CTO-level coordinator",
		"fab project list",
		"fab status",
		"fab issue create",
		"--project",
		"Cross-Project",
		"Projects Under Your Direction",
		"No projects registered",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("system prompt missing %q", check)
		}
	}
}

func TestDirectorSystemPromptWithProjects(t *testing.T) {
	// Create mock projects
	projects := []*project.Project{
		project.NewProject("backend", "git@github.com:org/backend.git"),
		project.NewProject("api", "git@github.com:org/api.git"),
	}
	projects[0].SetRunning(true)

	prompt := buildDirectorSystemPrompt(projects)

	// Check project info is included
	checks := []string{
		"### api",
		"### backend",
		"git@github.com:org/backend.git",
		"git@github.com:org/api.git",
		"Status: running",
		"Status: stopped",
		"~/.fab/projects/backend/repo/",
		"~/.fab/projects/api/repo/",
	}

	for _, check := range checks {
		if !strings.Contains(prompt, check) {
			t.Errorf("system prompt with projects missing %q", check)
		}
	}
}

func TestBuildProjectSummary(t *testing.T) {
	t.Run("empty projects", func(t *testing.T) {
		summary := buildProjectSummary(nil)
		if !strings.Contains(summary, "No projects registered") {
			t.Error("empty summary should mention no projects registered")
		}
	})

	t.Run("with projects", func(t *testing.T) {
		projects := []*project.Project{
			project.NewProject("zebra", "https://github.com/org/zebra.git"),
			project.NewProject("alpha", "https://github.com/org/alpha.git"),
		}
		projects[1].SetRunning(true)

		summary := buildProjectSummary(projects)

		// Projects should be sorted alphabetically
		alphaIdx := strings.Index(summary, "### alpha")
		zebraIdx := strings.Index(summary, "### zebra")
		if alphaIdx == -1 || zebraIdx == -1 {
			t.Error("summary should include both project names")
		}
		if alphaIdx > zebraIdx {
			t.Error("projects should be sorted alphabetically (alpha before zebra)")
		}

		// Check project details
		checks := []string{
			"Remote: https://github.com/org/alpha.git",
			"Remote: https://github.com/org/zebra.git",
			"Active agents: 0/3",
			"~/.fab/projects/alpha/repo/",
		}
		for _, check := range checks {
			if !strings.Contains(summary, check) {
				t.Errorf("summary missing %q", check)
			}
		}
	})

	t.Run("running project status", func(t *testing.T) {
		projects := []*project.Project{
			project.NewProject("myproj", "https://github.com/org/myproj.git"),
		}
		projects[0].SetRunning(true)

		summary := buildProjectSummary(projects)
		if !strings.Contains(summary, "Status: running") {
			t.Error("running project should show 'running' status")
		}
	})
}

func TestDirectorCreation(t *testing.T) {
	b := &mockBackend{}
	d := New("/test/workdir", b, []string{"fab:*"}, nil)

	if d == nil {
		t.Fatal("New() returned nil")
	}

	if d.ProcessAgent == nil {
		t.Fatal("ProcessAgent is nil")
	}

	if d.WorkDir() != "/test/workdir" {
		t.Errorf("WorkDir() = %q, want %q", d.WorkDir(), "/test/workdir")
	}

	if d.State() != StateStopped {
		t.Errorf("initial State() = %v, want %v", d.State(), StateStopped)
	}
}

func TestRegistryAccessor(t *testing.T) {
	b := &mockBackend{}
	d := New("/test/workdir", b, nil, nil)

	// With nil registry
	if d.Registry() != nil {
		t.Error("Registry() should be nil when created with nil")
	}
}
