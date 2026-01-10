package registry

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/tessro/fab/internal/project"
)

func TestNew(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if r.Count() != 0 {
		t.Errorf("Count() = %d, want 0", r.Count())
	}

	if r.ConfigPath() != configPath {
		t.Errorf("ConfigPath() = %q, want %q", r.ConfigPath(), configPath)
	}
}

func TestRegistry_Add(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	remoteURL := "git@github.com:user/myproject.git"

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	// Add project
	p, err := r.Add(remoteURL, "myproject", 0, false)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if p.Name != "myproject" {
		t.Errorf("Name = %q, want %q", p.Name, "myproject")
	}

	if p.RemoteURL != remoteURL {
		t.Errorf("RemoteURL = %q, want %q", p.RemoteURL, remoteURL)
	}

	if p.MaxAgents != project.DefaultMaxAgents {
		t.Errorf("MaxAgents = %d, want %d", p.MaxAgents, project.DefaultMaxAgents)
	}

	if r.Count() != 1 {
		t.Errorf("Count() = %d, want 1", r.Count())
	}

	// Verify config file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("config file was not created")
	}
}

func TestRegistry_AddDuplicate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	remoteURL := "git@github.com:user/myproject.git"

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if _, err := r.Add(remoteURL, "myproject", 0, false); err != nil {
		t.Fatalf("first Add() error = %v", err)
	}

	if _, err := r.Add(remoteURL, "myproject", 0, false); err != ErrProjectExists {
		t.Errorf("second Add() error = %v, want ErrProjectExists", err)
	}
}

func TestRegistry_AddEmptyURL(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if _, err := r.Add("", "test", 0, false); err != ErrInvalidRemoteURL {
		t.Errorf("Add() error = %v, want ErrInvalidRemoteURL", err)
	}
}

func TestRegistry_AddDefaultName(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	remoteURL := "git@github.com:user/awesome-project.git"

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	p, err := r.Add(remoteURL, "", 0, false)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if p.Name != "awesome-project" {
		t.Errorf("Name = %q, want %q", p.Name, "awesome-project")
	}
}

func TestRegistry_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	remoteURL := "git@github.com:user/myproject.git"

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if _, err := r.Add(remoteURL, "myproject", 0, false); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	if err := r.Remove("myproject"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}

	if r.Count() != 0 {
		t.Errorf("Count() = %d, want 0", r.Count())
	}
}

func TestRegistry_RemoveNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if err := r.Remove("nonexistent"); err != ErrProjectNotFound {
		t.Errorf("Remove() error = %v, want ErrProjectNotFound", err)
	}
}

func TestRegistry_Get(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	remoteURL := "git@github.com:user/myproject.git"

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if _, err := r.Add(remoteURL, "myproject", 5, false); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	p, err := r.Get("myproject")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if p.Name != "myproject" {
		t.Errorf("Name = %q, want %q", p.Name, "myproject")
	}

	if p.MaxAgents != 5 {
		t.Errorf("MaxAgents = %d, want 5", p.MaxAgents)
	}
}

func TestRegistry_GetNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if _, err := r.Get("nonexistent"); err != ErrProjectNotFound {
		t.Errorf("Get() error = %v, want ErrProjectNotFound", err)
	}
}

func TestRegistry_List(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if _, err := r.Add("git@github.com:user/project1.git", "project1", 0, false); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	if _, err := r.Add("git@github.com:user/project2.git", "project2", 0, false); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	projects := r.List()
	if len(projects) != 2 {
		t.Errorf("len(List()) = %d, want 2", len(projects))
	}
}

func TestRegistry_Update(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	remoteURL := "git@github.com:user/myproject.git"

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if _, err := r.Add(remoteURL, "myproject", 3, false); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	newMax := 5
	if err := r.Update("myproject", &newMax, nil); err != nil {
		t.Fatalf("Update() error = %v", err)
	}

	p, _ := r.Get("myproject")
	if p.MaxAgents != 5 {
		t.Errorf("MaxAgents = %d, want 5", p.MaxAgents)
	}
}

func TestRegistry_Persistence(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")
	remoteURL := "git@github.com:user/myproject.git"

	// Create registry and add project
	r1, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if _, err := r1.Add(remoteURL, "myproject", 7, false); err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Create new registry instance and verify data persisted
	r2, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if r2.Count() != 1 {
		t.Errorf("Count() = %d, want 1", r2.Count())
	}

	p, err := r2.Get("myproject")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if p.Name != "myproject" {
		t.Errorf("Name = %q, want %q", p.Name, "myproject")
	}

	if p.RemoteURL != remoteURL {
		t.Errorf("RemoteURL = %q, want %q", p.RemoteURL, remoteURL)
	}

	if p.MaxAgents != 7 {
		t.Errorf("MaxAgents = %d, want 7", p.MaxAgents)
	}
}

func TestRegistry_ManagerAllowedPatterns_Default(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	// With no config, should return default patterns
	patterns := r.ManagerAllowedPatterns()
	if len(patterns) != 1 || patterns[0] != "fab:*" {
		t.Errorf("ManagerAllowedPatterns() = %v, want [fab:*]", patterns)
	}
}

func TestRegistry_ManagerAllowedPatterns_Custom(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write config with custom manager patterns
	configContent := `
[manager]
allowed_patterns = ["fab:*", "git:*", "ls:*"]
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	patterns := r.ManagerAllowedPatterns()
	expected := []string{"fab:*", "git:*", "ls:*"}
	if len(patterns) != len(expected) {
		t.Fatalf("ManagerAllowedPatterns() len = %d, want %d", len(patterns), len(expected))
	}
	for i, p := range patterns {
		if p != expected[i] {
			t.Errorf("ManagerAllowedPatterns()[%d] = %q, want %q", i, p, expected[i])
		}
	}
}

func TestRegistry_ManagerAllowedPatterns_InvalidPattern(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write config with empty pattern (invalid)
	configContent := `
[manager]
allowed_patterns = ["fab:*", ""]
`
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		t.Fatalf("WriteFile error = %v", err)
	}

	_, err := NewWithPath(configPath)
	if err == nil {
		t.Error("NewWithPath() expected error for empty pattern, got nil")
	}
}
