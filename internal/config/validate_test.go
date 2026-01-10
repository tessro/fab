package config

import (
	"errors"
	"testing"
)

func TestValidateProjectName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"valid simple", "myproject", nil},
		{"valid with dash", "my-project", nil},
		{"valid with underscore", "my_project", nil},
		{"valid with dot", "my.project", nil},
		{"valid with numbers", "project123", nil},
		{"valid mixed", "My-Project_1.0", nil},
		{"empty", "", ErrEmptyProjectName},
		{"starts with dash", "-project", ErrInvalidProjectName},
		{"starts with dot", ".project", ErrInvalidProjectName},
		{"starts with underscore", "_project", ErrInvalidProjectName},
		{"contains slash", "my/project", ErrInvalidProjectName},
		{"contains backslash", "my\\project", ErrInvalidProjectName},
		{"contains space", "my project", ErrInvalidProjectName},
		{"too long", string(make([]byte, MaxProjectNameLength+1)), ErrProjectNameTooLong},
		{"max length", string(repeatByte('a', MaxProjectNameLength)), nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProjectName(tt.input)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateProjectName(%q) = %v, want nil", tt.input, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateProjectName(%q) = nil, want error", tt.input)
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateProjectName(%q) = %v, want %v", tt.input, err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateRemoteURL(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		// Valid HTTPS URLs
		{"https github", "https://github.com/user/repo.git", nil},
		{"https github no .git", "https://github.com/user/repo", nil},
		{"https gitlab", "https://gitlab.com/user/repo.git", nil},
		{"http url", "http://github.com/user/repo.git", nil},
		{"https with org", "https://github.com/org/sub/repo.git", nil},

		// Valid SSH URLs
		{"ssh github", "git@github.com:user/repo.git", nil},
		{"ssh github no .git", "git@github.com:user/repo", nil},
		{"ssh gitlab", "git@gitlab.com:user/repo.git", nil},
		{"ssh custom domain", "git@git.example.com:org/repo.git", nil},

		// Valid file:// URLs (for local testing)
		{"file url", "file:///tmp/repo", nil},
		{"file url with path", "file:///home/user/projects/repo", nil},

		// Invalid URLs
		{"empty", "", ErrEmptyRemoteURL},
		{"just text", "myproject", ErrInvalidRemoteURL},
		{"local path", "/home/user/repo", ErrInvalidRemoteURL},
		{"relative path", "./repo", ErrInvalidRemoteURL},
		{"incomplete https", "https://github.com", ErrInvalidRemoteURL},
		{"incomplete ssh", "git@github.com", ErrInvalidRemoteURL},
		{"ssh without repo", "git@github.com:user", ErrInvalidRemoteURL},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRemoteURL(tt.input)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateRemoteURL(%q) = %v, want nil", tt.input, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateRemoteURL(%q) = nil, want error", tt.input)
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateRemoteURL(%q) = %v, want %v", tt.input, err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateMaxAgents(t *testing.T) {
	tests := []struct {
		name    string
		input   int
		wantErr error
	}{
		{"minimum valid", 1, nil},
		{"typical value", 3, nil},
		{"maximum valid", MaxMaxAgents, nil},
		{"zero", 0, ErrInvalidMaxAgents},
		{"negative", -1, ErrInvalidMaxAgents},
		{"too high", MaxMaxAgents + 1, ErrInvalidMaxAgents},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateMaxAgents(tt.input)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateMaxAgents(%d) = %v, want nil", tt.input, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateMaxAgents(%d) = nil, want error", tt.input)
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateMaxAgents(%d) = %v, want %v", tt.input, err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateProjectEntry(t *testing.T) {
	tests := []struct {
		name      string
		projName  string
		remoteURL string
		maxAgents int
		wantErr   error
	}{
		{"valid", "myproject", "git@github.com:user/repo.git", 3, nil},
		{"invalid name", "", "git@github.com:user/repo.git", 3, ErrEmptyProjectName},
		{"invalid url", "myproject", "", 3, ErrEmptyRemoteURL},
		{"invalid max_agents", "myproject", "git@github.com:user/repo.git", 0, ErrInvalidMaxAgents},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateProjectEntry(tt.projName, tt.remoteURL, tt.maxAgents)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateProjectEntry() = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateProjectEntry() = nil, want error")
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateProjectEntry() = %v, want %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateToolName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"Bash", "Bash", nil},
		{"Read", "Read", nil},
		{"Write", "Write", nil},
		{"Edit", "Edit", nil},
		{"Glob", "Glob", nil},
		{"Grep", "Grep", nil},
		{"WebFetch", "WebFetch", nil},
		{"Task", "Task", nil},
		{"Skill", "Skill", nil},
		{"WebSearch", "WebSearch", nil},
		{"NotebookEdit", "NotebookEdit", nil},
		{"empty", "", ErrEmptyToolName},
		{"lowercase bash", "bash", ErrInvalidToolName},
		{"unknown tool", "UnknownTool", ErrInvalidToolName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateToolName(tt.input)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateToolName(%q) = %v, want nil", tt.input, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateToolName(%q) = nil, want error", tt.input)
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateToolName(%q) = %v, want %v", tt.input, err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateAction(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"allow", "allow", nil},
		{"deny", "deny", nil},
		{"pass", "pass", nil},
		{"empty", "", ErrEmptyAction},
		{"uppercase", "ALLOW", ErrInvalidAction},
		{"invalid", "block", ErrInvalidAction},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateAction(tt.input)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateAction(%q) = %v, want nil", tt.input, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateAction(%q) = nil, want error", tt.input)
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateAction(%q) = %v, want %v", tt.input, err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidatePattern(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr error
	}{
		{"empty allowed", "", nil},
		{"wildcard", ":*", nil},
		{"prefix match", "git :*", nil},
		{"exact match", "git status", nil},
		{"path pattern", "/etc/:*", nil},
		{"whitespace only", "   ", ErrEmptyPattern},
		{"tab only", "\t", ErrEmptyPattern},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePattern(tt.input)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidatePattern(%q) = %v, want nil", tt.input, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidatePattern(%q) = nil, want error", tt.input)
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidatePattern(%q) = %v, want %v", tt.input, err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidatePatterns(t *testing.T) {
	tests := []struct {
		name    string
		input   []string
		wantErr error
	}{
		{"empty slice", []string{}, nil},
		{"single pattern", []string{"git :*"}, nil},
		{"multiple patterns", []string{"git :*", "cargo :*", "go :*"}, nil},
		{"contains empty", []string{"git :*", "", "go :*"}, ErrEmptyPatternElement},
		{"contains whitespace", []string{"git :*", "  ", "go :*"}, ErrEmptyPatternElement},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePatterns(tt.input)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidatePatterns(%v) = %v, want nil", tt.input, err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidatePatterns(%v) = nil, want error", tt.input)
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidatePatterns(%v) = %v, want %v", tt.input, err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidateRule(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		action   string
		pattern  string
		patterns []string
		script   string
		wantErr  error
	}{
		{"valid with pattern", "Bash", "allow", "git :*", nil, "", nil},
		{"valid with patterns", "Bash", "allow", "", []string{"git :*", "cargo :*"}, "", nil},
		{"valid with script", "Bash", "allow", "", nil, "/path/to/script.sh", nil},
		{"valid no matcher", "Read", "allow", "", nil, "", nil},
		{"invalid tool", "Unknown", "allow", "", nil, "", ErrInvalidToolName},
		{"invalid action", "Bash", "block", "", nil, "", ErrInvalidAction},
		{"invalid pattern", "Bash", "allow", "   ", nil, "", ErrEmptyPattern},
		{"invalid patterns", "Bash", "allow", "", []string{""}, "", ErrEmptyPatternElement},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateRule(tt.tool, tt.action, tt.pattern, tt.patterns, tt.script)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidateRule() = %v, want nil", err)
				}
			} else {
				if err == nil {
					t.Errorf("ValidateRule() = nil, want error")
				} else if !errors.Is(err, tt.wantErr) {
					t.Errorf("ValidateRule() = %v, want %v", err, tt.wantErr)
				}
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	t.Run("error with value", func(t *testing.T) {
		err := &ValidationError{
			Field:   "name",
			Value:   "bad-value",
			Message: "is invalid",
			Err:     ErrInvalidProjectName,
		}
		want := `name: is invalid (got "bad-value")`
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
		if !errors.Is(err, ErrInvalidProjectName) {
			t.Error("Unwrap() should return underlying error")
		}
	})

	t.Run("error without value", func(t *testing.T) {
		err := &ValidationError{
			Field:   "name",
			Message: "cannot be empty",
			Err:     ErrEmptyProjectName,
		}
		want := "name: cannot be empty"
		if got := err.Error(); got != want {
			t.Errorf("Error() = %q, want %q", got, want)
		}
	})
}

func TestIsEmptyOrWhitespace(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", true},
		{"single space", " ", true},
		{"multiple spaces", "   ", true},
		{"tab", "\t", true},
		{"newline", "\n", true},
		{"mixed whitespace", " \t\n ", true},
		{"non-empty", "hello", false},
		{"leading space", " hello", false},
		{"trailing space", "hello ", false},
		{"surrounded by spaces", " hello ", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEmptyOrWhitespace(tt.input); got != tt.want {
				t.Errorf("isEmptyOrWhitespace(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

// repeatByte creates a string of repeated bytes.
func repeatByte(b byte, count int) []byte {
	result := make([]byte, count)
	for i := range result {
		result[i] = b
	}
	return result
}
