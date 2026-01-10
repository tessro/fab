package supervisor

import (
	"context"
	"fmt"
	"os"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/daemon"
)

// handleStart starts orchestration for a project.
func (s *Supervisor) handleStart(ctx context.Context, req *daemon.Request) *daemon.Response {
	var startReq daemon.StartRequest
	if err := unmarshalPayload(req.Payload, &startReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if startReq.All {
		// Start all projects
		projects := s.registry.List()
		for _, p := range projects {
			if err := s.startOrchestrator(ctx, p); err != nil {
				return errorResponse(req, fmt.Sprintf("failed to start project %s: %v", p.Name, err))
			}
		}
		return successResponse(req, nil)
	}

	if startReq.Project == "" {
		return errorResponse(req, "project name required")
	}

	proj, err := s.registry.Get(startReq.Project)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("project not found: %s", startReq.Project))
	}

	if err := s.startOrchestrator(ctx, proj); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to start project: %v", err))
	}

	return successResponse(req, nil)
}

// handleStop stops orchestration for a project.
func (s *Supervisor) handleStop(ctx context.Context, req *daemon.Request) *daemon.Response {
	var stopReq daemon.StopRequest
	if err := unmarshalPayload(req.Payload, &stopReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if stopReq.All {
		// Stop all projects
		projects := s.registry.List()
		for _, p := range projects {
			s.stopOrchestrator(p.Name)
		}
		return successResponse(req, nil)
	}

	if stopReq.Project == "" {
		return errorResponse(req, "project name required")
	}

	proj, err := s.registry.Get(stopReq.Project)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("project not found: %s", stopReq.Project))
	}

	s.stopOrchestrator(proj.Name)
	return successResponse(req, nil)
}

// handleStatus returns the current daemon status.
func (s *Supervisor) handleStatus(ctx context.Context, req *daemon.Request) *daemon.Response {
	projects := s.registry.List()
	stateCounts := s.agents.CountByState()

	// Count active projects
	activeProjects := 0
	projectStatuses := make([]daemon.ProjectStatus, 0, len(projects))

	for _, p := range projects {
		if p.IsRunning() {
			activeProjects++
		}

		agents := s.agents.List(p.Name)
		agentStatuses := make([]daemon.AgentStatus, 0, len(agents))
		for _, a := range agents {
			info := a.Info()
			agentStatuses = append(agentStatuses, daemon.AgentStatus{
				ID:          info.ID,
				Project:     info.Project,
				State:       string(info.State),
				Worktree:    info.Worktree,
				StartedAt:   info.StartedAt,
				Task:        info.Task,
				Description: info.Description,
			})
		}

		projectStatuses = append(projectStatuses, daemon.ProjectStatus{
			Name:         p.Name,
			RemoteURL:    p.RemoteURL,
			Running:      p.IsRunning(),
			MaxAgents:    p.MaxAgents,
			ActiveAgents: p.ActiveAgentCount(),
			Agents:       agentStatuses,
		})
	}

	status := daemon.StatusResponse{
		Daemon: daemon.DaemonStatus{
			Running:   true,
			PID:       os.Getpid(),
			StartedAt: s.startedAt,
			Version:   Version,
		},
		Supervisor: daemon.SupervisorStatus{
			ActiveProjects: activeProjects,
			TotalAgents:    s.agents.Count(),
			RunningAgents:  stateCounts[agent.StateRunning],
			IdleAgents:     stateCounts[agent.StateIdle],
		},
		Projects: projectStatuses,
	}

	return successResponse(req, status)
}
