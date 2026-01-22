package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/backend"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/director"
	"github.com/tessro/fab/internal/paths"
	"github.com/tessro/fab/internal/rules"
	"github.com/tessro/fab/internal/runtime"
)

// getOrCreateDirector returns the global director agent, creating it if necessary.
func (s *Supervisor) getOrCreateDirector() (*director.Director, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Return existing director if already created
	if s.director != nil {
		return s.director, nil
	}

	// Get director work directory
	workDir, err := paths.DirectorWorkDir()
	if err != nil {
		return nil, fmt.Errorf("get director work dir: %w", err)
	}

	// Get the default backend (claude)
	b, err := backend.Get("claude")
	if err != nil {
		return nil, fmt.Errorf("get backend: %w", err)
	}

	// Load director allowed patterns from global permissions.toml
	directorPatterns := loadDirectorPatterns()

	// Create the director with access to the registry for cross-project visibility
	s.director = director.New(workDir, b, directorPatterns, s.registry)

	return s.director, nil
}

// setupDirectorCallbacks sets up callbacks for director events to broadcast to TUI clients.
func (s *Supervisor) setupDirectorCallbacks(d *director.Director) {
	d.OnStateChange(func(old, new director.State) {
		s.updateDirectorRuntimeState(new)
		s.broadcastDirectorState(string(new), d.StartedAt())
	})
	d.OnEntry(func(entry agent.ChatEntry) {
		s.broadcastDirectorChatEntry(entry)
	})
	d.OnThreadIDChange(func(threadID string) {
		s.updateDirectorThreadID(threadID)
	})
}

// saveDirectorRuntime persists director runtime metadata to the store.
func (s *Supervisor) saveDirectorRuntime(d *director.Director) {
	s.mu.RLock()
	store := s.runtimeStore
	s.mu.RUnlock()

	if store == nil {
		return
	}

	rt := runtime.AgentRuntime{
		ID:         "director",
		Project:    "", // Director is global, no project
		Kind:       runtime.KindDirector,
		PID:        0, // ProcessAgent doesn't expose PID directly
		StartedAt:  d.StartedAt(),
		ThreadID:   d.ThreadID(),
		LastState:  string(d.State()),
		LastUpdate: time.Now(),
	}

	if err := store.Upsert(rt); err != nil {
		slog.Error("failed to save director runtime", "error", err)
	}
}

// removeDirectorRuntime removes director metadata from the runtime store.
func (s *Supervisor) removeDirectorRuntime() {
	s.mu.RLock()
	store := s.runtimeStore
	s.mu.RUnlock()

	if store == nil {
		return
	}

	if err := store.Remove("director"); err != nil {
		slog.Error("failed to remove director runtime", "error", err)
	}
}

// updateDirectorRuntimeState updates the director state in the runtime store.
func (s *Supervisor) updateDirectorRuntimeState(state director.State) {
	s.mu.RLock()
	store := s.runtimeStore
	s.mu.RUnlock()

	if store == nil {
		return
	}

	if err := store.UpdateState("director", string(state)); err != nil {
		slog.Error("failed to update director runtime state", "error", err)
	}
}

// updateDirectorThreadID updates the director thread ID in the runtime store.
func (s *Supervisor) updateDirectorThreadID(threadID string) {
	s.mu.RLock()
	store := s.runtimeStore
	s.mu.RUnlock()

	if store == nil {
		return
	}

	if err := store.UpdateThreadID("director", threadID); err != nil {
		slog.Error("failed to update director thread ID", "error", err)
	}
}

// handleDirectorStart starts the global director agent.
func (s *Supervisor) handleDirectorStart(_ context.Context, req *daemon.Request) *daemon.Response {
	d, err := s.getOrCreateDirector()
	if err != nil {
		return errorResponse(req, err.Error())
	}

	// Set up callbacks before starting
	s.setupDirectorCallbacks(d)

	if err := d.Start(); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to start director: %v", err))
	}

	// Persist director runtime metadata
	s.saveDirectorRuntime(d)

	slog.Info("director agent started")
	return successResponse(req, nil)
}

