// Package supervisor provides the daemon request handler and orchestration logic.
package supervisor

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/issue"
	"github.com/tessro/fab/internal/issue/gh"
	"github.com/tessro/fab/internal/issue/tk"
	"github.com/tessro/fab/internal/manager"
	"github.com/tessro/fab/internal/orchestrator"
	"github.com/tessro/fab/internal/planner"
	"github.com/tessro/fab/internal/project"
	"github.com/tessro/fab/internal/registry"
	"github.com/tessro/fab/internal/rules"
	"github.com/tessro/fab/internal/usage"
	"github.com/tessro/fab/internal/version"
)

// Version is the supervisor/daemon version.
var Version = version.Version

// Supervisor handles IPC requests and orchestrates agents across projects.
// It implements the daemon.Handler interface.
type Supervisor struct {
	registry    *registry.Registry
	agents      *agent.Manager
	orchConfig  orchestrator.Config
	permissions *daemon.PermissionManager
	questions   *daemon.UserQuestionManager
	startedAt   time.Time

	// +checklocks:mu
	orchestrators map[string]*orchestrator.Orchestrator // project name -> orchestrator

	// Manager allowed patterns loaded from global permissions
	managerPatterns []string

	// Per-project manager agents (project name -> manager)
	// +checklocks:mu
	managers map[string]*manager.Manager

	// Planner agents for implementation planning
	// +checklocks:mu
	planners *planner.Manager

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
	// Load manager allowed patterns from global permissions.toml
	managerPatterns := loadManagerPatterns()

	s := &Supervisor{
		registry:        reg,
		agents:          agents,
		orchestrators:   make(map[string]*orchestrator.Orchestrator),
		orchConfig:      orchestrator.DefaultConfig(),
		permissions:     daemon.NewPermissionManager(PermissionTimeout),
		questions:       daemon.NewUserQuestionManager(PermissionTimeout),
		startedAt:       time.Now(),
		shutdownCh:      make(chan struct{}),
		managerPatterns: managerPatterns,
		managers:        make(map[string]*manager.Manager),
		planners:        planner.NewManager(),
	}

	// Set up callback to start agent read loops when agent starts
	s.orchConfig.OnAgentStarted = func(a *agent.Agent) {
		// Log but don't fail - agent is still usable without broadcasting
		_ = s.StartAgentReadLoop(a)
	}

	// Register event handler to broadcast agent events
	agents.OnEvent(s.handleAgentEvent)

	// Set up planner event handlers
	s.planners.OnEvent(s.handlePlannerEvent)

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
	case daemon.MsgProjectConfigShow:
		return s.handleProjectConfigShow(ctx, req)
	case daemon.MsgProjectConfigGet:
		return s.handleProjectConfigGet(ctx, req)
	case daemon.MsgProjectConfigSet:
		return s.handleProjectConfigSet(ctx, req)

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
	case daemon.MsgAgentDescribe:
		return s.handleAgentDescribe(ctx, req)

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

	// User question handling (AskUserQuestion tool)
	case daemon.MsgUserQuestionRequest:
		return s.handleUserQuestionRequest(ctx, req)
	case daemon.MsgUserQuestionRespond:
		return s.handleUserQuestionRespond(ctx, req)

	// Ticket claims
	case daemon.MsgAgentClaim:
		return s.handleAgentClaim(ctx, req)
	case daemon.MsgClaimList:
		return s.handleClaimList(ctx, req)

	// Commit tracking
	case daemon.MsgCommitList:
		return s.handleCommitList(ctx, req)

	// Stats
	case daemon.MsgStats:
		return s.handleStats(ctx, req)

	// Manager agent
	case daemon.MsgManagerStart:
		return s.handleManagerStart(ctx, req)
	case daemon.MsgManagerStop:
		return s.handleManagerStop(ctx, req)
	case daemon.MsgManagerStatus:
		return s.handleManagerStatus(ctx, req)
	case daemon.MsgManagerSendMessage:
		return s.handleManagerSendMessage(ctx, req)
	case daemon.MsgManagerChatHistory:
		return s.handleManagerChatHistory(ctx, req)
	case daemon.MsgManagerClearHistory:
		return s.handleManagerClearHistory(ctx, req)

	// Planning agents
	case daemon.MsgPlanStart:
		return s.handlePlanStart(ctx, req)
	case daemon.MsgPlanStop:
		return s.handlePlanStop(ctx, req)
	case daemon.MsgPlanList:
		return s.handlePlanList(ctx, req)
	case daemon.MsgPlanSendMessage:
		return s.handlePlanSendMessage(ctx, req)
	case daemon.MsgPlanChatHistory:
		return s.handlePlanChatHistory(ctx, req)

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
	proj, err := s.registry.Add(addReq.RemoteURL, addReq.Name, addReq.MaxAgents, addReq.Autostart)
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

	// Worktrees are created on-demand when agents start

	return successResponse(req, daemon.ProjectAddResponse{
		Name:      proj.Name,
		RemoteURL: proj.RemoteURL,
		RepoDir:   proj.RepoDir(),
		MaxAgents: proj.MaxAgents,
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
			_ = proj.DeleteAllWorktrees()
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
// Deprecated: Use handleProjectConfigSet instead.
func (s *Supervisor) handleProjectSet(ctx context.Context, req *daemon.Request) *daemon.Response {
	var setReq daemon.ProjectSetRequest
	if err := unmarshalPayload(req.Payload, &setReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if setReq.Name == "" {
		return errorResponse(req, "project name required")
	}

	// Update the project settings
	if err := s.registry.Update(setReq.Name, setReq.MaxAgents, setReq.Autostart); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to update project: %v", err))
	}

	// No need to resize worktree pool - worktrees are created/deleted on-demand

	return successResponse(req, nil)
}

// handleProjectConfigShow returns all config for a project.
func (s *Supervisor) handleProjectConfigShow(ctx context.Context, req *daemon.Request) *daemon.Response {
	var showReq daemon.ProjectConfigShowRequest
	if err := unmarshalPayload(req.Payload, &showReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if showReq.Name == "" {
		return errorResponse(req, "project name required")
	}

	config, err := s.registry.GetConfig(showReq.Name)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("failed to get config: %v", err))
	}

	return successResponse(req, daemon.ProjectConfigShowResponse{
		Name:   showReq.Name,
		Config: config,
	})
}

// handleProjectConfigGet returns a single config value for a project.
func (s *Supervisor) handleProjectConfigGet(ctx context.Context, req *daemon.Request) *daemon.Response {
	var getReq daemon.ProjectConfigGetRequest
	if err := unmarshalPayload(req.Payload, &getReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if getReq.Name == "" {
		return errorResponse(req, "project name required")
	}
	if getReq.Key == "" {
		return errorResponse(req, "config key required")
	}

	if !registry.IsValidConfigKey(getReq.Key) {
		return errorResponse(req, fmt.Sprintf("invalid config key: %s (valid keys: max-agents, autostart, issue-backend)", getReq.Key))
	}

	value, err := s.registry.GetConfigValue(getReq.Name, registry.ConfigKey(getReq.Key))
	if err != nil {
		return errorResponse(req, fmt.Sprintf("failed to get config value: %v", err))
	}

	return successResponse(req, daemon.ProjectConfigGetResponse{
		Name:  getReq.Name,
		Key:   getReq.Key,
		Value: value,
	})
}

// handleProjectConfigSet sets a single config value for a project.
func (s *Supervisor) handleProjectConfigSet(ctx context.Context, req *daemon.Request) *daemon.Response {
	var setReq daemon.ProjectConfigSetRequest
	if err := unmarshalPayload(req.Payload, &setReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if setReq.Name == "" {
		return errorResponse(req, "project name required")
	}
	if setReq.Key == "" {
		return errorResponse(req, "config key required")
	}

	if !registry.IsValidConfigKey(setReq.Key) {
		return errorResponse(req, fmt.Sprintf("invalid config key: %s (valid keys: max-agents, autostart, issue-backend)", setReq.Key))
	}

	if err := s.registry.SetConfigValue(setReq.Name, registry.ConfigKey(setReq.Key), setReq.Value); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to set config value: %v", err))
	}

	return successResponse(req, nil)
}

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

// handleActionQueued broadcasts action_queued events to attached clients.
func (s *Supervisor) handleActionQueued(project string, action orchestrator.StagedAction) {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()

	if srv == nil {
		return
	}

	srv.Broadcast(&daemon.StreamEvent{
		Type:    "action_queued",
		AgentID: action.AgentID,
		Project: project,
		StagedAction: &daemon.StagedAction{
			ID:        action.ID,
			AgentID:   action.AgentID,
			Project:   project,
			Type:      daemon.ActionType(action.Type),
			Payload:   action.Payload,
			CreatedAt: action.CreatedAt,
		},
	})
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

	// Worktrees are created on-demand when agents start

	// Register project with agent manager
	s.agents.RegisterProject(proj)

	// Configure orchestrator with issue backend factory for auto-spawning
	cfg := s.orchConfig
	cfg.IssueBackendFactory = issueBackendFactoryForProject(proj)

	// Create orchestrator
	orch := orchestrator.New(proj, s.agents, cfg)
	s.orchestrators[proj.Name] = orch

	// Register callback for action queue events
	orch.Actions().OnAdded(func(action orchestrator.StagedAction) {
		s.handleActionQueued(proj.Name, action)
	})

	// Mark project as running
	proj.SetRunning(true)

	// Start the orchestrator
	return orch.Start()
}

// issueBackendFactoryForProject creates an issue backend factory based on project config.
func issueBackendFactoryForProject(proj *project.Project) issue.NewBackendFunc {
	backendType := proj.IssueBackend
	if backendType == "" {
		backendType = "tk" // Default to tk backend
	}

	return func(repoDir string) (issue.Backend, error) {
		switch backendType {
		case "tk":
			return tk.New(repoDir)
		case "github", "gh":
			return gh.New(repoDir, proj.AllowedAuthors)
		default:
			return nil, fmt.Errorf("unknown issue backend: %s", backendType)
		}
	}
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

// StartAutostart starts orchestration for all projects with autostart=true.
// This should be called once during daemon startup.
func (s *Supervisor) StartAutostart() {
	ctx := context.Background()
	for _, proj := range s.registry.List() {
		if proj.Autostart {
			slog.Info("autostarting project", "project", proj.Name)
			if err := s.startOrchestrator(ctx, proj); err != nil {
				slog.Error("failed to autostart project",
					"project", proj.Name,
					"error", err)
			}
		}
	}
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

// handleUserQuestionRequest handles a user question request from the hook command.
// This blocks until a TUI client responds via question.respond.
func (s *Supervisor) handleUserQuestionRequest(_ context.Context, req *daemon.Request) *daemon.Response {
	var questionReq daemon.UserQuestionRequestPayload
	if err := unmarshalPayload(req.Payload, &questionReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if len(questionReq.Questions) == 0 {
		return errorResponse(req, "questions are required")
	}

	// Find the project for this agent
	var project string
	if questionReq.AgentID != "" {
		if a, err := s.agents.Get(questionReq.AgentID); err == nil {
			project = a.Info().Project
		}
	}

	slog.Info("user question request received",
		"agent", questionReq.AgentID,
		"project", project,
		"question_count", len(questionReq.Questions),
	)

	// Create the user question
	userQuestion := &daemon.UserQuestion{
		AgentID:     questionReq.AgentID,
		Project:     project,
		Questions:   questionReq.Questions,
		RequestedAt: time.Now(),
	}

	// Add to the question manager and get the response channel
	id, respCh := s.questions.Add(userQuestion)
	userQuestion.ID = id

	// Broadcast the user question to attached TUI clients
	s.broadcastUserQuestion(userQuestion)

	// Block waiting for a response from the TUI
	resp := <-respCh
	if resp == nil {
		slog.Warn("user question request timed out",
			"id", id,
			"agent", questionReq.AgentID,
		)
		// Channel was closed without a response (timeout or cancellation)
		return errorResponse(req, "user question cancelled or timed out")
	}

	slog.Info("user question response sent",
		"id", id,
		"agent", questionReq.AgentID,
		"answers", resp.Answers,
	)

	return successResponse(req, resp)
}

// handleUserQuestionRespond handles a user question response from the TUI.
func (s *Supervisor) handleUserQuestionRespond(_ context.Context, req *daemon.Request) *daemon.Response {
	var respPayload daemon.UserQuestionRespondPayload
	if err := unmarshalPayload(req.Payload, &respPayload); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if respPayload.ID == "" {
		return errorResponse(req, "question request ID required")
	}

	// Get the original question for logging
	origQuestion := s.questions.Get(respPayload.ID)
	if origQuestion != nil {
		slog.Info("user question response from TUI",
			"id", respPayload.ID,
			"agent", origQuestion.AgentID,
			"answers", respPayload.Answers,
		)
	} else {
		slog.Info("user question response from TUI",
			"id", respPayload.ID,
			"answers", respPayload.Answers,
		)
	}

	resp := &daemon.UserQuestionResponse{
		ID:      respPayload.ID,
		Answers: respPayload.Answers,
	}

	if err := s.questions.Respond(respPayload.ID, resp); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to respond: %v", err))
	}

	return successResponse(req, nil)
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

// handleStats returns aggregated session statistics.
func (s *Supervisor) handleStats(_ context.Context, req *daemon.Request) *daemon.Response {
	var statsReq daemon.StatsRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &statsReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
		}
	}

	// Count commits across projects
	commitCount := 0
	s.mu.RLock()
	for name, orch := range s.orchestrators {
		if statsReq.Project != "" && statsReq.Project != name {
			continue
		}
		commitCount += orch.Commits().Len()
	}
	s.mu.RUnlock()

	// Get current billing window usage
	window, err := usage.GetCurrentBillingWindowWithUsage()
	if err != nil {
		slog.Debug("failed to get usage stats", "error", err)
		// Return response with zero usage on error
		return successResponse(req, daemon.StatsResponse{
			CommitCount: commitCount,
			Usage: daemon.UsageStats{
				Plan: "pro",
			},
		})
	}

	// Use Pro limits by default (can be made configurable later)
	limits := usage.DefaultProLimits()
	percent := window.Usage.PercentInt(limits)
	timeLeft := window.TimeRemaining()

	return successResponse(req, daemon.StatsResponse{
		CommitCount: commitCount,
		Usage: daemon.UsageStats{
			OutputTokens: window.Usage.OutputTokens,
			Percent:      percent,
			WindowEnd:    window.End.Format(time.RFC3339),
			TimeLeft:     formatDuration(timeLeft),
			PlanLimit:    limits.OutputTokens,
			Plan:         "pro",
		},
	})
}

// formatDuration formats a duration as a human-readable string (e.g., "2h 15m").
func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

// handleCommitList returns recent commits across projects.
func (s *Supervisor) handleCommitList(_ context.Context, req *daemon.Request) *daemon.Response {
	var listReq daemon.CommitListRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &listReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
		}
	}

	var commits []daemon.CommitInfo

	s.mu.RLock()
	for name, orch := range s.orchestrators {
		if listReq.Project != "" && listReq.Project != name {
			continue
		}

		var records []orchestrator.CommitRecord
		if listReq.Limit > 0 {
			records = orch.Commits().ListRecent(listReq.Limit)
		} else {
			records = orch.Commits().List()
		}

		for _, r := range records {
			commits = append(commits, daemon.CommitInfo{
				SHA:      r.SHA,
				Branch:   r.Branch,
				AgentID:  r.AgentID,
				TaskID:   r.TaskID,
				Project:  name,
				MergedAt: r.MergedAt.Format(time.RFC3339),
			})
		}
	}
	s.mu.RUnlock()

	return successResponse(req, daemon.CommitListResponse{
		Commits: commits,
	})
}

// getProjectManager returns the manager for a project, creating it if necessary.
// It also creates the manager worktree if it doesn't exist.
func (s *Supervisor) getProjectManager(projectName string) (*manager.Manager, error) {
	proj, err := s.registry.Get(projectName)
	if err != nil {
		return nil, fmt.Errorf("project not found: %s", projectName)
	}

	// Check if we already have a manager for this project
	s.mu.Lock()
	mgr, ok := s.managers[projectName]
	if ok {
		s.mu.Unlock()
		return mgr, nil
	}

	// Ensure manager worktree exists
	if err := proj.CreateManagerWorktree(); err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("create manager worktree: %w", err)
	}

	// Create new manager for this project
	wtPath := proj.ManagerWorktreePath()
	mgr = manager.New(wtPath, projectName, s.managerPatterns)
	s.managers[projectName] = mgr
	s.mu.Unlock()

	return mgr, nil
}

// setupManagerCallbacks sets up callbacks for manager events to broadcast to TUI clients.
func (s *Supervisor) setupManagerCallbacks(mgr *manager.Manager) {
	projectName := mgr.Project()

	mgr.OnStateChange(func(old, new manager.State) {
		s.broadcastManagerState(projectName, new, mgr.StartedAt())
	})
	mgr.OnEntry(func(entry agent.ChatEntry) {
		s.broadcastManagerChatEntry(projectName, entry)
	})
}

// handleManagerStart starts the manager agent for a project.
func (s *Supervisor) handleManagerStart(_ context.Context, req *daemon.Request) *daemon.Response {
	var startReq daemon.ManagerStartRequest
	if err := unmarshalPayload(req.Payload, &startReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if startReq.Project == "" {
		return errorResponse(req, "project is required")
	}

	mgr, err := s.getProjectManager(startReq.Project)
	if err != nil {
		return errorResponse(req, err.Error())
	}

	// Set up callbacks before starting
	s.setupManagerCallbacks(mgr)

	if err := mgr.Start(); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to start manager: %v", err))
	}

	slog.Info("manager agent started", "project", startReq.Project)
	return successResponse(req, nil)
}

// handleManagerStop stops the manager agent for a project.
func (s *Supervisor) handleManagerStop(_ context.Context, req *daemon.Request) *daemon.Response {
	var stopReq daemon.ManagerStopRequest
	if err := unmarshalPayload(req.Payload, &stopReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if stopReq.Project == "" {
		return errorResponse(req, "project is required")
	}

	s.mu.RLock()
	mgr, ok := s.managers[stopReq.Project]
	s.mu.RUnlock()

	if !ok {
		return errorResponse(req, fmt.Sprintf("no manager running for project: %s", stopReq.Project))
	}

	if err := mgr.Stop(); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to stop manager: %v", err))
	}

	slog.Info("manager agent stopped", "project", stopReq.Project)
	return successResponse(req, nil)
}

// handleManagerStatus returns the manager agent status for a project.
func (s *Supervisor) handleManagerStatus(_ context.Context, req *daemon.Request) *daemon.Response {
	var statusReq daemon.ManagerStatusRequest
	if err := unmarshalPayload(req.Payload, &statusReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if statusReq.Project == "" {
		return errorResponse(req, "project is required")
	}

	proj, err := s.registry.Get(statusReq.Project)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("project not found: %s", statusReq.Project))
	}

	s.mu.RLock()
	mgr, ok := s.managers[statusReq.Project]
	s.mu.RUnlock()

	if !ok {
		// No manager exists yet - return stopped status
		return successResponse(req, daemon.ManagerStatusResponse{
			Project:   statusReq.Project,
			Running:   false,
			State:     string(manager.StateStopped),
			StartedAt: "",
			WorkDir:   proj.ManagerWorktreePath(),
		})
	}

	startedAt := ""
	if mgr.IsRunning() {
		startedAt = mgr.StartedAt().Format(time.RFC3339)
	}

	return successResponse(req, daemon.ManagerStatusResponse{
		Project:   statusReq.Project,
		Running:   mgr.IsRunning(),
		State:     string(mgr.State()),
		StartedAt: startedAt,
		WorkDir:   mgr.WorkDir(),
	})
}

