// Package supervisor provides the daemon request handler and orchestration logic.
package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/orchestrator"
	"github.com/tessro/fab/internal/project"
	"github.com/tessro/fab/internal/registry"
)

// Version is the supervisor/daemon version.
const Version = "0.1.0"

// Supervisor handles IPC requests and orchestrates agents across projects.
// It implements the daemon.Handler interface.
type Supervisor struct {
	registry    *registry.Registry
	agents      *agent.Manager
	orchConfig  orchestrator.Config
	permissions *daemon.PermissionManager
	startedAt   time.Time

	// +checklocks:mu
	orchestrators map[string]*orchestrator.Orchestrator // project name -> orchestrator

	shutdownCh chan struct{} // Created at init, closed to signal shutdown
	shutdownMu sync.Mutex    // Protects closing shutdownCh exactly once

	// +checklocks:mu
	server *daemon.Server // Server reference for broadcasting output events

	mu sync.RWMutex
}

// PermissionTimeout is the default timeout for permission requests.
const PermissionTimeout = 5 * time.Minute

// New creates a new Supervisor with the given registry and agent manager.
func New(reg *registry.Registry, agents *agent.Manager) *Supervisor {
	s := &Supervisor{
		registry:      reg,
		agents:        agents,
		orchestrators: make(map[string]*orchestrator.Orchestrator),
		orchConfig:    orchestrator.DefaultConfig(),
		permissions:   daemon.NewPermissionManager(PermissionTimeout),
		startedAt:     time.Now(),
		shutdownCh:    make(chan struct{}),
	}

	// Set up callback to start agent read loops when agent starts
	s.orchConfig.OnAgentStarted = func(a *agent.Agent) {
		// Log but don't fail - agent is still usable without broadcasting
		_ = s.StartAgentReadLoop(a)
	}

	// Register event handler to broadcast agent events
	agents.OnEvent(s.handleAgentEvent)

	return s
}

// Handle processes IPC requests and returns responses.
// Implements daemon.Handler.
func (s *Supervisor) Handle(ctx context.Context, req *daemon.Request) *daemon.Response {
	slog.Debug("supervisor handling request", "type", req.Type)
	switch req.Type {
	// Server management
	case daemon.MsgPing:
		return s.handlePing(ctx, req)
	case daemon.MsgShutdown:
		return s.handleShutdown(ctx, req)

	// Supervisor control
	case daemon.MsgStart:
		return s.handleStart(ctx, req)
	case daemon.MsgStop:
		return s.handleStop(ctx, req)
	case daemon.MsgStatus:
		return s.handleStatus(ctx, req)

	// Project management
	case daemon.MsgProjectAdd:
		return s.handleProjectAdd(ctx, req)
	case daemon.MsgProjectRemove:
		return s.handleProjectRemove(ctx, req)
	case daemon.MsgProjectList:
		return s.handleProjectList(ctx, req)
	case daemon.MsgProjectSet:
		return s.handleProjectSet(ctx, req)

	// Agent management
	case daemon.MsgAgentList:
		return s.handleAgentList(ctx, req)
	case daemon.MsgAgentCreate:
		return s.handleAgentCreate(ctx, req)
	case daemon.MsgAgentDelete:
		return s.handleAgentDelete(ctx, req)
	case daemon.MsgAgentAbort:
		return s.handleAgentAbort(ctx, req)
	case daemon.MsgAgentInput:
		return s.handleAgentInput(ctx, req)
	case daemon.MsgAgentOutput:
		return s.handleAgentOutput(ctx, req)
	case daemon.MsgAgentSendMessage:
		return s.handleAgentSendMessage(ctx, req)
	case daemon.MsgAgentChatHistory:
		return s.handleAgentChatHistory(ctx, req)

	// TUI streaming
	case daemon.MsgAttach:
		return s.handleAttach(ctx, req)
	case daemon.MsgDetach:
		return s.handleDetach(ctx, req)

	// Orchestrator
	case daemon.MsgAgentDone:
		return s.handleAgentDone(ctx, req)
	case daemon.MsgListStagedActions:
		return s.handleListStagedActions(ctx, req)
	case daemon.MsgApproveAction:
		return s.handleApproveAction(ctx, req)
	case daemon.MsgRejectAction:
		return s.handleRejectAction(ctx, req)

	// Permission handling
	case daemon.MsgPermissionRequest:
		return s.handlePermissionRequest(ctx, req)
	case daemon.MsgPermissionRespond:
		return s.handlePermissionRespond(ctx, req)
	case daemon.MsgPermissionList:
		return s.handlePermissionList(ctx, req)

	// Ticket claims
	case daemon.MsgAgentClaim:
		return s.handleAgentClaim(ctx, req)
	case daemon.MsgClaimList:
		return s.handleClaimList(ctx, req)

	default:
		return errorResponse(req, fmt.Sprintf("unknown message type: %s", req.Type))
	}
}

