package orchestrator

import (
	"strings"
	"testing"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/project"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.KickstartPrompt == "" {
		t.Error("expected KickstartPrompt to be non-empty")
	}
}

func TestNew(t *testing.T) {
	proj := &project.Project{Name: "test-project", MaxAgents: 3}
	agents := agent.NewManager()
	cfg := DefaultConfig()

	orch := New(proj, agents, cfg)

	if orch.project != proj {
		t.Error("expected orchestrator to have the project set")
	}
	if orch.agents != agents {
		t.Error("expected orchestrator to have the agent manager set")
	}
	if orch.claims == nil {
		t.Error("expected orchestrator to have a claim registry")
	}
	if orch.commits == nil {
		t.Error("expected orchestrator to have a commit log")
	}
	if orch.IsRunning() {
		t.Error("expected orchestrator to not be running initially")
	}
}

func TestOrchestrator_StartStop(t *testing.T) {
	proj := &project.Project{Name: "test-project", MaxAgents: 0} // 0 agents to avoid spawning
	agents := agent.NewManager()
	cfg := DefaultConfig()

	orch := New(proj, agents, cfg)

	// Start should succeed
	if err := orch.Start(); err != nil {
		t.Errorf("Start() returned error: %v", err)
	}

	if !orch.IsRunning() {
		t.Error("expected orchestrator to be running after Start()")
	}

	// Starting again should fail
	if err := orch.Start(); err != ErrAlreadyRunning {
		t.Errorf("expected ErrAlreadyRunning, got %v", err)
	}

	// Stop should work
	orch.Stop()

	// Give the goroutine time to clean up
	time.Sleep(10 * time.Millisecond)

	if orch.IsRunning() {
		t.Error("expected orchestrator to not be running after Stop()")
	}

	// Stopping again should be safe (no panic)
	orch.Stop()
}

func TestOrchestrator_Project(t *testing.T) {
	proj := &project.Project{Name: "test-project"}
	agents := agent.NewManager()
	cfg := DefaultConfig()

	orch := New(proj, agents, cfg)

	if orch.Project() != proj {
		t.Error("expected Project() to return the project")
	}
}

func TestOrchestrator_Config(t *testing.T) {
	proj := &project.Project{Name: "test-project"}
	agents := agent.NewManager()
	cfg := DefaultConfig()
	cfg.KickstartPrompt = "custom prompt"

	orch := New(proj, agents, cfg)

	if orch.Config().KickstartPrompt != "custom prompt" {
		t.Error("expected Config() to return the configured prompt")
	}

	// Test SetConfig
	newCfg := DefaultConfig()
	newCfg.KickstartPrompt = "updated prompt"
	orch.SetConfig(newCfg)

	if orch.Config().KickstartPrompt != "updated prompt" {
		t.Error("expected SetConfig() to update the prompt")
	}
}

func TestOrchestrator_Claims(t *testing.T) {
	proj := &project.Project{Name: "test-project"}
	agents := agent.NewManager()
	cfg := DefaultConfig()

	orch := New(proj, agents, cfg)

	claims := orch.Claims()
	if claims == nil {
		t.Fatal("expected Claims() to return non-nil")
	}

	// Should be able to claim tickets
	if err := claims.Claim("TICKET-1", "agent-1"); err != nil {
		t.Errorf("expected claim to succeed, got %v", err)
	}
}

func TestOrchestrator_Commits(t *testing.T) {
	proj := &project.Project{Name: "test-project"}
	agents := agent.NewManager()
	cfg := DefaultConfig()

	orch := New(proj, agents, cfg)

	commits := orch.Commits()
	if commits == nil {
		t.Fatal("expected Commits() to return non-nil")
	}

	// Should be able to add commits
	commits.Add(CommitRecord{
		SHA:      "abc123",
		Branch:   "feature",
		AgentID:  "agent-1",
		TaskID:   "task-1",
		MergedAt: time.Now(),
	})

	if commits.Len() != 1 {
		t.Errorf("expected 1 commit, got %d", commits.Len())
	}
}

