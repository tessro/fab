package planner

import (
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/tessro/fab/internal/backend"
	"github.com/tessro/fab/internal/event"
	"github.com/tessro/fab/internal/id"
	"github.com/tessro/fab/internal/runtime"
)

// Manager errors.
var (
	ErrPlannerNotFound      = errors.New("planner not found")
	ErrPlannerAlreadyExists = errors.New("planner already exists")
)

// EventType identifies the type of planner event.
type EventType string

const (
	EventCreated      EventType = "created"
	EventStateChanged EventType = "state_changed"
	EventInfoChanged  EventType = "info_changed"
	EventDeleted      EventType = "deleted"
)

// Event represents a planner lifecycle event.
type Event struct {
	Type     EventType
	Planner  *Planner
	OldState State // For state_changed events
	NewState State // For state_changed events
}

// EventHandler is called when planner events occur.
type EventHandler func(event Event)

// Manager manages planning agents.
// Planning agents are not subject to max-agents limits.
type Manager struct {
	// +checklocks:mu
	planners map[string]*Planner // ID -> Planner

	events event.Emitter[Event]

	// runtimeStore persists planner metadata for daemon restart recovery.
	// May be nil if persistence is disabled.
	runtimeStore *runtime.Store

	mu sync.RWMutex
}

// NewManager creates a new planner manager.
func NewManager() *Manager {
	return &Manager{
		planners: make(map[string]*Planner),
	}
}

// SetRuntimeStore sets the runtime store for persisting planner metadata.
func (m *Manager) SetRuntimeStore(store *runtime.Store) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtimeStore = store
}

// savePlannerRuntime persists planner runtime metadata to the store.
func (m *Manager) savePlannerRuntime(p *Planner) {
	m.mu.RLock()
	store := m.runtimeStore
	m.mu.RUnlock()

	if store == nil {
		return
	}

	info := p.Info()
	rt := runtime.AgentRuntime{
		ID:         p.ID(),
		Project:    p.Project(),
		Kind:       runtime.KindPlanner,
		Backend:    info.Backend,
		PID:        0, // ProcessAgent doesn't expose PID directly
		StartedAt:  info.StartedAt,
		ThreadID:   p.ThreadID(),
		LastState:  string(info.State),
		LastUpdate: time.Now(),
	}

	if err := store.Upsert(rt); err != nil {
		slog.Error("failed to save planner runtime", "planner", p.ID(), "error", err)
	}
}

// removePlannerRuntime removes planner metadata from the runtime store.
func (m *Manager) removePlannerRuntime(id string) {
	m.mu.RLock()
	store := m.runtimeStore
	m.mu.RUnlock()

	if store == nil {
		return
	}

	if err := store.Remove(id); err != nil {
		slog.Error("failed to remove planner runtime", "planner", id, "error", err)
	}
}

// updatePlannerState updates the state in the runtime store.
func (m *Manager) updatePlannerState(id string, state State) {
	m.mu.RLock()
	store := m.runtimeStore
	m.mu.RUnlock()

	if store == nil {
		return
	}

	if err := store.UpdateState(id, string(state)); err != nil {
		slog.Error("failed to update planner runtime state", "planner", id, "error", err)
	}
}

// UpdatePlannerThreadID updates the thread ID in the runtime store.
func (m *Manager) UpdatePlannerThreadID(id, threadID string) {
	m.mu.RLock()
	store := m.runtimeStore
	m.mu.RUnlock()

	if store == nil {
		return
	}

	if err := store.UpdateThreadID(id, threadID); err != nil {
		slog.Error("failed to update planner thread ID", "planner", id, "error", err)
	}
}

// OnEvent registers an event handler.
func (m *Manager) OnEvent(handler EventHandler) {
	m.events.OnEvent(handler)
}

// emit sends an event to all registered handlers.
func (m *Manager) emit(e Event) {
	m.events.Emit(e)
}

// GenerateID generates a new unique planner ID.
func (m *Manager) GenerateID() string {
	return id.Generate()
}