// ShutdownCh returns a channel that is closed when shutdown is requested.
func (s *Supervisor) ShutdownCh() <-chan struct{} {
	return s.shutdownCh
}

// StartedAt returns when the supervisor was started.
func (s *Supervisor) StartedAt() time.Time {
	return s.startedAt
}

// handlePing responds to ping requests.
func (s *Supervisor) handlePing(ctx context.Context, req *daemon.Request) *daemon.Response {
	uptime := time.Since(s.startedAt)
	return successResponse(req, daemon.PingResponse{
		Version:   Version,
		Uptime:    uptime.Round(time.Second).String(),
		StartedAt: s.startedAt,
	})
}

// handleShutdown initiates daemon shutdown.
func (s *Supervisor) handleShutdown(ctx context.Context, req *daemon.Request) *daemon.Response {
	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()

	select {
	case <-s.shutdownCh:
		// Already shutting down
	default:
		close(s.shutdownCh)
	}

	return successResponse(req, nil)
}

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
				ID:        info.ID,
				Project:   info.Project,
				State:     string(info.State),
				Worktree:  info.Worktree,
				StartedAt: info.StartedAt,
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

// handleProjectAdd adds a new project.
func (s *Supervisor) handleProjectAdd(ctx context.Context, req *daemon.Request) *daemon.Response {
	var addReq daemon.ProjectAddRequest
	if err := unmarshalPayload(req.Payload, &addReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if addReq.RemoteURL == "" {
		return errorResponse(req, "remote URL required")
	}

	// Register project in config first (validates and generates name)
	proj, err := s.registry.Add(addReq.RemoteURL, addReq.Name, addReq.MaxAgents)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("failed to add project: %v", err))
	}

	// Create project directory structure
	projectDir := proj.ProjectDir()
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		_ = s.registry.Remove(proj.Name)
		return errorResponse(req, fmt.Sprintf("failed to create project dir: %v", err))
	}

	// Clone the repository
	repoDir := proj.RepoDir()
	cmd := exec.Command("git", "clone", addReq.RemoteURL, repoDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		_ = s.registry.Remove(proj.Name)
		_ = os.RemoveAll(projectDir)
		return errorResponse(req, fmt.Sprintf("failed to clone: %v\n%s", err, output))
	}

	// Create worktree pool
	worktrees, err := proj.CreateWorktreePool()
	if err != nil {
		_ = s.registry.Remove(proj.Name)
		_ = os.RemoveAll(projectDir)
		return errorResponse(req, fmt.Sprintf("failed to create worktree pool: %v", err))
	}

	return successResponse(req, daemon.ProjectAddResponse{
		Name:      proj.Name,
		RemoteURL: proj.RemoteURL,
		RepoDir:   proj.RepoDir(),
		MaxAgents: proj.MaxAgents,
		Worktrees: worktrees,
	})
}

// handleProjectRemove removes a project.
func (s *Supervisor) handleProjectRemove(ctx context.Context, req *daemon.Request) *daemon.Response {
	var removeReq daemon.ProjectRemoveRequest
	if err := unmarshalPayload(req.Payload, &removeReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if removeReq.Name == "" {
		return errorResponse(req, "project name required")
	}

	// Stop all agents first
	s.agents.DeleteAll(removeReq.Name)
	s.agents.UnregisterProject(removeReq.Name)

	// Delete worktrees if requested
	if removeReq.DeleteWorktrees {
		proj, err := s.registry.Get(removeReq.Name)
		if err == nil {
			_ = proj.DeleteWorktreePool()
		}
	}

	if err := s.registry.Remove(removeReq.Name); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to remove project: %v", err))
	}

	return successResponse(req, nil)
}

// handleProjectList lists all projects.
func (s *Supervisor) handleProjectList(ctx context.Context, req *daemon.Request) *daemon.Response {
	projects := s.registry.List()
	infos := make([]daemon.ProjectInfo, 0, len(projects))

	for _, p := range projects {
		infos = append(infos, daemon.ProjectInfo{
			Name:      p.Name,
			RemoteURL: p.RemoteURL,
			MaxAgents: p.MaxAgents,
			Running:   p.IsRunning(),
		})
	}

	return successResponse(req, daemon.ProjectListResponse{
		Projects: infos,
	})
}

