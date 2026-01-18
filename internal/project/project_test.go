package project

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNewProject(t *testing.T) {
	p := NewProject("myapp", "git@github.com:user/myapp.git")

	if p.Name != "myapp" {
		t.Errorf("Name = %q, want %q", p.Name, "myapp")
	}
	if p.RemoteURL != "git@github.com:user/myapp.git" {
		t.Errorf("RemoteURL = %q, want %q", p.RemoteURL, "git@github.com:user/myapp.git")
	}
	if p.MaxAgents != DefaultMaxAgents {
		t.Errorf("MaxAgents = %d, want %d", p.MaxAgents, DefaultMaxAgents)
	}
	if p.Running {
		t.Error("Running = true, want false")
	}
	if len(p.Worktrees) != 0 {
		t.Errorf("Worktrees = %d, want 0", len(p.Worktrees))
	}
}

func TestCreateWorktreeForAgent_Success(t *testing.T) {
	p := NewProject("test", "")
	p.MaxAgents = 3

	wt, err := p.CreateWorktreeForAgent("agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wt == nil {
		t.Fatal("expected worktree, got nil")
	} else {
		if !wt.InUse {
			t.Error("InUse = false, want true")
		}
		if wt.AgentID != "agent1" {
			t.Errorf("AgentID = %q, want %q", wt.AgentID, "agent1")
		}
	}
	// Path should be wt-{agentID}
	if wt.Path != p.WorktreesDir()+"/wt-agent1" {
		t.Errorf("Path = %q, want %q", wt.Path, p.WorktreesDir()+"/wt-agent1")
	}

	// Should be tracked
	if len(p.Worktrees) != 1 {
		t.Errorf("len(Worktrees) = %d, want 1", len(p.Worktrees))
	}
}

func TestCreateWorktreeForAgent_MaxAgentsReached(t *testing.T) {
	p := NewProject("test", "")
	p.MaxAgents = 1
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt-agent1", InUse: true, AgentID: "agent1"},
	}

	_, err := p.CreateWorktreeForAgent("agent2")
	if err != ErrNoWorktreeAvailable {
		t.Errorf("err = %v, want ErrNoWorktreeAvailable", err)
	}
}

func TestDeleteWorktreeForAgent_Success(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt-agent1", InUse: true, AgentID: "agent1"},
		{Path: "/tmp/wt-agent2", InUse: true, AgentID: "agent2"},
	}

	err := p.DeleteWorktreeForAgent("agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should have 1 worktree left
	if len(p.Worktrees) != 1 {
		t.Errorf("len(Worktrees) = %d, want 1", len(p.Worktrees))
	}
	// Remaining worktree should be agent2's
	if p.Worktrees[0].AgentID != "agent2" {
		t.Errorf("remaining AgentID = %q, want agent2", p.Worktrees[0].AgentID)
	}
}

func TestDeleteWorktreeForAgent_NotFound(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt-agent1", InUse: true, AgentID: "agent1"},
	}

	err := p.DeleteWorktreeForAgent("nonexistent")
	if err != ErrWorktreeNotFound {
		t.Errorf("err = %v, want ErrWorktreeNotFound", err)
	}
}

func TestWorktreeLifecycle(t *testing.T) {
	// Test the full create-delete cycle
	p := NewProject("test", "")
	p.MaxAgents = 2

	// Create first worktree
	wt1, err := p.CreateWorktreeForAgent("agent1")
	if err != nil {
		t.Fatalf("create agent1: %v", err)
	}
	if wt1.AgentID != "agent1" {
		t.Errorf("wt1.AgentID = %q, want agent1", wt1.AgentID)
	}

	// Create second worktree
	wt2, err := p.CreateWorktreeForAgent("agent2")
	if err != nil {
		t.Fatalf("create agent2: %v", err)
	}
	if wt2.AgentID != "agent2" {
		t.Errorf("wt2.AgentID = %q, want agent2", wt2.AgentID)
	}

	// Should be at capacity
	_, err = p.CreateWorktreeForAgent("agent3")
	if err != ErrNoWorktreeAvailable {
		t.Errorf("create agent3: err = %v, want ErrNoWorktreeAvailable", err)
	}

	// Delete first worktree
	if err := p.DeleteWorktreeForAgent("agent1"); err != nil {
		t.Fatalf("delete agent1: %v", err)
	}

	// Now we can create again
	wt3, err := p.CreateWorktreeForAgent("agent3")
	if err != nil {
		t.Fatalf("create agent3 after delete: %v", err)
	}
	if wt3.AgentID != "agent3" {
		t.Errorf("wt3.AgentID = %q, want agent3", wt3.AgentID)
	}
}

