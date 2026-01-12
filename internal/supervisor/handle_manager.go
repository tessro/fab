package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/backend"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/manager"
	"github.com/tessro/fab/internal/rules"
	"github.com/tessro/fab/internal/runtime"
)

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

	// Get the agent backend for this project
	b, err := backend.Get(proj.GetAgentBackend())
	if err != nil {
		s.mu.Unlock()
		return nil, fmt.Errorf("get backend: %w", err)
	}

	// Create new manager for this project
	wtPath := proj.ManagerWorktreePath()
	mgr = manager.New(wtPath, projectName, b, s.managerPatterns)
	s.managers[projectName] = mgr
	s.mu.Unlock()

	return mgr, nil
}

// setupManagerCallbacks sets up callbacks for manager events to broadcast to TUI clients.
func (s *Supervisor) setupManagerCallbacks(mgr *manager.Manager) {
	projectName := mgr.Project()

	mgr.OnStateChange(func(old, new manager.State) {
		s.updateManagerRuntimeState(projectName, new)
		s.broadcastManagerStateTyped(projectName, new, mgr.StartedAt())
	})
	mgr.OnEntry(func(entry agent.ChatEntry) {
		s.broadcastManagerChatEntry(projectName, entry)
	})
	mgr.OnThreadIDChange(func(threadID string) {
		s.updateManagerThreadID(projectName, threadID)
	})
}

// saveManagerRuntime persists manager runtime metadata to the store.
func (s *Supervisor) saveManagerRuntime(mgr *manager.Manager) {
	s.mu.RLock()
	store := s.runtimeStore
	s.mu.RUnlock()

	if store == nil {
		return
	}

	rt := runtime.AgentRuntime{
		ID:         "manager:" + mgr.Project(),
		Project:    mgr.Project(),
		Kind:       runtime.KindManager,
		PID:        0, // ProcessAgent doesn't expose PID directly
		StartedAt:  mgr.StartedAt(),
		ThreadID:   mgr.ThreadID(),
		LastState:  string(mgr.State()),
		LastUpdate: time.Now(),
	}

	if err := store.Upsert(rt); err != nil {
		slog.Error("failed to save manager runtime", "project", mgr.Project(), "error", err)
	}
}

// removeManagerRuntime removes manager metadata from the runtime store.
func (s *Supervisor) removeManagerRuntime(projectName string) {
	s.mu.RLock()
	store := s.runtimeStore
	s.mu.RUnlock()

	if store == nil {
		return
	}

	if err := store.Remove("manager:" + projectName); err != nil {
		slog.Error("failed to remove manager runtime", "project", projectName, "error", err)
	}
}

// updateManagerRuntimeState updates the manager state in the runtime store.
func (s *Supervisor) updateManagerRuntimeState(projectName string, state manager.State) {
	s.mu.RLock()
	store := s.runtimeStore
	s.mu.RUnlock()

	if store == nil {
		return
	}

	if err := store.UpdateState("manager:"+projectName, string(state)); err != nil {
		slog.Error("failed to update manager runtime state", "project", projectName, "error", err)
	}
}

// updateManagerThreadID updates the manager thread ID in the runtime store.
func (s *Supervisor) updateManagerThreadID(projectName, threadID string) {
	s.mu.RLock()
	store := s.runtimeStore
	s.mu.RUnlock()

	if store == nil {
		return
	}

	if err := store.UpdateThreadID("manager:"+projectName, threadID); err != nil {
		slog.Error("failed to update manager thread ID", "project", projectName, "error", err)
	}
}

// broadcastManagerStateTyped sends a manager state change to attached clients.
// This is the typed version that takes manager.State directly.
func (s *Supervisor) broadcastManagerStateTyped(projectName string, state manager.State, startedAt time.Time) {
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

	// Persist manager runtime metadata
	s.saveManagerRuntime(mgr)

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

	// Remove from runtime store
	s.removeManagerRuntime(stopReq.Project)

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