// handleProjectSet updates project settings.
func (s *Supervisor) handleProjectSet(ctx context.Context, req *daemon.Request) *daemon.Response {
	var setReq daemon.ProjectSetRequest
	if err := unmarshalPayload(req.Payload, &setReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if setReq.Name == "" {
		return errorResponse(req, "project name required")
	}

	if err := s.registry.Update(setReq.Name, setReq.MaxAgents); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to update project: %v", err))
	}

	return successResponse(req, nil)
}

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
	statuses := make([]daemon.AgentStatus, 0, len(agents))

	for _, a := range agents {
		info := a.Info()
		statuses = append(statuses, daemon.AgentStatus{
			ID:        info.ID,
			Project:   info.Project,
			State:     string(info.State),
			Worktree:  info.Worktree,
			StartedAt: info.StartedAt,
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

// handleAgentOutput returns buffered PTY output for an agent.
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

	if err := a.SendMessage(sendReq.Content); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to send message: %v", err))
	}

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

// handleAttach subscribes a client to streaming events.
func (s *Supervisor) handleAttach(ctx context.Context, req *daemon.Request) *daemon.Response {
	var attachReq daemon.AttachRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &attachReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
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

// StartAgentReadLoop starts the read loop for an agent.
// This should be called after the agent's process is started.
func (s *Supervisor) StartAgentReadLoop(a *agent.Agent) error {
	info := a.Info()

	cfg := agent.DefaultReadLoopConfig()
	cfg.OnEntry = func(entry agent.ChatEntry) {
		s.broadcastChatEntry(info.ID, info.Project, entry)
	}
	cfg.OnExit = func(exitErr error) {
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

// successResponse creates a successful response.
func successResponse(req *daemon.Request, payload any) *daemon.Response {
	return &daemon.Response{
		Type:    req.Type,
		ID:      req.ID,
		Success: true,
		Payload: payload,
	}
}

// errorResponse creates an error response.
func errorResponse(req *daemon.Request, msg string) *daemon.Response {
	return &daemon.Response{
		Type:    req.Type,
		ID:      req.ID,
		Success: false,
		Error:   msg,
	}
}

// unmarshalPayload converts an any payload to a specific type.
func unmarshalPayload(payload any, dst any) error {
	if payload == nil {
		return nil
	}

	// If payload is already the right type, use it directly
	if m, ok := payload.(map[string]any); ok {
		// Re-marshal and unmarshal to convert
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, dst)
	}

	// Try direct type assertion
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// startOrchestrator creates and starts an orchestrator for the given project.
func (s *Supervisor) startOrchestrator(_ context.Context, proj *project.Project) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Check if already running
	if orch, ok := s.orchestrators[proj.Name]; ok && orch.IsRunning() {
		return nil
	}

	// Ensure worktree pool is populated (for projects loaded from config)
	if err := proj.RestoreWorktreePool(); err != nil {
		return fmt.Errorf("restore worktree pool: %w", err)
	}

	// Register project with agent manager
	s.agents.RegisterProject(proj)

	// Create orchestrator
	orch := orchestrator.New(proj, s.agents, s.orchConfig)
	s.orchestrators[proj.Name] = orch

	// Mark project as running
	proj.SetRunning(true)

	// Start the orchestrator
	return orch.Start()
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

// handleAgentDone handles agent completion signals.
func (s *Supervisor) handleAgentDone(ctx context.Context, req *daemon.Request) *daemon.Response {
	var doneReq daemon.AgentDoneRequest
	if err := unmarshalPayload(req.Payload, &doneReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if doneReq.AgentID == "" {
		return errorResponse(req, "agent_id is required")
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

// handlePermissionRequest handles a permission request from the hook command.
// This blocks until a TUI client responds via permission.respond.
func (s *Supervisor) handlePermissionRequest(_ context.Context, req *daemon.Request) *daemon.Response {
	var permReq daemon.PermissionRequestPayload
	if err := unmarshalPayload(req.Payload, &permReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if permReq.ToolName == "" {
		return errorResponse(req, "tool_name is required")
	}

	// Find the project for this agent
	var project string
	if permReq.AgentID != "" {
		if a, err := s.agents.Get(permReq.AgentID); err == nil {
			project = a.Info().Project
		}
	}

	slog.Info("permission request received",
		"agent", permReq.AgentID,
		"project", project,
		"tool", permReq.ToolName,
		"input", string(permReq.ToolInput),
	)

	// Create the permission request
	permissionReq := &daemon.PermissionRequest{
		AgentID:     permReq.AgentID,
		Project:     project,
		ToolName:    permReq.ToolName,
		ToolInput:   permReq.ToolInput,
		ToolUseID:   permReq.ToolUseID,
		RequestedAt: time.Now(),
	}

	// Add to the permission manager and get the response channel
	id, respCh := s.permissions.Add(permissionReq)
	permissionReq.ID = id

	// Broadcast the permission request to attached TUI clients
	s.broadcastPermissionRequest(permissionReq)

	// Block waiting for a response from the TUI
	resp := <-respCh
	if resp == nil {
		slog.Warn("permission request timed out",
			"id", id,
			"agent", permReq.AgentID,
			"tool", permReq.ToolName,
		)
		// Channel was closed without a response (timeout or cancellation)
		return errorResponse(req, "permission request cancelled or timed out")
	}

	slog.Info("permission response sent",
		"id", id,
		"agent", permReq.AgentID,
		"tool", permReq.ToolName,
		"input", string(permReq.ToolInput),
		"behavior", resp.Behavior,
		"message", resp.Message,
	)

	return successResponse(req, resp)
}

// handlePermissionRespond handles a permission response from the TUI.
func (s *Supervisor) handlePermissionRespond(_ context.Context, req *daemon.Request) *daemon.Response {
	var respPayload daemon.PermissionRespondPayload
	if err := unmarshalPayload(req.Payload, &respPayload); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if respPayload.ID == "" {
		return errorResponse(req, "permission request ID required")
	}

	// Get the original request for logging
	origReq := s.permissions.Get(respPayload.ID)
	if origReq != nil {
		slog.Info("permission response from TUI",
			"id", respPayload.ID,
			"agent", origReq.AgentID,
			"tool", origReq.ToolName,
			"input", string(origReq.ToolInput),
			"behavior", respPayload.Behavior,
			"message", respPayload.Message,
		)
	} else {
		slog.Info("permission response from TUI",
			"id", respPayload.ID,
			"behavior", respPayload.Behavior,
			"message", respPayload.Message,
		)
	}

	resp := &daemon.PermissionResponse{
		ID:        respPayload.ID,
		Behavior:  respPayload.Behavior,
		Message:   respPayload.Message,
		Interrupt: respPayload.Interrupt,
	}

	if err := s.permissions.Respond(respPayload.ID, resp); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to respond: %v", err))
	}

	return successResponse(req, nil)
}

// handlePermissionList returns pending permission requests.
func (s *Supervisor) handlePermissionList(_ context.Context, req *daemon.Request) *daemon.Response {
	var listReq daemon.PermissionListRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &listReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
		}
	}

	var requests []*daemon.PermissionRequest
	if listReq.Project != "" {
		requests = s.permissions.ListForProject(listReq.Project)
	} else {
		requests = s.permissions.List()
	}

	// Convert to slice of values for response
	result := make([]daemon.PermissionRequest, len(requests))
	for i, r := range requests {
		result[i] = *r
	}

	return successResponse(req, daemon.PermissionListResponse{
		Requests: result,
	})
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

// handleAgentClaim handles ticket claim requests from agents.
func (s *Supervisor) handleAgentClaim(_ context.Context, req *daemon.Request) *daemon.Response {
	var claimReq daemon.AgentClaimRequest
	if err := unmarshalPayload(req.Payload, &claimReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if claimReq.AgentID == "" {
		return errorResponse(req, "agent_id is required")
	}
	if claimReq.TicketID == "" {
		return errorResponse(req, "ticket_id is required")
	}

	// Find the agent to get its project
	a, err := s.agents.Get(claimReq.AgentID)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("agent not found: %s", claimReq.AgentID))
	}

	// Get the orchestrator for the agent's project
	orch := s.getOrchestrator(a.Info().Project)
	if orch == nil {
		return errorResponse(req, "orchestrator not running for project")
	}

	// Attempt to claim the ticket
	if err := orch.Claims().Claim(claimReq.TicketID, claimReq.AgentID); err != nil {
		return errorResponse(req, fmt.Sprintf("claim failed: %v", err))
	}

	// Update the agent's task field
	a.SetTask(claimReq.TicketID)

	slog.Info("ticket claimed",
		"ticket", claimReq.TicketID,
		"agent", claimReq.AgentID,
		"project", a.Info().Project,
	)

	return successResponse(req, nil)
}

// handleClaimList returns all active ticket claims.
func (s *Supervisor) handleClaimList(_ context.Context, req *daemon.Request) *daemon.Response {
	var listReq daemon.ClaimListRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &listReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
		}
	}

	var claims []daemon.ClaimInfo

	s.mu.RLock()
	for name, orch := range s.orchestrators {
		if listReq.Project != "" && listReq.Project != name {
			continue
		}

		for ticketID, agentID := range orch.Claims().List() {
			claims = append(claims, daemon.ClaimInfo{
				TicketID: ticketID,
				AgentID:  agentID,
				Project:  name,
			})
		}
	}
	s.mu.RUnlock()

	return successResponse(req, daemon.ClaimListResponse{
		Claims: claims,
	})
}
