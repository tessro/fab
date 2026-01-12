package supervisor

import (
	"context"
	"log/slog"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/planner"
)

// handleAttach subscribes a client to streaming events.
func (s *Supervisor) handleAttach(ctx context.Context, req *daemon.Request) *daemon.Response {
	var attachReq daemon.AttachRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &attachReq); err != nil {
			return errorResponse(req, "invalid payload: "+err.Error())
		}
	}

	conn := daemon.ConnFromContext(ctx)
	srv := daemon.ServerFromContext(ctx)
	encoder := daemon.EncoderFromContext(ctx)
	writeMu := daemon.WriteMuFromContext(ctx)

	if conn == nil || srv == nil || encoder == nil || writeMu == nil {
		return errorResponse(req, "internal error: missing connection context")
	}

	srv.Attach(conn, attachReq.Projects, encoder, writeMu)
	return successResponse(req, nil)
}

// handleDetach unsubscribes a client from streaming events.
func (s *Supervisor) handleDetach(ctx context.Context, req *daemon.Request) *daemon.Response {
	conn := daemon.ConnFromContext(ctx)
	srv := daemon.ServerFromContext(ctx)

	if conn == nil || srv == nil {
		return errorResponse(req, "internal error: missing connection context")
	}

	srv.Detach(conn)
	return successResponse(req, nil)
}

// SetServer sets the daemon server for broadcasting events.
// This must be called before agents are created.
func (s *Supervisor) SetServer(srv *daemon.Server) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.server = srv
}

// Server returns the daemon server, or nil if not set.
func (s *Supervisor) Server() *daemon.Server {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.server
}

// handleAgentEvent broadcasts agent events to attached clients.
func (s *Supervisor) handleAgentEvent(event agent.Event) {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()

	if srv == nil {
		return
	}

	var streamEvent *daemon.StreamEvent

	switch event.Type {
	case agent.EventCreated:
		info := event.Agent.Info()
		streamEvent = &daemon.StreamEvent{
			Type:      "created",
			AgentID:   info.ID,
			Project:   info.Project,
			StartedAt: info.StartedAt.Format(time.RFC3339),
		}
	case agent.EventStateChanged:
		info := event.Agent.Info()
		streamEvent = &daemon.StreamEvent{
			Type:    "state",
			AgentID: info.ID,
			Project: info.Project,
			State:   string(event.NewState),
		}
	case agent.EventInfoChanged:
		info := event.Agent.Info()
		streamEvent = &daemon.StreamEvent{
			Type:        "info",
			AgentID:     info.ID,
			Project:     info.Project,
			Task:        info.Task,
			Description: info.Description,
		}
	case agent.EventDeleted:
		info := event.Agent.Info()
		streamEvent = &daemon.StreamEvent{
			Type:    "deleted",
			AgentID: info.ID,
			Project: info.Project,
		}
	}

	if streamEvent != nil {
		srv.Broadcast(streamEvent)
	}
}

// broadcastChatEntry sends a chat entry to attached clients.
func (s *Supervisor) broadcastChatEntry(agentID, project string, entry agent.ChatEntry) {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()

	if srv == nil {
		slog.Debug("broadcastChatEntry: no server")
		return
	}

	slog.Debug("broadcastChatEntry",
		"agent", agentID,
		"project", project,
		"role", entry.Role,
		"content_len", len(entry.Content),
		"attached_clients", srv.AttachedCount(),
	)

	dto := &daemon.ChatEntryDTO{
		Role:       entry.Role,
		Content:    entry.Content,
		ToolName:   entry.ToolName,
		ToolInput:  entry.ToolInput,
		ToolResult: entry.ToolResult,
		Timestamp:  entry.Timestamp.Format(time.RFC3339),
	}
	srv.Broadcast(&daemon.StreamEvent{
		Type:      "chat_entry",
		AgentID:   agentID,
		Project:   project,
		ChatEntry: dto,
	})
}

// broadcastInterventionState sends an intervention state change to attached TUI clients.
func (s *Supervisor) broadcastInterventionState(agentID, project string, intervening bool) {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()

	if srv == nil {
		return
	}

	srv.Broadcast(&daemon.StreamEvent{
		Type:        "intervention",
		AgentID:     agentID,
		Project:     project,
		Intervening: &intervening,
	})
}

