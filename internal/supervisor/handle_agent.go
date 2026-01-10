package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/daemon"
)

// ManagerAgentID is the special agent ID for the manager in the agent list.
const ManagerAgentID = "manager"

// handleAgentList lists agents.
func (s *Supervisor) handleAgentList(ctx context.Context, req *daemon.Request) *daemon.Response {
	var listReq daemon.AgentListRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &listReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
		}
	}

	agents := s.agents.List(listReq.Project)
	slog.Debug("agent list requested", "filter", listReq.Project, "count", len(agents))
	statuses := make([]daemon.AgentStatus, 0, len(agents)+1)

	// Add running project managers to the list
	s.mu.RLock()
	for projectName, mgr := range s.managers {
		// Skip if filtering by project and this isn't the one
		if listReq.Project != "" && listReq.Project != projectName {
			continue
		}

		// Check if project has a running manager
		if mgr.IsRunning() {
			statuses = append(statuses, daemon.AgentStatus{
				ID:          ManagerAgentID,
				Project:     projectName,
				State:       string(mgr.State()),
				Worktree:    mgr.WorkDir(),
				StartedAt:   mgr.StartedAt(),
				Task:        "",
				Description: "Manager",
			})
		}
	}
	s.mu.RUnlock()

	for _, a := range agents {
		info := a.Info()
		statuses = append(statuses, daemon.AgentStatus{
			ID:          info.ID,
			Project:     info.Project,
			State:       string(info.State),
			Worktree:    info.Worktree,
			StartedAt:   info.StartedAt,
			Task:        info.Task,
			Description: info.Description,
		})
	}

	return successResponse(req, daemon.AgentListResponse{
		Agents: statuses,
	})
}

// handleAgentCreate creates a new agent.
func (s *Supervisor) handleAgentCreate(ctx context.Context, req *daemon.Request) *daemon.Response {
	var createReq daemon.AgentCreateRequest
	if err := unmarshalPayload(req.Payload, &createReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if createReq.Project == "" {
		return errorResponse(req, "project name required")
	}

	proj, err := s.registry.Get(createReq.Project)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("project not found: %s", createReq.Project))
	}

	a, err := s.agents.Create(proj)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("failed to create agent: %v", err))
	}

	return successResponse(req, daemon.AgentCreateResponse{
		ID:       a.ID,
		Project:  proj.Name,
		Worktree: a.Info().Worktree,
	})
}

// handleAgentDelete deletes an agent.
func (s *Supervisor) handleAgentDelete(ctx context.Context, req *daemon.Request) *daemon.Response {
	var deleteReq daemon.AgentDeleteRequest
	if err := unmarshalPayload(req.Payload, &deleteReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if deleteReq.ID == "" {
		return errorResponse(req, "agent ID required")
	}

	// Stop the agent first
	if err := s.agents.Stop(deleteReq.ID); err != nil && err != agent.ErrAgentNotFound {
		if !deleteReq.Force {
			return errorResponse(req, fmt.Sprintf("failed to stop agent: %v", err))
		}
	}

	if err := s.agents.Delete(deleteReq.ID); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to delete agent: %v", err))
	}

	return successResponse(req, nil)
}

// handleAgentAbort aborts a running agent.
// If force is false, sends /quit for graceful shutdown.
// If force is true, kills the process immediately with SIGKILL.
func (s *Supervisor) handleAgentAbort(ctx context.Context, req *daemon.Request) *daemon.Response {
	var abortReq daemon.AgentAbortRequest
	if err := unmarshalPayload(req.Payload, &abortReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if abortReq.ID == "" {
		return errorResponse(req, "agent ID required")
	}

	a, err := s.agents.Get(abortReq.ID)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("agent not found: %s", abortReq.ID))
	}

	// Check if agent is already in terminal state
	if a.IsTerminal() {
		return errorResponse(req, fmt.Sprintf("agent %s is already in %s state", abortReq.ID, a.GetState()))
	}

	if abortReq.Force {
		// Force stop: sends SIGTERM then SIGKILL after timeout
		if err := a.Stop(); err != nil {
			return errorResponse(req, fmt.Sprintf("failed to stop agent: %v", err))
		}
	} else {
		// Graceful abort: send /quit command
		if err := a.SendMessage("/quit"); err != nil {
			return errorResponse(req, fmt.Sprintf("failed to send quit command: %v", err))
		}
	}

	return successResponse(req, nil)
}

