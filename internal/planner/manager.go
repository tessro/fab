package planner

import (
	"errors"
	"log/slog"
	"sync"

	"github.com/tessro/fab/internal/event"
	"github.com/tessro/fab/internal/id"
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
	EventDeleted      EventType = "deleted"
	EventPlanComplete EventType = "plan_complete"
)

// Event represents a planner lifecycle event.
type Event struct {
	Type     EventType
	Planner  *Planner
	OldState State // For state_changed events
	NewState State // For state_changed events
	PlanFile string // For plan_complete events
}

// EventHandler is called when planner events occur.
type EventHandler func(event Event)

// Manager manages planning agents.
// Planning agents are not subject to max-agents limits.
type Manager struct {
	// +checklocks:mu
	planners map[string]*Planner // ID -> Planner

	events event.Emitter[Event]

	mu sync.RWMutex
}

// NewManager creates a new planner manager.
func NewManager() *Manager {
	return &Manager{
		planners: make(map[string]*Planner),
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
func (m *Manager) Create(project, workDir, prompt string) (*Planner, error) {
	return m.CreateWithID(id.Generate(), project, workDir, prompt)
}

// CreateWithID creates a new planning agent with a specific ID.
// This is useful when the ID must be known before creation (e.g., for worktree naming).
func (m *Manager) CreateWithID(id, project, workDir, prompt string) (*Planner, error) {
	p := New(id, project, workDir, prompt)

	// Register state change callback
	p.OnStateChange(func(old, new State) {
		slog.Debug("planner state changed",
			"planner", p.ID(),
			"from", old,
			"to", new,
		)
		m.emit(Event{
			Type:     EventStateChanged,
			Planner:  p,
			OldState: old,
			NewState: new,
		})
	})

	// Register plan completion callback
	p.OnPlanComplete(func(planFile string) {
		slog.Info("planner completed plan",
			"planner", p.ID(),
			"plan_file", planFile,
		)
		m.emit(Event{
			Type:     EventPlanComplete,
			Planner:  p,
			PlanFile: planFile,
		})
	})

	m.mu.Lock()
	m.planners[id] = p
	m.mu.Unlock()

	slog.Info("planner created",
		"planner", id,
		"project", project,
		"workdir", workDir,
	)

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