// handleDirectorStop stops the global director agent.
func (s *Supervisor) handleDirectorStop(_ context.Context, req *daemon.Request) *daemon.Response {
	s.mu.RLock()
	d := s.director
	s.mu.RUnlock()

	if d == nil {
		return errorResponse(req, "director not running")
	}

	if err := d.Stop(); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to stop director: %v", err))
	}

	// Remove from runtime store
	s.removeDirectorRuntime()

	slog.Info("director agent stopped")
	return successResponse(req, nil)
}

// handleDirectorStatus returns the director agent status.
func (s *Supervisor) handleDirectorStatus(_ context.Context, req *daemon.Request) *daemon.Response {
	// Get director work directory for response
	workDir, err := paths.DirectorWorkDir()
	if err != nil {
		return errorResponse(req, fmt.Sprintf("get director work dir: %v", err))
	}

	s.mu.RLock()
	d := s.director
	s.mu.RUnlock()

	if d == nil {
		// No director exists yet - return stopped status
		return successResponse(req, daemon.DirectorStatusResponse{
			Running:   false,
			State:     string(director.StateStopped),
			StartedAt: "",
			WorkDir:   workDir,
		})
	}

	startedAt := ""
	if d.IsRunning() {
		startedAt = d.StartedAt().Format(time.RFC3339)
	}

	return successResponse(req, daemon.DirectorStatusResponse{
		Running:   d.IsRunning(),
		State:     string(d.State()),
		StartedAt: startedAt,
		WorkDir:   d.WorkDir(),
	})
}

// handleDirectorSendMessage sends a message to the director agent.
func (s *Supervisor) handleDirectorSendMessage(_ context.Context, req *daemon.Request) *daemon.Response {
	var sendReq daemon.DirectorSendMessageRequest
	if err := unmarshalPayload(req.Payload, &sendReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	s.mu.RLock()
	d := s.director
	s.mu.RUnlock()

	if d == nil {
		return errorResponse(req, "director not running")
	}

	// If director process has stopped (e.g., Claude Code exited after responding),
	// auto-resume it to continue the conversation.
	if d.State() == director.StateStopped {
		slog.Info("auto-resuming stopped director")
		if err := d.Resume(); err != nil {
			return errorResponse(req, fmt.Sprintf("failed to resume director: %v", err))
		}
		// Update runtime store with resumed state
		s.saveDirectorRuntime(d)
	}

	if err := d.SendMessage(sendReq.Content); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to send message: %v", err))
	}

	return successResponse(req, nil)
}

// handleDirectorChatHistory returns the director chat history.
func (s *Supervisor) handleDirectorChatHistory(_ context.Context, req *daemon.Request) *daemon.Response {
	var histReq daemon.DirectorChatHistoryRequest
	if err := unmarshalPayload(req.Payload, &histReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	s.mu.RLock()
	d := s.director
	s.mu.RUnlock()

	if d == nil {
		// No director exists - return empty history
		return successResponse(req, daemon.DirectorChatHistoryResponse{
			Entries: []daemon.ChatEntryDTO{},
		})
	}

	entries := d.History().Entries(histReq.Limit)

	// Convert to DTO format
	dtos := make([]daemon.ChatEntryDTO, len(entries))
	for i, e := range entries {
		dtos[i] = daemon.ChatEntryDTO{
			Role:       e.Role,
			Content:    e.Content,
			ToolName:   e.ToolName,
			ToolInput:  e.ToolInput,
			ToolResult: e.ToolResult,
			IsError:    e.IsError,
			Timestamp:  e.Timestamp.Format(time.RFC3339),
		}
	}

	return successResponse(req, daemon.DirectorChatHistoryResponse{
		Entries: dtos,
	})
}

// handleDirectorClearHistory clears the director chat history.
func (s *Supervisor) handleDirectorClearHistory(_ context.Context, req *daemon.Request) *daemon.Response {
	s.mu.RLock()
	d := s.director
	s.mu.RUnlock()

	if d == nil {
		return errorResponse(req, "director not running")
	}

	d.History().Clear()

	slog.Info("director chat history cleared")
	return successResponse(req, nil)
}

// loadDirectorPatterns loads director allowed patterns from the global permissions.toml.
// Returns default patterns if the file doesn't exist or has no director section.
func loadDirectorPatterns() []string {
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

	// For now, director uses the same patterns as manager.
	// A future enhancement could add director-specific patterns.
	return cfg.ManagerAllowedPatterns()
}