// StartAgentReadLoop starts the read loop for an agent.
// This should be called after the agent's process is started.
func (s *Supervisor) StartAgentReadLoop(a *agent.Agent) error {
	info := a.Info()

	cfg := agent.DefaultReadLoopConfig()
	cfg.OnEntry = func(entry agent.ChatEntry) {
		s.broadcastChatEntry(info.ID, info.Project, entry)
		// Record output for heartbeat monitoring
		if s.heartbeat != nil {
			s.heartbeat.RecordOutput(info.ID)
		}
	}
	cfg.OnExit = func(exitErr error) {
		// Remove from heartbeat monitoring
		if s.heartbeat != nil {
			s.heartbeat.RemoveAgent(info.ID)
		}
		// Release claims when agent crashes (non-nil exitErr means crash)
		if exitErr != nil {
			orch := s.getOrchestrator(info.Project)
			if orch != nil {
				released := orch.Claims().ReleaseByAgent(info.ID)
				if released > 0 {
					slog.Info("released claims for crashed agent",
						"agent", info.ID,
						"project", info.Project,
						"released", released,
						"error", exitErr,
					)
				}
			}
		}
	}

	return a.StartReadLoop(cfg)
}

// broadcastPermissionRequest sends a permission request to attached TUI clients.
func (s *Supervisor) broadcastPermissionRequest(req *daemon.PermissionRequest) {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()

	if srv == nil {
		return
	}

	srv.Broadcast(&daemon.StreamEvent{
		Type:              "permission_request",
		AgentID:           req.AgentID,
		Project:           req.Project,
		PermissionRequest: req,
	})
}

// broadcastUserQuestion sends a user question to attached TUI clients.
func (s *Supervisor) broadcastUserQuestion(question *daemon.UserQuestion) {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()

	if srv == nil {
		return
	}

	srv.Broadcast(&daemon.StreamEvent{
		Type:         "user_question",
		AgentID:      question.AgentID,
		Project:      question.Project,
		UserQuestion: question,
	})
}

// broadcastManagerChatEntry sends a manager chat entry to attached clients.
func (s *Supervisor) broadcastManagerChatEntry(projectName string, entry agent.ChatEntry) {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()

	if srv == nil {
		return
	}

	dto := &daemon.ChatEntryDTO{
		Role:       entry.Role,
		Content:    entry.Content,
		ToolName:   entry.ToolName,
		ToolInput:  entry.ToolInput,
		ToolResult: entry.ToolResult,
		Timestamp:  entry.Timestamp.Format(time.RFC3339),
	}
	srv.Broadcast(&daemon.StreamEvent{
		Type:      "manager_chat_entry",
		Project:   projectName,
		ChatEntry: dto,
	})
}

// handlePlannerEvent broadcasts planner events to attached clients.
func (s *Supervisor) handlePlannerEvent(event planner.Event) {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()

	if srv == nil {
		return
	}

	var streamEvent *daemon.StreamEvent

	switch event.Type {
	case planner.EventCreated:
		info := event.Planner.Info()
		streamEvent = &daemon.StreamEvent{
			Type:      "planner_created",
			AgentID:   info.ID,
			Project:   info.Project,
			StartedAt: info.StartedAt.Format(time.RFC3339),
			Backend:   info.Backend,
		}
	case planner.EventStateChanged:
		info := event.Planner.Info()
		streamEvent = &daemon.StreamEvent{
			Type:    "planner_state",
			AgentID: info.ID,
			Project: info.Project,
			State:   string(event.NewState),
		}
	case planner.EventInfoChanged:
		info := event.Planner.Info()
		streamEvent = &daemon.StreamEvent{
			Type:        "planner_info",
			AgentID:     info.ID,
			Project:     info.Project,
			Description: info.Description,
		}
	case planner.EventDeleted:
		info := event.Planner.Info()
		streamEvent = &daemon.StreamEvent{
			Type:    "planner_deleted",
			AgentID: info.ID,
			Project: info.Project,
		}
	}

	if streamEvent != nil {
		srv.Broadcast(streamEvent)
	}
}

// broadcastPlannerChatEntry sends a planner chat entry to attached clients.
func (s *Supervisor) broadcastPlannerChatEntry(plannerID, project string, entry agent.ChatEntry) {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()

	if srv == nil {
		return
	}

	dto := &daemon.ChatEntryDTO{
		Role:       entry.Role,
		Content:    entry.Content,
		ToolName:   entry.ToolName,
		ToolInput:  entry.ToolInput,
		ToolResult: entry.ToolResult,
		Timestamp:  entry.Timestamp.Format(time.RFC3339),
	}
	srv.Broadcast(&daemon.StreamEvent{
		Type:      "planner_chat_entry",
		AgentID:   plannerID,
		Project:   project,
		ChatEntry: dto,
	})
}
