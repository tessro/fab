// Package supervisor provides the daemon request handler and orchestration logic.
package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/config"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/manager"
	"github.com/tessro/fab/internal/orchestrator"
	"github.com/tessro/fab/internal/planner"
	"github.com/tessro/fab/internal/registry"
	"github.com/tessro/fab/internal/runtime"
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

	// Planner agents for implementation planning.
	// Safe for concurrent access via Manager's internal synchronization.
	planners *planner.Manager

	shutdownCh chan struct{} // Created at init, closed to signal shutdown
	shutdownMu sync.Mutex    // Protects closing shutdownCh exactly once
	stopHost   bool          // If true, stop the agent host on shutdown

	// +checklocks:mu
	server *daemon.Server // Server reference for broadcasting output events

	// Global config for LLM auth settings
	globalConfig *config.GlobalConfig

	// Heartbeat monitor for detecting stuck agents
	heartbeat *HeartbeatMonitor

	// runtimeStore persists agent metadata for daemon restart recovery.
	// May be nil if persistence is disabled.
	runtimeStore *runtime.Store

	// Webhook server for receiving issue events
	webhookServer *WebhookServer
	webhookEvents chan *IssueEvent
	dedupStore    *runtime.DedupStore

	mu sync.RWMutex
}

// PermissionTimeout is the default timeout for permission requests.
const PermissionTimeout = 5 * time.Minute

