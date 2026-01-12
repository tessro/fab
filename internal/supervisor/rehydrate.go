package supervisor

import (
	"log/slog"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/agenthost"
)

// RehydrateFromHosts discovers running agent hosts and restores agents from them.
// This should be called during supervisor startup, before StartAutostart.
// It returns the number of agents successfully rehydrated.
func (s *Supervisor) RehydrateFromHosts() int {
	hosts, err := agenthost.DiscoverActiveHosts()
	if err != nil {
		slog.Warn("failed to discover agent hosts", "error", err)
		return 0
	}

	if len(hosts) == 0 {
		slog.Debug("no active agent hosts found")
		return 0
	}

	slog.Info("discovered active agent hosts", "count", len(hosts))

	rehydrated := 0
	for _, host := range hosts {
		if err := s.rehydrateFromHost(host); err != nil {
			slog.Warn("failed to rehydrate agent from host",
				"agent_id", host.AgentID,
				"error", err,
			)
			continue
		}
		rehydrated++
	}

	slog.Info("rehydrated agents from hosts", "count", rehydrated)
	return rehydrated
}

// rehydrateFromHost restores a single agent from discovered host data.
func (s *Supervisor) rehydrateFromHost(host agenthost.DiscoveredHost) error {
	if host.Status == nil {
		return nil // No status available
	}

	agentInfo := host.Status.Agent

	// Check if agent already exists
	if s.agents.Exists(agentInfo.ID) {
		slog.Debug("agent already exists, skipping rehydration",
			"agent_id", agentInfo.ID,
		)
		return nil
	}

	// Check if the project is registered
	proj, err := s.registry.Get(agentInfo.Project)
	if err != nil {
		slog.Debug("project not registered, skipping agent rehydration",
			"agent_id", agentInfo.ID,
			"project", agentInfo.Project,
		)
		return nil
	}

	// Register the project with the agent manager if not already done
	s.agents.RegisterProject(proj)

	// Parse state from host response
	state := parseAgentState(agentInfo.State)

	// Hydrate the agent
	info := agent.HydrateInfo{
		ID:          agentInfo.ID,
		Project:     agentInfo.Project,
		State:       state,
		Worktree:    agentInfo.Worktree,
		Task:        agentInfo.Task,
		Description: agentInfo.Description,
		StartedAt:   agentInfo.StartedAt,
		Backend:     agentInfo.Backend,
	}

	a, err := s.agents.Hydrate(info)
	if err != nil {
		return err
	}

	slog.Info("agent rehydrated from host",
		"agent_id", a.ID,
		"project", agentInfo.Project,
		"state", state,
		"worktree", agentInfo.Worktree,
	)

	return nil
}

// parseAgentState converts a state string to an agent.State.
func parseAgentState(s string) agent.State {
	switch s {
	case "starting":
		return agent.StateStarting
	case "running":
		return agent.StateRunning
	case "idle":
		return agent.StateIdle
	case "done":
		return agent.StateDone
	case "error":
		return agent.StateError
	default:
		// Default to running if unknown
		return agent.StateRunning
	}
}
