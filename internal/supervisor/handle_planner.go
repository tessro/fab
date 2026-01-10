package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/planner"
)

// handlePlanStart starts a planning agent.
func (s *Supervisor) handlePlanStart(_ context.Context, req *daemon.Request) *daemon.Response {
	slog.Debug("handlePlanStart: received request")

	var startReq daemon.PlanStartRequest
	if err := unmarshalPayload(req.Payload, &startReq); err != nil {
		slog.Error("handlePlanStart: invalid payload", "error", err)
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	slog.Debug("handlePlanStart: parsed request", "project", startReq.Project, "prompt_len", len(startReq.Prompt))

	if startReq.Prompt == "" {
		slog.Error("handlePlanStart: empty prompt")
		return errorResponse(req, "prompt is required")
	}

	// Determine working directory
	var workDir string
	var projectName string

	if startReq.Project != "" {
		slog.Debug("handlePlanStart: using project worktree", "project", startReq.Project)

		// Use project worktree
		proj, err := s.registry.Get(startReq.Project)
		if err != nil {
			slog.Error("handlePlanStart: project not found", "project", startReq.Project, "error", err)
			return errorResponse(req, fmt.Sprintf("project not found: %s", startReq.Project))
		}

		projectName = proj.Name

		// Generate a planner ID first so we can create the worktree
		plannerID := s.planners.GenerateID()
		slog.Debug("handlePlanStart: generated planner ID", "id", plannerID)

		// Create a dedicated worktree for the planner (not subject to MaxAgents)
		slog.Debug("handlePlanStart: creating planner worktree", "id", plannerID)
		wtPath, err := proj.CreatePlannerWorktree(plannerID)
		if err != nil {
			slog.Error("handlePlanStart: failed to create worktree", "id", plannerID, "error", err)
			return errorResponse(req, fmt.Sprintf("failed to create planner worktree: %v", err))
		}
		workDir = wtPath
		slog.Debug("handlePlanStart: worktree created", "id", plannerID, "path", workDir)

		// Create the planner with the specific ID
		slog.Debug("handlePlanStart: creating planner instance", "id", plannerID)
		p, err := s.planners.CreateWithID(plannerID, projectName, workDir, startReq.Prompt)
		if err != nil {
			slog.Error("handlePlanStart: failed to create planner", "id", plannerID, "error", err)
			_ = proj.DeletePlannerWorktree(plannerID)
			return errorResponse(req, fmt.Sprintf("failed to create planner: %v", err))
		}

		// Set up entry callback for broadcasting
		p.OnEntry(func(entry agent.ChatEntry) {
			s.broadcastPlannerChatEntry(p.ID(), projectName, entry)
		})

		// Start the planner
		slog.Debug("handlePlanStart: starting planner", "id", plannerID)
		if err := p.Start(); err != nil {
			slog.Error("handlePlanStart: failed to start planner", "id", plannerID, "error", err)
			_ = s.planners.Delete(p.ID())
			_ = proj.DeletePlannerWorktree(plannerID)
			return errorResponse(req, fmt.Sprintf("failed to start planner: %v", err))
		}

		slog.Info("planner started",
			"planner", p.ID(),
			"project", projectName,
			"workdir", workDir,
		)

		return successResponse(req, daemon.PlanStartResponse{
			ID:      p.ID(),
			Project: projectName,
			WorkDir: workDir,
		})
	}

	// Use default planner directory (no project)
	slog.Debug("handlePlanStart: using default planner directory (no project)")
	home, _ := os.UserHomeDir()
	workDir = filepath.Join(home, ".fab", "planners")

	// Create the planner
	slog.Debug("handlePlanStart: creating planner instance", "workdir", workDir)
	p, err := s.planners.Create(projectName, workDir, startReq.Prompt)
	if err != nil {
		slog.Error("handlePlanStart: failed to create planner", "error", err)
		return errorResponse(req, fmt.Sprintf("failed to create planner: %v", err))
	}
	slog.Debug("handlePlanStart: planner created", "id", p.ID())

	// Set up entry callback for broadcasting
	p.OnEntry(func(entry agent.ChatEntry) {
		s.broadcastPlannerChatEntry(p.ID(), projectName, entry)
	})

	// Start the planner
	slog.Debug("handlePlanStart: starting planner", "id", p.ID())
	if err := p.Start(); err != nil {
		slog.Error("handlePlanStart: failed to start planner", "id", p.ID(), "error", err)
		_ = s.planners.Delete(p.ID())
		return errorResponse(req, fmt.Sprintf("failed to start planner: %v", err))
	}

	slog.Info("planner started",
		"planner", p.ID(),
		"project", projectName,
		"workdir", workDir,
	)

	return successResponse(req, daemon.PlanStartResponse{
		ID:      p.ID(),
		Project: projectName,
		WorkDir: workDir,
	})
}

// handlePlanStop stops a planning agent.
func (s *Supervisor) handlePlanStop(_ context.Context, req *daemon.Request) *daemon.Response {
	var stopReq daemon.PlanStopRequest
	if err := unmarshalPayload(req.Payload, &stopReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if stopReq.ID == "" {
		return errorResponse(req, "planner ID required")
	}

	if err := s.planners.Stop(stopReq.ID); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to stop planner: %v", err))
	}

	slog.Info("planner stopped", "planner", stopReq.ID)
	return successResponse(req, nil)
}

// handlePlanList lists planning agents.
func (s *Supervisor) handlePlanList(_ context.Context, req *daemon.Request) *daemon.Response {
	var listReq daemon.PlanListRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &listReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
		}
	}

	var planners []*planner.Planner
	if listReq.Project != "" {
		planners = s.planners.ListByProject(listReq.Project)
	} else {
		planners = s.planners.List()
	}

	statuses := make([]daemon.PlannerStatus, 0, len(planners))
	for _, p := range planners {
		info := p.Info()
		startedAt := ""
		if !info.StartedAt.IsZero() {
			startedAt = info.StartedAt.Format(time.RFC3339)
		}
		statuses = append(statuses, daemon.PlannerStatus{
			ID:        info.ID,
			Project:   info.Project,
			State:     string(info.State),
			WorkDir:   info.WorkDir,
			StartedAt: startedAt,
			PlanFile:  info.PlanFile,
		})
	}

	return successResponse(req, daemon.PlanListResponse{
		Planners: statuses,
	})
}

