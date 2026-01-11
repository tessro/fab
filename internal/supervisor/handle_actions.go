package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/orchestrator"
)

// handleAgentDone handles agent completion signals.
func (s *Supervisor) handleAgentDone(ctx context.Context, req *daemon.Request) *daemon.Response {
	var doneReq daemon.AgentDoneRequest
	if err := unmarshalPayload(req.Payload, &doneReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if doneReq.AgentID == "" {
		return errorResponse(req, "agent_id is required")
	}

	// Check if this is a planner agent (agent ID starts with "plan:")
	if strings.HasPrefix(doneReq.AgentID, "plan:") {
		plannerID := strings.TrimPrefix(doneReq.AgentID, "plan:")
		return s.handlePlannerDone(ctx, req, plannerID, doneReq.Error)
	}

	// Find the agent and its orchestrator
	orch := s.getOrchestratorForAgent(doneReq.AgentID)
	if orch == nil {
		return errorResponse(req, "agent not found or no orchestrator")
	}

	// Notify the orchestrator
	result, err := orch.HandleAgentDone(doneReq.AgentID, doneReq.TaskID, doneReq.Error)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("handle agent done: %v", err))
	}

	resp := daemon.AgentDoneResponse{
		Merged:     result.Merged,
		BranchName: result.BranchName,
		SHA:        result.SHA,
		MergeError: result.MergeError,
	}

	if !result.Merged {
		// Return success: false to signal agent should resolve conflicts
		return &daemon.Response{
			Type:    req.Type,
			ID:      req.ID,
			Success: false,
			Error:   fmt.Sprintf("merge conflict on %s: %s", result.BranchName, result.MergeError),
			Payload: resp,
		}
	}

	return successResponse(req, resp)
}

// handleListStagedActions returns all pending staged actions.
func (s *Supervisor) handleListStagedActions(_ context.Context, req *daemon.Request) *daemon.Response {
	var listReq daemon.StagedActionsRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &listReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
		}
	}

	var actions []daemon.StagedAction

	s.mu.RLock()
	for name, orch := range s.orchestrators {
		if listReq.Project != "" && listReq.Project != name {
			continue
		}

		for _, a := range orch.Actions().List() {
			actions = append(actions, daemon.StagedAction{
				ID:        a.ID,
				AgentID:   a.AgentID,
				Project:   a.Project,
				Type:      daemon.ActionType(a.Type),
				Payload:   a.Payload,
				CreatedAt: a.CreatedAt,
			})
		}
	}
	s.mu.RUnlock()

	return successResponse(req, daemon.StagedActionsResponse{
		Actions: actions,
	})
}

// handleApproveAction approves and executes a staged action.
func (s *Supervisor) handleApproveAction(_ context.Context, req *daemon.Request) *daemon.Response {
	var approveReq daemon.ApproveActionRequest
	if err := unmarshalPayload(req.Payload, &approveReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if approveReq.ActionID == "" {
		return errorResponse(req, "action ID required")
	}

	// Find the orchestrator with this action
	s.mu.RLock()
	var foundOrch *orchestrator.Orchestrator
	for _, orch := range s.orchestrators {
		if _, ok := orch.Actions().Get(approveReq.ActionID); ok {
			foundOrch = orch
			break
		}
	}
	s.mu.RUnlock()

	if foundOrch == nil {
		return errorResponse(req, "action not found")
	}

	// Use the orchestrator's ApproveAction method which removes and executes
	if err := foundOrch.ApproveAction(approveReq.ActionID); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to execute action: %v", err))
	}

	return successResponse(req, nil)
}

// handleRejectAction rejects a staged action without executing it.
func (s *Supervisor) handleRejectAction(_ context.Context, req *daemon.Request) *daemon.Response {
	var rejectReq daemon.RejectActionRequest
	if err := unmarshalPayload(req.Payload, &rejectReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if rejectReq.ActionID == "" {
		return errorResponse(req, "action ID required")
	}

	// Find the orchestrator with this action and reject it
	s.mu.RLock()
	for _, orch := range s.orchestrators {
		if _, ok := orch.Actions().Get(rejectReq.ActionID); ok {
			s.mu.RUnlock()
			if err := orch.RejectAction(rejectReq.ActionID, rejectReq.Reason); err != nil {
				return errorResponse(req, fmt.Sprintf("failed to reject action: %v", err))
			}
			return successResponse(req, nil)
		}
	}
	s.mu.RUnlock()

	return errorResponse(req, "action not found")
}

// handlePlannerDone handles completion signals from planner agents.
// It stops the planner and deletes it from the manager, triggering
// the appropriate cleanup and TUI events.
func (s *Supervisor) handlePlannerDone(_ context.Context, req *daemon.Request, plannerID, errMsg string) *daemon.Response {
	p, err := s.planners.Get(plannerID)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("planner not found: %s", plannerID))
	}

	// Log completion
	if errMsg != "" {
		slog.Warn("planner completed with error",
			"planner", plannerID,
			"project", p.Project(),
			"error", errMsg,
		)
	} else {
		slog.Info("planner completed successfully",
			"planner", plannerID,
			"project", p.Project(),
		)
	}

	// Stop the planner gracefully
	if err := s.planners.Stop(plannerID); err != nil {
		slog.Warn("error stopping planner", "planner", plannerID, "error", err)
		// Continue with deletion even if stop fails
	}

	// Delete the planner from manager (triggers EventDeleted for TUI)
	if err := s.planners.Delete(plannerID); err != nil {
		slog.Warn("error deleting planner", "planner", plannerID, "error", err)
	}

	return successResponse(req, daemon.AgentDoneResponse{})
}