// Create creates a new planning agent.
// workDir is the directory the planner will work in.
// prompt is the planning task to work on.
// b is the backend to use for CLI command building.
func (m *Manager) Create(project, workDir, prompt string, b backend.Backend) (*Planner, error) {
	return m.CreateWithID(id.Generate(), project, workDir, prompt, b)
}

// CreateWithID creates a new planning agent with a specific ID.
// This is useful when the ID must be known before creation (e.g., for worktree naming).
// b is the backend to use for CLI command building.
func (m *Manager) CreateWithID(plannerID, project, workDir, prompt string, b backend.Backend) (*Planner, error) {
	p := New(plannerID, project, workDir, prompt, b)

	// Register state change callback to emit events and update runtime store
	p.OnStateChange(func(old, new State) {
		slog.Debug("planner state changed",
			"planner", p.ID(),
			"from", old,
			"to", new,
		)
		m.updatePlannerState(p.ID(), new)
		m.emit(Event{
			Type:     EventStateChanged,
			Planner:  p,
			OldState: old,
			NewState: new,
		})
	})

	// Register info change callback to emit events when description changes
	p.OnInfoChange(func() {
		slog.Debug("planner info changed",
			"planner", p.ID(),
			"project", project,
			"description", p.Info().Description,
		)
		m.emit(Event{
			Type:    EventInfoChanged,
			Planner: p,
		})
	})

	// Register thread ID change callback to persist to runtime store
	p.OnThreadIDChange(func(threadID string) {
		slog.Debug("planner thread ID changed",
			"planner", p.ID(),
			"project", project,
			"thread_id", threadID,
		)
		m.UpdatePlannerThreadID(p.ID(), threadID)
	})

	m.mu.Lock()
	m.planners[plannerID] = p
	m.mu.Unlock()

	slog.Info("planner created",
		"planner", plannerID,
		"project", project,
		"workdir", workDir,
	)

	// Persist planner runtime metadata
	m.savePlannerRuntime(p)

	m.emit(Event{
		Type:    EventCreated,
		Planner: p,
	})

	return p, nil
}

// Get retrieves a planner by ID.
func (m *Manager) Get(id string) (*Planner, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	p, ok := m.planners[id]
	if !ok {
		return nil, ErrPlannerNotFound
	}
	return p, nil
}

// List returns all planners.
func (m *Manager) List() []*Planner {
	m.mu.RLock()
	defer m.mu.RUnlock()

	result := make([]*Planner, 0, len(m.planners))
	for _, p := range m.planners {
		result = append(result, p)
	}
	return result
}

// ListByProject returns planners for a specific project.
func (m *Manager) ListByProject(project string) []*Planner {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var result []*Planner
	for _, p := range m.planners {
		if p.Project() == project {
			result = append(result, p)
		}
	}
	return result
}

// Count returns the total number of planners.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.planners)
}

// Delete removes a planner from the manager.
// The planner should be stopped before calling Delete.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()

	p, ok := m.planners[id]
	if !ok {
		m.mu.Unlock()
		return ErrPlannerNotFound
	}

	delete(m.planners, id)
	m.mu.Unlock()

	// Remove from runtime store
	m.removePlannerRuntime(id)

	slog.Info("planner deleted", "planner", id)

	m.emit(Event{
		Type:    EventDeleted,
		Planner: p,
	})

	return nil
}

// Stop stops a planner.
func (m *Manager) Stop(id string) error {
	p, err := m.Get(id)
	if err != nil {
		return err
	}

	slog.Debug("stopping planner", "planner", id)

	if err := p.Stop(); err != nil && !errors.Is(err, ErrNotRunning) {
		slog.Error("failed to stop planner", "planner", id, "error", err)
		return err
	}

	slog.Info("planner stopped", "planner", id, "state", p.State())
	return nil
}

// StopAll stops all planners.
func (m *Manager) StopAll() {
	planners := m.List()
	for _, p := range planners {
		_ = m.Stop(p.ID())
	}
}

// ActivePlanners returns all planners in Starting or Running state.
func (m *Manager) ActivePlanners() []*Planner {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var active []*Planner
	for _, p := range m.planners {
		if p.IsRunning() {
			active = append(active, p)
		}
	}
	return active
}
