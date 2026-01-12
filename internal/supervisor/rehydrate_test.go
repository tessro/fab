package supervisor

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/agenthost"
	"github.com/tessro/fab/internal/registry"
)

func TestParseAgentState(t *testing.T) {
	tests := []struct {
		input    string
		expected agent.State
	}{
		{"starting", agent.StateStarting},
		{"running", agent.StateRunning},
		{"idle", agent.StateIdle},
		{"done", agent.StateDone},
		{"error", agent.StateError},
		{"unknown", agent.StateRunning}, // Default
		{"", agent.StateRunning},        // Default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := parseAgentState(tt.input)
			if result != tt.expected {
				t.Errorf("parseAgentState(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestRehydrateFromHosts_NoHosts(t *testing.T) {
	// Create temp directory for config and project storage
	tmpDir, err := os.MkdirTemp("", "fab-rehydrate-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Set FAB_DIR to isolate from system state
	os.Setenv("FAB_DIR", tmpDir)
	defer os.Unsetenv("FAB_DIR")

	configPath := filepath.Join(tmpDir, "config.toml")
	reg, err := registry.NewWithPath(configPath)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	agents := agent.NewManager()
	sup := New(reg, agents)

	// Should return 0 when no hosts exist
	count := sup.RehydrateFromHosts()
	if count != 0 {
		t.Errorf("RehydrateFromHosts() = %d, want 0", count)
	}
}

func TestRehydrateFromHost_ProjectNotRegistered(t *testing.T) {
	// Create temp directory for config and project storage
	tmpDir, err := os.MkdirTemp("", "fab-rehydrate-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	configPath := filepath.Join(tmpDir, "config.toml")
	reg, err := registry.NewWithPath(configPath)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	agents := agent.NewManager()
	sup := New(reg, agents)

	// Try to rehydrate an agent for a project that doesn't exist
	host := agenthost.DiscoveredHost{
		AgentID:    "test-agent",
		SocketPath: "/tmp/test.sock",
		Status: &agenthost.StatusResponse{
			Agent: agenthost.AgentInfo{
				ID:        "test-agent",
				Project:   "nonexistent-project",
				State:     "running",
				Worktree:  "/tmp/worktree",
				StartedAt: time.Now(),
				Backend:   "claude",
			},
		},
	}

	// Should silently skip when project not registered
	err = sup.rehydrateFromHost(host)
	if err != nil {
		t.Errorf("rehydrateFromHost() error = %v, want nil", err)
	}

	// Verify no agents were added
	if agents.Count() != 0 {
		t.Errorf("expected 0 agents, got %d", agents.Count())
	}
}

func TestRehydrateFromHost_Success(t *testing.T) {
	// Create temp directory for config and project storage
	tmpDir, err := os.MkdirTemp("", "fab-rehydrate-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a git repo to act as "remote"
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	cmd = exec.Command("git", "commit", "--allow-empty", "-m", "Initial commit")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.toml")
	reg, err := registry.NewWithPath(configPath)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	// Set project base dir to temp directory
	reg.SetProjectBaseDir(tmpDir)

	// Add a project
	proj, err := reg.Add("file://"+repoDir, "test-project", 3, false, "claude")
	if err != nil {
		t.Fatalf("failed to add project: %v", err)
	}
	proj.BaseDir = tmpDir

	agents := agent.NewManager()
	sup := New(reg, agents)

	// Create worktree directory
	worktreeDir := filepath.Join(tmpDir, "worktree-abc123")
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	// Try to rehydrate an agent
	startedAt := time.Now().Add(-1 * time.Hour)
	host := agenthost.DiscoveredHost{
		AgentID:    "abc123",
		SocketPath: "/tmp/abc123.sock",
		Status: &agenthost.StatusResponse{
			Agent: agenthost.AgentInfo{
				ID:          "abc123",
				Project:     "test-project",
				State:       "idle",
				Worktree:    worktreeDir,
				StartedAt:   startedAt,
				Task:        "TASK-123",
				Description: "Working on feature",
				Backend:     "claude",
			},
		},
	}

	err = sup.rehydrateFromHost(host)
	if err != nil {
		t.Fatalf("rehydrateFromHost() error = %v", err)
	}

	// Verify agent was added
	if agents.Count() != 1 {
		t.Fatalf("expected 1 agent, got %d", agents.Count())
	}

	// Verify agent properties
	a, err := agents.Get("abc123")
	if err != nil {
		t.Fatalf("failed to get agent: %v", err)
	}

	if a.ID != "abc123" {
		t.Errorf("agent ID = %s, want abc123", a.ID)
	}
	if a.GetState() != agent.StateIdle {
		t.Errorf("agent state = %v, want idle", a.GetState())
	}
	if a.GetTask() != "TASK-123" {
		t.Errorf("agent task = %s, want TASK-123", a.GetTask())
	}
	if a.GetDescription() != "Working on feature" {
		t.Errorf("agent description = %s, want 'Working on feature'", a.GetDescription())
	}
}

func TestRehydrateFromHost_AlreadyExists(t *testing.T) {
	// Create temp directory for config and project storage
	tmpDir, err := os.MkdirTemp("", "fab-rehydrate-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// Create a git repo to act as "remote"
	repoDir := filepath.Join(tmpDir, "repo")
	if err := os.MkdirAll(repoDir, 0755); err != nil {
		t.Fatalf("failed to create repo dir: %v", err)
	}
	cmd := exec.Command("git", "init")
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to init git repo: %v", err)
	}
	cmd = exec.Command("git", "commit", "--allow-empty", "-m", "Initial commit")
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	if err := cmd.Run(); err != nil {
		t.Fatalf("failed to create initial commit: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.toml")
	reg, err := registry.NewWithPath(configPath)
	if err != nil {
		t.Fatalf("failed to create registry: %v", err)
	}

	// Set project base dir to temp directory
	reg.SetProjectBaseDir(tmpDir)

	// Add a project
	proj, err := reg.Add("file://"+repoDir, "test-project", 3, false, "claude")
	if err != nil {
		t.Fatalf("failed to add project: %v", err)
	}
	proj.BaseDir = tmpDir

	agents := agent.NewManager()
	sup := New(reg, agents)

	// Register project with agent manager
	agents.RegisterProject(proj)

	// Pre-hydrate an agent
	worktreeDir := filepath.Join(tmpDir, "worktree-existing")
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		t.Fatalf("failed to create worktree dir: %v", err)
	}

	_, err = agents.Hydrate(agent.HydrateInfo{
		ID:        "existing",
		Project:   "test-project",
		State:     agent.StateRunning,
		Worktree:  worktreeDir,
		StartedAt: time.Now(),
		Backend:   "claude",
	})
	if err != nil {
		t.Fatalf("failed to pre-hydrate agent: %v", err)
	}

	// Try to rehydrate the same agent again
	host := agenthost.DiscoveredHost{
		AgentID:    "existing",
		SocketPath: "/tmp/existing.sock",
		Status: &agenthost.StatusResponse{
			Agent: agenthost.AgentInfo{
				ID:        "existing",
				Project:   "test-project",
				State:     "idle",
				Worktree:  worktreeDir,
				StartedAt: time.Now(),
				Backend:   "claude",
			},
		},
	}

	// Should silently skip when agent already exists
	err = sup.rehydrateFromHost(host)
	if err != nil {
		t.Errorf("rehydrateFromHost() error = %v, want nil", err)
	}

	// Verify still only 1 agent
	if agents.Count() != 1 {
		t.Errorf("expected 1 agent, got %d", agents.Count())
	}
}
