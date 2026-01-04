// Package supervisor provides the daemon request handler and orchestration logic.
package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
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
	registry   *registry.Registry
	agents     *agent.Manager
	orchConfig orchestrator.Config
	startedAt  time.Time

	// +checklocks:mu
	orchestrators map[string]*orchestrator.Orchestrator // project name -> orchestrator

	shutdownCh chan struct{} // Created at init, closed to signal shutdown
	shutdownMu sync.Mutex    // Protects closing shutdownCh exactly once

	// +checklocks:mu
	server *daemon.Server // Server reference for broadcasting output events

	mu sync.RWMutex
}

// New creates a new Supervisor with the given registry and agent manager.
func New(reg *registry.Registry, agents *agent.Manager) *Supervisor {
	s := &Supervisor{
		registry:      reg,
		agents:        agents,
		orchestrators: make(map[string]*orchestrator.Orchestrator),
		orchConfig:    orchestrator.DefaultConfig(),
		startedAt:     time.Now(),
		shutdownCh:    make(chan struct{}),
	}

	// Set up callback to start agent read loops when agent starts
	s.orchConfig.OnAgentStarted = func(a *agent.Agent) {
		if err := s.StartAgentReadLoop(a); err != nil {
			// Log but don't fail - agent is still usable without broadcasting
		}
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
	case daemon.MsgAgentInput:
		return s.handleAgentInput(ctx, req)
	case daemon.MsgAgentOutput:
		return s.handleAgentOutput(ctx, req)
	case daemon.MsgAgentSendMessage:
		return s.handleAgentSendMessage(ctx, req)

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
			Path:         p.Path,
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

	if addReq.Path == "" {
		return errorResponse(req, "project path required")
	}

	proj, err := s.registry.Add(addReq.Path, addReq.Name, addReq.MaxAgents)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("failed to add project: %v", err))
	}

	// Create worktree pool
	worktrees, err := proj.CreateWorktreePool()
	if err != nil {
		// Remove the project from registry since worktree creation failed
		_ = s.registry.Remove(proj.Name)
		return errorResponse(req, fmt.Sprintf("failed to create worktree pool: %v", err))
	}

	return successResponse(req, daemon.ProjectAddResponse{
		Name:      proj.Name,
		Path:      proj.Path,
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
			Path:      p.Path,
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

	if conn == nil || srv == nil {
		return errorResponse(req, "internal error: missing connection context")
	}

	srv.Attach(conn, attachReq.Projects)
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
			Type:    "created",
			AgentID: info.ID,
			Project: info.Project,
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

// broadcastOutput sends PTY output to attached clients.
func (s *Supervisor) broadcastOutput(agentID, project string, data []byte) {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()

	if srv == nil {
		return
	}

	srv.Broadcast(&daemon.StreamEvent{
		Type:    "output",
		AgentID: agentID,
		Project: project,
		Data:    string(data),
	})
}

// broadcastChatEntry sends a chat entry to attached clients.
func (s *Supervisor) broadcastChatEntry(agentID, project string, entry agent.ChatEntry) {
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

// Shutdown gracefully stops all orchestrators and agents.
// This should be called during daemon shutdown.
func (s *Supervisor) Shutdown() {
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
	if err := orch.HandleAgentDone(doneReq.AgentID, doneReq.TaskID, doneReq.Error); err != nil {
		return errorResponse(req, fmt.Sprintf("handle agent done: %v", err))
	}

	return successResponse(req, nil)
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