func TestAvailableWorktreeCount(t *testing.T) {
	p := NewProject("test", "")
	p.MaxAgents = 5
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
		{Path: "/tmp/wt2", InUse: true, AgentID: "agent2"},
		{Path: "/tmp/wt3", InUse: true, AgentID: "agent3"},
	}

	// 3 in use out of 5 max = 2 available
	count := p.AvailableWorktreeCount()
	// Note: AvailableWorktreeCount counts worktrees not in use,
	// but with the new model all worktrees are in use (they only exist when in use)
	// Available capacity is MaxAgents - len(Worktrees)
	// But the function counts InUse=false, which won't exist in new model
	// So we expect 0 available
	if count != 0 {
		t.Errorf("AvailableWorktreeCount() = %d, want 0", count)
	}
}

func TestActiveAgentCount(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
		{Path: "/tmp/wt2", InUse: true, AgentID: "agent2"},
		{Path: "/tmp/wt3", InUse: true, AgentID: "agent3"},
	}

	count := p.ActiveAgentCount()
	if count != 3 {
		t.Errorf("ActiveAgentCount() = %d, want 3", count)
	}
}

func TestSetRunning(t *testing.T) {
	p := NewProject("test", "")

	if p.IsRunning() {
		t.Error("IsRunning() = true, want false initially")
	}

	p.SetRunning(true)
	if !p.IsRunning() {
		t.Error("IsRunning() = false after SetRunning(true)")
	}

	p.SetRunning(false)
	if p.IsRunning() {
		t.Error("IsRunning() = true after SetRunning(false)")
	}
}

func TestAddWorktree(t *testing.T) {
	p := NewProject("test", "")

	p.AddWorktree(Worktree{Path: "/tmp/wt1", InUse: false})
	p.AddWorktree(Worktree{Path: "/tmp/wt2", InUse: true, AgentID: "agent1"})

	if len(p.Worktrees) != 2 {
		t.Errorf("len(Worktrees) = %d, want 2", len(p.Worktrees))
	}
	if p.Worktrees[0].Path != "/tmp/wt1" {
		t.Errorf("Worktrees[0].Path = %q, want /tmp/wt1", p.Worktrees[0].Path)
	}
	if p.Worktrees[1].AgentID != "agent1" {
		t.Errorf("Worktrees[1].AgentID = %q, want agent1", p.Worktrees[1].AgentID)
	}
}

func TestProjectDirs(t *testing.T) {
	p := NewProject("myapp", "")

	// These should return paths without error
	projectDir := p.ProjectDir()
	if projectDir == "" {
		t.Error("ProjectDir() returned empty string")
	}

	repoDir := p.RepoDir()
	if repoDir == "" {
		t.Error("RepoDir() returned empty string")
	}

	wtDir := p.WorktreesDir()
	if wtDir == "" {
		t.Error("WorktreesDir() returned empty string")
	}

	// Verify relationships
	if repoDir != projectDir+"/repo" {
		t.Errorf("RepoDir() = %q, want %q/repo", repoDir, projectDir)
	}
	if wtDir != projectDir+"/worktrees" {
		t.Errorf("WorktreesDir() = %q, want %q/worktrees", wtDir, projectDir)
	}
}

func TestProjectDirs_WithBaseDir(t *testing.T) {
	p := NewProject("myapp", "")
	p.BaseDir = "/custom/base"

	projectDir := p.ProjectDir()
	if projectDir != "/custom/base/myapp" {
		t.Errorf("ProjectDir() = %q, want %q", projectDir, "/custom/base/myapp")
	}

	repoDir := p.RepoDir()
	if repoDir != "/custom/base/myapp/repo" {
		t.Errorf("RepoDir() = %q, want %q", repoDir, "/custom/base/myapp/repo")
	}

	wtDir := p.WorktreesDir()
	if wtDir != "/custom/base/myapp/worktrees" {
		t.Errorf("WorktreesDir() = %q, want %q", wtDir, "/custom/base/myapp/worktrees")
	}
}

func TestWorktreePathForAgent(t *testing.T) {
	p := NewProject("myapp", "")

	path := p.worktreePathForAgent("abc123")
	expected := p.WorktreesDir() + "/wt-abc123"
	if path != expected {
		t.Errorf("worktreePathForAgent = %q, want %q", path, expected)
	}
}

