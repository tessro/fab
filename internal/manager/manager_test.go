package manager

import "testing"

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
	m := New("", "testproject", []string{"fab:*", "git:*", "ls"})

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
	m := New("", "testproject", []string{"fab:*"})

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
