package registry

import (
	"os"
	"path/filepath"
	"strings"
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

func TestRegistry_LegacyConfigFormat(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config file with legacy underscore-format keys
	legacyConfig := `[[projects]]
name = "legacy-project"
remote_url = "git@github.com:user/legacy.git"
max_agents = 5
issue_backend = "tk"
allowed_authors = ["user1", "user2"]
permissions_checker = "llm"
`
	if err := os.WriteFile(configPath, []byte(legacyConfig), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Load the registry - should parse legacy format
	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	if r.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", r.Count())
	}

	p, err := r.Get("legacy-project")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Verify all legacy fields were parsed correctly
	if p.RemoteURL != "git@github.com:user/legacy.git" {
		t.Errorf("RemoteURL = %q, want %q", p.RemoteURL, "git@github.com:user/legacy.git")
	}
	if p.MaxAgents != 5 {
		t.Errorf("MaxAgents = %d, want 5", p.MaxAgents)
	}
	if p.IssueBackend != "tk" {
		t.Errorf("IssueBackend = %q, want %q", p.IssueBackend, "tk")
	}
	if len(p.AllowedAuthors) != 2 || p.AllowedAuthors[0] != "user1" {
		t.Errorf("AllowedAuthors = %v, want [user1 user2]", p.AllowedAuthors)
	}
	if p.PermissionsChecker != "llm" {
		t.Errorf("PermissionsChecker = %q, want %q", p.PermissionsChecker, "llm")
	}
}

func TestRegistry_HyphenConfigFormat(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config file with hyphen-format keys (new format)
	newConfig := `[[projects]]
name = "new-project"
remote-url = "git@github.com:user/new.git"
max-agents = 3
issue-backend = "github"
allowed-authors = ["author1"]
permissions-checker = "llm"
`
	if err := os.WriteFile(configPath, []byte(newConfig), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Load the registry
	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	p, err := r.Get("new-project")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Verify all hyphen-format fields were parsed correctly
	if p.RemoteURL != "git@github.com:user/new.git" {
		t.Errorf("RemoteURL = %q, want %q", p.RemoteURL, "git@github.com:user/new.git")
	}
	if p.MaxAgents != 3 {
		t.Errorf("MaxAgents = %d, want 3", p.MaxAgents)
	}
	if p.IssueBackend != "github" {
		t.Errorf("IssueBackend = %q, want %q", p.IssueBackend, "github")
	}
	if len(p.AllowedAuthors) != 1 || p.AllowedAuthors[0] != "author1" {
		t.Errorf("AllowedAuthors = %v, want [author1]", p.AllowedAuthors)
	}
	if p.PermissionsChecker != "llm" {
		t.Errorf("PermissionsChecker = %q, want %q", p.PermissionsChecker, "llm")
	}
}

func TestRegistry_HyphenTakesPrecedence(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config file with both hyphen and underscore format (hyphen should win)
	mixedConfig := `[[projects]]
name = "mixed-project"
remote-url = "git@github.com:user/hyphen.git"
remote_url = "git@github.com:user/underscore.git"
max-agents = 10
max_agents = 5
permissions-checker = "llm"
permissions_checker = "manual"
`
	if err := os.WriteFile(configPath, []byte(mixedConfig), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Load the registry
	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	p, err := r.Get("mixed-project")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	// Hyphen format should take precedence
	if p.RemoteURL != "git@github.com:user/hyphen.git" {
		t.Errorf("RemoteURL = %q, want hyphen version", p.RemoteURL)
	}
	if p.MaxAgents != 10 {
		t.Errorf("MaxAgents = %d, want 10 (hyphen version)", p.MaxAgents)
	}
	if p.PermissionsChecker != "llm" {
		t.Errorf("PermissionsChecker = %q, want llm (hyphen version)", p.PermissionsChecker)
	}
}

func TestRegistry_SavePreservesGlobalConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config file with global config and projects
	initialConfig := `log_level = "debug"

[providers]
[providers.anthropic]
api-key = "sk-secret-key"

[llm_auth]
provider = "anthropic"
model = "claude-haiku-4-5"

[[projects]]
name = "existing-project"
remote-url = "git@github.com:user/existing.git"
max-agents = 2
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Load the registry
	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	// Verify existing project was loaded
	if r.Count() != 1 {
		t.Fatalf("Count() = %d, want 1", r.Count())
	}

	// Add a new project (this triggers save)
	_, err = r.Add("git@github.com:user/new-project.git", "new-project", 3, false)
	if err != nil {
		t.Fatalf("Add() error = %v", err)
	}

	// Read back the config file
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	configStr := string(content)

	// Verify global config was preserved
	if !strings.Contains(configStr, `log_level = "debug"`) {
		t.Errorf("Config should contain log_level = debug, got:\n%s", configStr)
	}
	if !strings.Contains(configStr, `api-key = "sk-secret-key"`) {
		t.Errorf("Config should contain api-key = sk-secret-key, got:\n%s", configStr)
	}
	if !strings.Contains(configStr, `provider = "anthropic"`) {
		t.Errorf("Config should contain provider = anthropic, got:\n%s", configStr)
	}
	if !strings.Contains(configStr, `model = "claude-haiku-4-5"`) {
		t.Errorf("Config should contain model = claude-haiku, got:\n%s", configStr)
	}

	// Verify both projects are present
	if !strings.Contains(configStr, `name = "existing-project"`) {
		t.Errorf("Config should contain existing-project, got:\n%s", configStr)
	}
	if !strings.Contains(configStr, `name = "new-project"`) {
		t.Errorf("Config should contain new-project, got:\n%s", configStr)
	}
}

func TestRegistry_SavePreservesGlobalConfigOnUpdate(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.toml")

	// Write a config file with global config and a project
	initialConfig := `log_level = "warn"

[providers]
[providers.openai]
api-key = "openai-key"

[[projects]]
name = "test-project"
remote-url = "git@github.com:user/test.git"
max-agents = 1
`
	if err := os.WriteFile(configPath, []byte(initialConfig), 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	// Load the registry
	r, err := NewWithPath(configPath)
	if err != nil {
		t.Fatalf("NewWithPath() error = %v", err)
	}

	// Update the project config (this triggers save)
	err = r.SetConfigValue("test-project", ConfigKeyMaxAgents, "5")
	if err != nil {
		t.Fatalf("SetConfigValue() error = %v", err)
	}

	// Read back the config file
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	configStr := string(content)

	// Verify global config was preserved
	if !strings.Contains(configStr, `log_level = "warn"`) {
		t.Errorf("Config should contain log_level = warn, got:\n%s", configStr)
	}
	if !strings.Contains(configStr, `api-key = "openai-key"`) {
		t.Errorf("Config should contain api-key = openai-key, got:\n%s", configStr)
	}

	// Verify project update was saved
	if !strings.Contains(configStr, `max-agents = 5`) {
		t.Errorf("Config should contain max-agents = 5, got:\n%s", configStr)
	}
}