// handlePlanSendMessage sends a message to a planning agent.
func (s *Supervisor) handlePlanSendMessage(_ context.Context, req *daemon.Request) *daemon.Response {
	var sendReq daemon.PlanSendMessageRequest
	if err := unmarshalPayload(req.Payload, &sendReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if sendReq.ID == "" {
		return errorResponse(req, "planner ID required")
	}

	p, err := s.planners.Get(sendReq.ID)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("planner not found: %s", sendReq.ID))
	}

	if err := p.SendMessage(sendReq.Content); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to send message: %v", err))
	}

	return successResponse(req, nil)
}

// handlePlanChatHistory returns the chat history for a planning agent.
func (s *Supervisor) handlePlanChatHistory(_ context.Context, req *daemon.Request) *daemon.Response {
	var histReq daemon.PlanChatHistoryRequest
	if err := unmarshalPayload(req.Payload, &histReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if histReq.ID == "" {
		return errorResponse(req, "planner ID required")
	}

	p, err := s.planners.Get(histReq.ID)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("planner not found: %s", histReq.ID))
	}

	entries := p.History().Entries(histReq.Limit)

	dtos := make([]daemon.ChatEntryDTO, len(entries))
	for i, e := range entries {
		dtos[i] = daemon.ChatEntryDTO{
			Role:       e.Role,
			Content:    e.Content,
			ToolName:   e.ToolName,
			ToolInput:  e.ToolInput,
			ToolResult: e.ToolResult,
			Timestamp:  e.Timestamp.Format(time.RFC3339),
		}
	}

	return successResponse(req, daemon.PlanChatHistoryResponse{
		PlannerID: histReq.ID,
		Entries:   dtos,
	})
}