// handleAgentInput sends raw input to an agent's stdin.
// Deprecated: Use handleAgentSendMessage for structured message input.
func (s *Supervisor) handleAgentInput(ctx context.Context, req *daemon.Request) *daemon.Response {
	var inputReq daemon.AgentInputRequest
	if err := unmarshalPayload(req.Payload, &inputReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if inputReq.ID == "" {
		return errorResponse(req, "agent ID required")
	}

	a, err := s.agents.Get(inputReq.ID)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("agent not found: %s", inputReq.ID))
	}

	n, err := a.Write([]byte(inputReq.Input))
	if err != nil {
		return errorResponse(req, fmt.Sprintf("failed to write to agent: %v", err))
	}

	return successResponse(req, map[string]int{"bytes_written": n})
}

// handleAgentOutput returns buffered output for an agent.
func (s *Supervisor) handleAgentOutput(ctx context.Context, req *daemon.Request) *daemon.Response {
	var outputReq daemon.AgentOutputRequest
	if err := unmarshalPayload(req.Payload, &outputReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if outputReq.ID == "" {
		return errorResponse(req, "agent ID required")
	}

	a, err := s.agents.Get(outputReq.ID)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("agent not found: %s", outputReq.ID))
	}

	// Get all buffered output from the agent's chat history
	output := string(a.Output(-1))

	return successResponse(req, &daemon.AgentOutputResponse{
		ID:     outputReq.ID,
		Output: output,
	})
}

// handleAgentSendMessage sends a message to an agent using the stream-json protocol.
func (s *Supervisor) handleAgentSendMessage(ctx context.Context, req *daemon.Request) *daemon.Response {
	var sendReq daemon.AgentSendMessageRequest
	if err := unmarshalPayload(req.Payload, &sendReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if sendReq.ID == "" {
		return errorResponse(req, "agent ID required")
	}

	a, err := s.agents.Get(sendReq.ID)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("agent not found: %s", sendReq.ID))
	}

	// Mark that user is intervening (for kickstart pause logic)
	a.MarkUserInput()

	if err := a.SendMessage(sendReq.Content); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to send message: %v", err))
	}

	// Broadcast intervention state change
	s.broadcastInterventionState(a.Info().ID, a.Info().Project, true)

	return successResponse(req, nil)
}

// handleAgentChatHistory returns the chat history for an agent.
func (s *Supervisor) handleAgentChatHistory(ctx context.Context, req *daemon.Request) *daemon.Response {
	var histReq daemon.AgentChatHistoryRequest
	if err := unmarshalPayload(req.Payload, &histReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if histReq.ID == "" {
		return errorResponse(req, "agent ID required")
	}

	a, err := s.agents.Get(histReq.ID)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("agent not found: %s", histReq.ID))
	}

	// Get entries from the agent's history
	entries := a.History().Entries(histReq.Limit)

	// Convert to DTO format
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

	return successResponse(req, daemon.AgentChatHistoryResponse{
		AgentID: histReq.ID,
		Entries: dtos,
	})
}

// handleAgentDescribe sets the description for an agent.
func (s *Supervisor) handleAgentDescribe(ctx context.Context, req *daemon.Request) *daemon.Response {
	var descReq daemon.AgentDescribeRequest
	if err := unmarshalPayload(req.Payload, &descReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if descReq.AgentID == "" {
		return errorResponse(req, "agent_id is required")
	}

	a, err := s.agents.Get(descReq.AgentID)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("agent not found: %s", descReq.AgentID))
	}

	a.SetDescription(descReq.Description)

	slog.Info("agent description set",
		"agent", descReq.AgentID,
		"description", descReq.Description,
	)

	return successResponse(req, nil)
}
