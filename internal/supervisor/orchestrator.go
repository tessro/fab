package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tessro/fab/internal/issue"
	"github.com/tessro/fab/internal/issue/gh"
	"github.com/tessro/fab/internal/issue/tk"
	"github.com/tessro/fab/internal/orchestrator"
	"github.com/tessro/fab/internal/project"
)

// startOrchestrator creates and starts an orchestrator for the given project.
func (s *Supervisor) startOrchestrator(_ context.Context, proj *project.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already running
	if orch, ok := s.orchestrators[proj.Name]; ok && orch.IsRunning() {
		return nil
	}

	// Worktrees are created on-demand when agents start

	// Register project with agent manager
	s.agents.RegisterProject(proj)

	// Configure orchestrator with issue backend factory for auto-spawning
	cfg := s.orchConfig
	cfg.IssueBackendFactory = issueBackendFactoryForProject(proj)

	// Create orchestrator
	orch := orchestrator.New(proj, s.agents, cfg)
	s.orchestrators[proj.Name] = orch

	// Register callback for action queue events
	orch.Actions().OnAdded(func(action orchestrator.StagedAction) {
		s.handleActionQueued(proj.Name, action)
	})

	// Mark project as running
	proj.SetRunning(true)

	// Start the orchestrator
	return orch.Start()
}

// issueBackendFactoryForProject creates an issue backend factory based on project config.
func issueBackendFactoryForProject(proj *project.Project) issue.NewBackendFunc {
	backendType := proj.IssueBackend
	if backendType == "" {
		backendType = "tk" // Default to tk backend
	}

	return func(repoDir string) (issue.Backend, error) {
		switch backendType {
		case "tk":
			return tk.New(repoDir)
		case "github", "gh":
			return gh.New(repoDir, proj.AllowedAuthors)
		default:
			return nil, fmt.Errorf("unknown issue backend: %s", backendType)
		}
	}
}

// stopOrchestrator stops the orchestrator for the given project.
func (s *Supervisor) stopOrchestrator(projectName string) {
	s.mu.Lock()
	orch, ok := s.orchestrators[projectName]
	s.mu.Unlock()

	if !ok {
		return
	}

	// Stop the orchestrator
	orch.Stop()

	// Stop all agents
	s.agents.StopAll(projectName)

	// Mark project as not running
	proj, err := s.registry.Get(projectName)
	if err == nil {
		proj.SetRunning(false)
	}

	// Clean up orchestrator
	s.mu.Lock()
	delete(s.orchestrators, projectName)
	s.mu.Unlock()
}

// StartAutostart starts orchestration for all projects with autostart=true.
// This should be called once during daemon startup.
func (s *Supervisor) StartAutostart() {
	ctx := context.Background()
	for _, proj := range s.registry.List() {
		if proj.Autostart {
			slog.Info("autostarting project", "project", proj.Name)
			if err := s.startOrchestrator(ctx, proj); err != nil {
				slog.Error("failed to autostart project",
					"project", proj.Name,
					"error", err)
			}
		}
	}
}

// ShutdownTimeout is the maximum time to wait for graceful shutdown.
const ShutdownTimeout = 30 * time.Second

// Shutdown gracefully stops all orchestrators and agents.
// This should be called during daemon shutdown.
func (s *Supervisor) Shutdown() {
	s.ShutdownWithTimeout(ShutdownTimeout)
}

// ShutdownWithTimeout stops all orchestrators and agents with a timeout.
// Returns true if shutdown completed gracefully, false if timed out.
func (s *Supervisor) ShutdownWithTimeout(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		s.shutdownInternal()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		slog.Warn("shutdown timed out, some agents may not have stopped gracefully",
			"timeout", timeout)
		return false
	}
}

// shutdownInternal performs the actual shutdown work.
func (s *Supervisor) shutdownInternal() {
	// Stop heartbeat monitor first
	if s.heartbeat != nil {
		s.heartbeat.Stop()
	}

	// Get list of running orchestrators
	s.mu.RLock()
	projectNames := make([]string, 0, len(s.orchestrators))
	for name := range s.orchestrators {
		projectNames = append(projectNames, name)
	}
	s.mu.RUnlock()

	// Stop each orchestrator (which also stops its agents)
	for _, name := range projectNames {
		s.stopOrchestrator(name)
	}
}

// getOrchestrator returns the orchestrator for a project, or nil if not running.
func (s *Supervisor) getOrchestrator(projectName string) *orchestrator.Orchestrator {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.orchestrators[projectName]
}

// getOrchestratorForAgent finds the orchestrator for an agent by ID.
func (s *Supervisor) getOrchestratorForAgent(agentID string) *orchestrator.Orchestrator {
	a, err := s.agents.Get(agentID)
	if err != nil {
		return nil
	}

	info := a.Info()
	return s.getOrchestrator(info.Project)
}