// New creates a new Supervisor with the given registry and agent manager.
func New(reg *registry.Registry, agents *agent.Manager) *Supervisor {
	// Load manager allowed patterns from global permissions.toml
	managerPatterns := loadManagerPatterns()

	// Load global config for LLM auth settings
	globalCfg, err := config.LoadGlobalConfig()
	if err != nil {
		slog.Warn("failed to load global config", "error", err)
	}

	// Initialize runtime store for agent metadata persistence
	runtimeStore, err := runtime.NewStore()
	if err != nil {
		slog.Warn("failed to create runtime store", "error", err)
	}

	// Initialize dedup store for webhook events
	dedupStore, err := runtime.NewDedupStoreDefault()
	if err != nil {
		slog.Warn("failed to create dedup store", "error", err)
	}

	// Create webhook event channel (buffered to prevent blocking)
	webhookEvents := make(chan *IssueEvent, 100)

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
		globalConfig:    globalCfg,
		runtimeStore:    runtimeStore,
		webhookEvents:   webhookEvents,
		dedupStore:      dedupStore,
	}

	// Wire up runtime store to agent and planner managers
	if runtimeStore != nil {
		agents.SetRuntimeStore(runtimeStore)
		s.planners.SetRuntimeStore(runtimeStore)
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

	// Set up heartbeat monitor for detecting stuck agents
	heartbeatCfg := DefaultHeartbeatConfig()
	heartbeatCfg.SendMessage = func(agentID, message string) error {
		a, err := agents.Get(agentID)
		if err != nil {
			return err
		}
		return a.SendMessage(message)
	}
	heartbeatCfg.StopAgent = func(agentID string) error {
		return agents.Stop(agentID)
	}
	s.heartbeat = NewHeartbeatMonitor(agents, heartbeatCfg)
	s.heartbeat.Start()

	// Initialize webhook server if enabled (requires dedup store)
	if globalCfg != nil && globalCfg.GetWebhookEnabled() && dedupStore != nil {
		webhookCfg := WebhookConfig{
			Enabled:    true,
			BindAddr:   globalCfg.GetWebhookBindAddr(),
			Secret:     globalCfg.GetWebhookSecret(),
			PathPrefix: globalCfg.GetWebhookPathPrefix(),
		}
		s.webhookServer = NewWebhookServer(webhookCfg, dedupStore, webhookEvents)
	}

	// Start event processing loop
	go s.processWebhookEvents()

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
	case daemon.MsgAgentIdle:
		return s.handleAgentIdle(ctx, req)

	// TUI streaming
	case daemon.MsgAttach:
		return s.handleAttach(ctx, req)
	case daemon.MsgDetach:
		return s.handleDetach(ctx, req)

	// Orchestrator
	case daemon.MsgAgentDone:
		return s.handleAgentDone(ctx, req)

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

// StopHost returns whether the agent host should be stopped during shutdown.
func (s *Supervisor) StopHost() bool {
	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()
	return s.stopHost
}

// StartWebhookServer starts the webhook HTTP server if configured.
func (s *Supervisor) StartWebhookServer() error {
	if s.webhookServer == nil {
		return nil
	}
	return s.webhookServer.Start()
}

// StopWebhookServer stops the webhook HTTP server.
func (s *Supervisor) StopWebhookServer() error {
	if s.webhookServer == nil {
		return nil
	}
	return s.webhookServer.Stop()
}

// processWebhookEvents handles incoming webhook events.
func (s *Supervisor) processWebhookEvents() {
	for {
		select {
		case <-s.shutdownCh:
			return
		case event, ok := <-s.webhookEvents:
			if !ok {
				return // Channel closed
			}
			s.handleIssueEvent(event)
		}
	}
}

// handleIssueEvent processes an issue event and routes it to the appropriate agent.
func (s *Supervisor) handleIssueEvent(event *IssueEvent) {
	slog.Info("processing issue event",
		"type", event.Type,
		"project", event.Project,
		"issue", event.IssueID,
		"author", event.Author,
	)

	// Look up the project
	proj, err := s.registry.Get(event.Project)
	if err != nil {
		slog.Warn("unknown project for webhook event",
			"project", event.Project,
			"error", err,
		)
		return
	}

	// Get the orchestrator for this project
	s.mu.RLock()
	orch := s.orchestrators[proj.Name]
	s.mu.RUnlock()

	if orch == nil {
		slog.Warn("no orchestrator for project",
			"project", event.Project,
		)
		return
	}

	// Route the event based on type
	switch event.Type {
	case EventIssueComment:
		s.handleCommentEvent(orch, event)
	case EventIssueCreated:
		// Issue creation could trigger agent spawning
		// The orchestrator already handles this via polling
		slog.Debug("issue created event received (handled by orchestrator polling)",
			"project", event.Project,
			"issue", event.IssueID,
		)
	case EventIssueUpdated:
		slog.Debug("issue updated event received",
			"project", event.Project,
			"issue", event.IssueID,
		)
	}
}

// handleCommentEvent processes an issue comment event.
// If an agent is working on the issue, the comment is sent to the agent.
// Otherwise, a new agent may be spawned to handle the comment.
func (s *Supervisor) handleCommentEvent(orch *orchestrator.Orchestrator, event *IssueEvent) {
	// Check if an agent already has this issue claimed
	agentID := orch.Claims().ClaimedBy(event.IssueID)
	if agentID != "" {
		// Agent is working on this issue - send the comment as a message
		a, err := s.agents.Get(agentID)
		if err != nil {
			slog.Warn("failed to get agent for comment delivery",
				"agent", agentID,
				"error", err,
			)
			return
		}

		// Format the comment as a message to the agent
		msg := fmt.Sprintf("New comment on issue #%s from %s:\n\n%s",
			event.IssueID, event.Author, event.Body)

		if err := a.SendMessage(msg); err != nil {
			slog.Warn("failed to send comment to agent",
				"agent", agentID,
				"error", err,
			)
		} else {
			slog.Info("delivered comment to agent",
				"agent", agentID,
				"issue", event.IssueID,
				"author", event.Author,
			)
		}
		return
	}

	// No agent is working on this issue - log for now
	// Future: could spawn an agent to handle the comment
	slog.Info("comment received for unclaimed issue",
		"project", event.Project,
		"issue", event.IssueID,
		"author", event.Author,
	)
}
