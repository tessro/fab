package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIsGitHubShorthand(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Valid shorthand patterns
		{"tessro/fab", true},
		{"owner/repo", true},
		{"a/b", true},
		{"my-org/my-repo", true},
		{"user123/project456", true},

		// Invalid: not a shorthand
		{"", false},
		{"repo", false},
		{"/repo", false},
		{"owner/", false},

		// Invalid: too many slashes (looks like a path)
		{"a/b/c", false},
		{"owner/repo/subdir", false},

		// Invalid: looks like a relative path
		{"./owner/repo", false},
		{"../owner/repo", false},

		// Invalid: looks like a home path
		{"~/owner/repo", false},

		// HTTPS URLs contain :// and multiple slashes so they don't match
		{"https://github.com/owner/repo", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := isGitHubShorthand(tc.input)
			if result != tc.expected {
				t.Errorf("isGitHubShorthand(%q) = %v, want %v", tc.input, result, tc.expected)
			}
		})
	}
}

func TestExpandGitHubShorthand(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"tessro/fab", "https://github.com/tessro/fab.git"},
		{"owner/repo", "https://github.com/owner/repo.git"},
		{"my-org/my-project", "https://github.com/my-org/my-project.git"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := expandGitHubShorthand(tc.input)
			if result != tc.expected {
				t.Errorf("expandGitHubShorthand(%q) = %q, want %q", tc.input, result, tc.expected)
			}
		})
	}
}

func TestIsGitURL(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		// Valid git URLs
		{"https://github.com/owner/repo.git", true},
		{"http://github.com/owner/repo.git", true},
		{"git://github.com/owner/repo.git", true},
		{"ssh://git@github.com/owner/repo.git", true},
		{"git@github.com:owner/repo.git", true},

		// Not git URLs
		{"owner/repo", false},
		{"/path/to/repo", false},
		{"./relative/path", false},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := isGitURL(tc.input)
			if result != tc.expected {
				t.Errorf("isGitURL(%q) = %v, want %v", tc.input, result, tc.expected)
			}
		})
	}
}

func TestResolveLocalDir(t *testing.T) {
	t.Run("existing directory returns true", func(t *testing.T) {
		tmpDir := t.TempDir()
		absPath, ok := resolveLocalDir(tmpDir)
		if !ok {
			t.Errorf("resolveLocalDir(%q) should return true for existing directory", tmpDir)
		}
		if absPath != tmpDir {
			t.Errorf("resolveLocalDir(%q) returned path %q, want %q", tmpDir, absPath, tmpDir)
		}
	})

	t.Run("non-existent path returns false", func(t *testing.T) {
		_, ok := resolveLocalDir("/nonexistent/path/that/does/not/exist")
		if ok {
			t.Error("resolveLocalDir should return false for non-existent path")
		}
	})

	t.Run("file returns false", func(t *testing.T) {
		tmpDir := t.TempDir()
		tmpFile := filepath.Join(tmpDir, "testfile")
		if err := os.WriteFile(tmpFile, []byte("test"), 0644); err != nil {
			t.Fatal(err)
		}
		_, ok := resolveLocalDir(tmpFile)
		if ok {
			t.Error("resolveLocalDir should return false for files")
		}
	})

	t.Run("owner/repo pattern as directory takes precedence", func(t *testing.T) {
		// Create a directory that looks like owner/repo
		tmpDir := t.TempDir()
		ownerDir := filepath.Join(tmpDir, "owner")
		repoDir := filepath.Join(ownerDir, "repo")
		if err := os.MkdirAll(repoDir, 0755); err != nil {
			t.Fatal(err)
		}

		// Change to tmpDir so "owner/repo" is a valid relative path
		oldWd, _ := os.Getwd()
		if err := os.Chdir(tmpDir); err != nil {
			t.Fatal(err)
		}
		defer func() { _ = os.Chdir(oldWd) }()

		absPath, ok := resolveLocalDir("owner/repo")
		if !ok {
			t.Error("resolveLocalDir should return true for existing owner/repo directory")
		}
		if absPath != repoDir {
			t.Errorf("resolveLocalDir returned %q, want %q", absPath, repoDir)
		}
	})
}