func TestOrchestrator_KickstartPromptContent(t *testing.T) {
	cfg := DefaultConfig()

	// Verify the default kickstart prompt contains expected instructions
	prompt := cfg.KickstartPrompt

	expectedPhrases := []string{
		"fab issue ready",
		"fab agent claim",
		"fab agent done",
		"fab issue close",
	}

	for _, phrase := range expectedPhrases {
		if !strings.Contains(prompt, phrase) {
			t.Errorf("expected kickstart prompt to contain '%s'", phrase)
		}
	}
}

func TestOrchestrator_ExecuteKickstart_SkipsWhenUserIntervening(t *testing.T) {
	proj := &project.Project{Name: "test-project"}
	agents := agent.NewManager()
	cfg := DefaultConfig()
	cfg.KickstartPrompt = "test kickstart prompt"
	cfg.InterventionSilence = 60 * time.Second

	orch := New(proj, agents, cfg)

	// Create a mock agent
	a := mockAgent("agent-1", proj.Name)

	// Mark user input to simulate intervention
	a.MarkUserInput()

	// Execute kickstart - should be skipped
	if orch.ExecuteKickstart(a) {
		t.Error("expected ExecuteKickstart to return false when user is intervening")
	}
}

func TestOrchestrator_ExecuteKickstart_ProceedsAfterSilence(t *testing.T) {
	proj := &project.Project{Name: "test-project"}
	agents := agent.NewManager()
	cfg := DefaultConfig()
	cfg.KickstartPrompt = "test kickstart prompt"
	cfg.InterventionSilence = 10 * time.Millisecond // Very short for testing

	orch := New(proj, agents, cfg)

	// Create a mock agent
	a := mockAgent("agent-1", proj.Name)

	// Mark user input
	a.MarkUserInput()

	// Wait for silence threshold to pass
	time.Sleep(20 * time.Millisecond)

	// Execute kickstart - should proceed now
	// Note: returns true even though sendMessage may fail (no stdin)
	orch.ExecuteKickstart(a)
	// Just verify it doesn't panic - actual execution requires a running agent
}

func TestOrchestrator_InterventionSilence_DisabledWhenZero(t *testing.T) {
	proj := &project.Project{Name: "test-project"}
	agents := agent.NewManager()
	cfg := DefaultConfig()
	cfg.KickstartPrompt = "test kickstart prompt"
	cfg.InterventionSilence = 0 // Disabled

	orch := New(proj, agents, cfg)

	// Create a mock agent
	a := mockAgent("agent-1", proj.Name)

	// Mark user input
	a.MarkUserInput()

	// Execute kickstart - should proceed even with recent user input
	// Note: ExecuteKickstart returns true when intervention detection is disabled
	if !orch.ExecuteKickstart(a) {
		t.Error("expected ExecuteKickstart to proceed when intervention detection is disabled")
	}
}

func TestOrchestrator_IsAgentIntervening(t *testing.T) {
	proj := &project.Project{Name: "test-project"}
	agents := agent.NewManager()
	agents.RegisterProject(proj)

	cfg := DefaultConfig()
	cfg.InterventionSilence = 60 * time.Second

	orch := New(proj, agents, cfg)

	// Create a real agent through manager
	a, err := agents.Create(proj)
	if err != nil {
		t.Skipf("skipping test: could not create agent: %v", err)
	}

	// Initially not intervening
	if orch.IsAgentIntervening(a.ID) {
		t.Error("expected agent not intervening initially")
	}

	// Mark user input
	a.MarkUserInput()

	// Should be intervening now
	if !orch.IsAgentIntervening(a.ID) {
		t.Error("expected agent intervening after MarkUserInput")
	}
}

func TestDefaultConfig_IncludesInterventionSilence(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.InterventionSilence != agent.DefaultInterventionSilence {
		t.Errorf("expected InterventionSilence=%v, got %v",
			agent.DefaultInterventionSilence, cfg.InterventionSilence)
	}
}

// mockAgent creates a minimal agent for testing
func mockAgent(id, projectName string) *agent.Agent {
	proj := &project.Project{Name: projectName}
	a := agent.New(id, proj, nil)
	return a
}
