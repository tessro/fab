package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/tessro/fab/internal/daemon"
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