// handleManagerSendMessage sends a message to the manager agent for a project.
func (s *Supervisor) handleManagerSendMessage(_ context.Context, req *daemon.Request) *daemon.Response {
	var sendReq daemon.ManagerSendMessageRequest
	if err := unmarshalPayload(req.Payload, &sendReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if sendReq.Project == "" {
		return errorResponse(req, "project is required")
	}

	s.mu.RLock()
	mgr, ok := s.managers[sendReq.Project]
	s.mu.RUnlock()

	if !ok {
		return errorResponse(req, fmt.Sprintf("no manager running for project: %s", sendReq.Project))
	}

	if err := mgr.SendMessage(sendReq.Content); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to send message: %v", err))
	}

	return successResponse(req, nil)
}

// handleManagerChatHistory returns the manager chat history for a project.
func (s *Supervisor) handleManagerChatHistory(_ context.Context, req *daemon.Request) *daemon.Response {
	var histReq daemon.ManagerChatHistoryRequest
	if err := unmarshalPayload(req.Payload, &histReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if histReq.Project == "" {
		return errorResponse(req, "project is required")
	}

	s.mu.RLock()
	mgr, ok := s.managers[histReq.Project]
	s.mu.RUnlock()

	if !ok {
		// No manager exists - return empty history
		return successResponse(req, daemon.ManagerChatHistoryResponse{
			Project: histReq.Project,
			Entries: []daemon.ChatEntryDTO{},
		})
	}

	entries := mgr.History().Entries(histReq.Limit)

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

	return successResponse(req, daemon.ManagerChatHistoryResponse{
		Project: histReq.Project,
		Entries: dtos,
	})
}

// handleManagerClearHistory clears the manager chat history for a project.
func (s *Supervisor) handleManagerClearHistory(_ context.Context, req *daemon.Request) *daemon.Response {
	var clearReq daemon.ManagerClearHistoryRequest
	if err := unmarshalPayload(req.Payload, &clearReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if clearReq.Project == "" {
		return errorResponse(req, "project is required")
	}

	s.mu.RLock()
	mgr, ok := s.managers[clearReq.Project]
	s.mu.RUnlock()

	if !ok {
		return errorResponse(req, fmt.Sprintf("no manager running for project: %s", clearReq.Project))
	}

	mgr.History().Clear()

	slog.Info("manager chat history cleared", "project", clearReq.Project)
	return successResponse(req, nil)
}

// broadcastManagerState sends a manager state change to attached clients.
func (s *Supervisor) broadcastManagerState(projectName string, state manager.State, startedAt time.Time) {
	s.mu.RLock()
	srv := s.server
	s.mu.RUnlock()

	if srv == nil {
		return
	}

	event := &daemon.StreamEvent{
		Type:         "manager_state",
		Project:      projectName,
		ManagerState: string(state),
	}

	// Include StartedAt when manager starts so TUI can add it to the agent list
	if state == manager.StateStarting {
		event.StartedAt = startedAt.Format(time.RFC3339)
	}

	srv.Broadcast(event)
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

// loadManagerPatterns loads manager allowed patterns from the global permissions.toml.
// Returns default patterns if the file doesn't exist or has no manager section.
func loadManagerPatterns() []string {
	path, err := rules.GlobalConfigPath()
	if err != nil {
		slog.Debug("failed to get permissions config path", "error", err)
		return rules.DefaultManagerAllowedPatterns
	}

	cfg, err := rules.LoadConfig(path)
	if err != nil {
		slog.Warn("failed to load permissions config", "path", path, "error", err)
		return rules.DefaultManagerAllowedPatterns
	}

	return cfg.ManagerAllowedPatterns()
}

// handlePlanStart starts a planning agent.
func (s *Supervisor) handlePlanStart(_ context.Context, req *daemon.Request) *daemon.Response {
	var startReq daemon.PlanStartRequest
	if err := unmarshalPayload(req.Payload, &startReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if startReq.Prompt == "" {
		return errorResponse(req, "prompt is required")
	}

	// Determine working directory
	var workDir string
	var projectName string

	if startReq.Project != "" {
		// Use project worktree
		proj, err := s.registry.Get(startReq.Project)
		if err != nil {
			return errorResponse(req, fmt.Sprintf("project not found: %s", startReq.Project))
		}

		// Planners use the main repo directory (not a worktree)
		// since they're just reading code, not making changes
		workDir = proj.RepoDir()
		projectName = proj.Name
	} else {
		// Use default planner directory
		home, _ := os.UserHomeDir()
		workDir = filepath.Join(home, ".fab", "planners")
	}

	// Create the planner
	p, err := s.planners.Create(projectName, workDir, startReq.Prompt)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("failed to create planner: %v", err))
	}

	// Set up entry callback for broadcasting
	p.OnEntry(func(entry agent.ChatEntry) {
		s.broadcastPlannerChatEntry(p.ID(), projectName, entry)
	})

	// Start the planner
	if err := p.Start(); err != nil {
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
		}
	case planner.EventStateChanged:
		info := event.Planner.Info()
		streamEvent = &daemon.StreamEvent{
			Type:    "planner_state",
			AgentID: info.ID,
			Project: info.Project,
			State:   string(event.NewState),
		}
	case planner.EventPlanComplete:
		info := event.Planner.Info()
		streamEvent = &daemon.StreamEvent{
			Type:    "plan_complete",
			AgentID: info.ID,
			Project: info.Project,
			Data:    event.PlanFile,
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
