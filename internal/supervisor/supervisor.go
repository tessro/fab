// Package supervisor provides the daemon request handler and orchestration logic.
package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/registry"
)

// Version is the supervisor/daemon version.
const Version = "0.1.0"

// Supervisor handles IPC requests and orchestrates agents across projects.
// It implements the daemon.Handler interface.
type Supervisor struct {
	registry  *registry.Registry
	agents    *agent.Manager
	startedAt time.Time

	// shutdownCh is closed when shutdown is requested
	shutdownCh chan struct{}
	shutdownMu sync.Mutex

	// Server reference for broadcasting output events
	server *daemon.Server

	mu sync.RWMutex
}

// New creates a new Supervisor with the given registry and agent manager.
func New(reg *registry.Registry, agents *agent.Manager) *Supervisor {
	s := &Supervisor{
		registry:   reg,
		agents:     agents,
		startedAt:  time.Now(),
		shutdownCh: make(chan struct{}),
	}

	// Register event handler to broadcast agent events
	agents.OnEvent(s.handleAgentEvent)

	return s
}

// Handle processes IPC requests and returns responses.
// Implements daemon.Handler.
func (s *Supervisor) Handle(ctx context.Context, req *daemon.Request) *daemon.Response {
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

	// TUI streaming
	case daemon.MsgAttach:
		return s.handleAttach(ctx, req)
	case daemon.MsgDetach:
		return s.handleDetach(ctx, req)

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
			p.SetRunning(true)
			s.agents.RegisterProject(p)
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

	proj.SetRunning(true)
	s.agents.RegisterProject(proj)
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
			s.agents.StopAll(p.Name)
			p.SetRunning(false)
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

	s.agents.StopAll(proj.Name)
	proj.SetRunning(false)
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

	// TODO: Create worktree pool (FAB-20)

	return successResponse(req, daemon.ProjectAddResponse{
		Name:      proj.Name,
		Path:      proj.Path,
		MaxAgents: proj.MaxAgents,
		Worktrees: []string{}, // Populated by FAB-20
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

	// TODO: Delete worktrees if requested (FAB-20)

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

// handleAgentInput sends input to an agent's PTY.
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

// StartAgentReadLoop starts the read loop for an agent.
// This should be called after the agent's PTY is started.
func (s *Supervisor) StartAgentReadLoop(a *agent.Agent) error {
	info := a.Info()

	cfg := agent.DefaultReadLoopConfig()
	cfg.OnOutput = func(data []byte) {
		s.broadcastOutput(info.ID, info.Project, data)
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