func TestGetAgentBackend(t *testing.T) {
	tests := []struct {
		name         string
		agentBackend string
		want         string
	}{
		{
			name:         "empty returns default",
			agentBackend: "",
			want:         DefaultAgentBackend,
		},
		{
			name:         "claude explicit",
			agentBackend: "claude",
			want:         "claude",
		},
		{
			name:         "codex explicit",
			agentBackend: "codex",
			want:         "codex",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProject("test", "")
			p.AgentBackend = tt.agentBackend
			if got := p.GetAgentBackend(); got != tt.want {
				t.Errorf("GetAgentBackend() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetPlannerBackend(t *testing.T) {
	tests := []struct {
		name           string
		plannerBackend string
		agentBackend   string
		want           string
	}{
		{
			name:           "explicit planner backend",
			plannerBackend: "codex",
			agentBackend:   "claude",
			want:           "codex",
		},
		{
			name:           "falls back to agent backend",
			plannerBackend: "",
			agentBackend:   "codex",
			want:           "codex",
		},
		{
			name:           "falls back to default when both empty",
			plannerBackend: "",
			agentBackend:   "",
			want:           DefaultAgentBackend,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProject("test", "")
			p.PlannerBackend = tt.plannerBackend
			p.AgentBackend = tt.agentBackend
			if got := p.GetPlannerBackend(); got != tt.want {
				t.Errorf("GetPlannerBackend() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetCodingBackend(t *testing.T) {
	tests := []struct {
		name          string
		codingBackend string
		agentBackend  string
		want          string
	}{
		{
			name:          "explicit coding backend",
			codingBackend: "codex",
			agentBackend:  "claude",
			want:          "codex",
		},
		{
			name:          "falls back to agent backend",
			codingBackend: "",
			agentBackend:  "codex",
			want:          "codex",
		},
		{
			name:          "falls back to default when both empty",
			codingBackend: "",
			agentBackend:  "",
			want:          DefaultAgentBackend,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := NewProject("test", "")
			p.CodingBackend = tt.codingBackend
			p.AgentBackend = tt.agentBackend
			if got := p.GetCodingBackend(); got != tt.want {
				t.Errorf("GetCodingBackend() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestProjectDir_FAB_DIR(t *testing.T) {
	// Save and restore FAB_DIR
	origFabDir := os.Getenv("FAB_DIR")
	defer os.Setenv("FAB_DIR", origFabDir)

	t.Run("uses FAB_DIR when set and BaseDir empty", func(t *testing.T) {
		os.Setenv("FAB_DIR", "/tmp/fab-e2e")
		p := NewProject("myapp", "")
		// BaseDir is empty, so ProjectDir should use paths.ProjectsDir() which uses FAB_DIR
		want := "/tmp/fab-e2e/projects/myapp"
		got := p.ProjectDir()
		if got != want {
			t.Errorf("ProjectDir() = %q, want %q", got, want)
		}
	})

	t.Run("RepoDir follows ProjectDir under FAB_DIR", func(t *testing.T) {
		os.Setenv("FAB_DIR", "/tmp/fab-e2e")
		p := NewProject("myapp", "")
		want := "/tmp/fab-e2e/projects/myapp/repo"
		got := p.RepoDir()
		if got != want {
			t.Errorf("RepoDir() = %q, want %q", got, want)
		}
	})

	t.Run("WorktreesDir follows ProjectDir under FAB_DIR", func(t *testing.T) {
		os.Setenv("FAB_DIR", "/tmp/fab-e2e")
		p := NewProject("myapp", "")
		want := "/tmp/fab-e2e/projects/myapp/worktrees"
		got := p.WorktreesDir()
		if got != want {
			t.Errorf("WorktreesDir() = %q, want %q", got, want)
		}
	})

	t.Run("BaseDir takes precedence over FAB_DIR", func(t *testing.T) {
		os.Setenv("FAB_DIR", "/tmp/fab-e2e")
		p := NewProject("myapp", "")
		p.BaseDir = "/custom/projects" // Explicit BaseDir should override FAB_DIR
		want := "/custom/projects/myapp"
		got := p.ProjectDir()
		if got != want {
			t.Errorf("ProjectDir() = %q, want %q", got, want)
		}
	})

	t.Run("defaults to ~/.fab/projects when FAB_DIR not set", func(t *testing.T) {
		os.Unsetenv("FAB_DIR")
		p := NewProject("myapp", "")
		home, _ := os.UserHomeDir()
		want := filepath.Join(home, ".fab", "projects", "myapp")
		got := p.ProjectDir()
		if got != want {
			t.Errorf("ProjectDir() = %q, want %q", got, want)
		}
	})
}
