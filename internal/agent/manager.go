// Package agent provides the Agent type and lifecycle management for Claude Code instances.
package agent

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"sync"

	"github.com/tessro/fab/internal/event"
	"github.com/tessro/fab/internal/project"
)

// Manager errors.
var (
	ErrAgentNotFound      = errors.New("agent not found")
	ErrAgentAlreadyExists = errors.New("agent already exists")
	ErrNoCapacity         = errors.New("no capacity for new agent")
)

// EventType identifies the type of agent event.
type EventType string

const (
	EventCreated      EventType = "created"
	EventStateChanged EventType = "state_changed"
	EventInfoChanged  EventType = "info_changed"
	EventDeleted      EventType = "deleted"
)

// ProjectEventType identifies the type of project event.
type ProjectEventType string

const (
	ProjectEventRegistered   ProjectEventType = "registered"
	ProjectEventUnregistered ProjectEventType = "unregistered"
)

// Event represents an agent lifecycle event.
type Event struct {
	Type     EventType
	Agent    *Agent
	OldState State // For state_changed events
	NewState State // For state_changed events
}

// EventHandler is called when agent events occur.
type EventHandler func(event Event)

// ProjectEvent represents a project lifecycle event.
type ProjectEvent struct {
	Type    ProjectEventType
	Project *project.Project
}

// ProjectEventHandler is called when project events occur.
type ProjectEventHandler func(event ProjectEvent)

// Manager manages a pool of agents across projects.
// It handles creation, tracking, and lifecycle events for all agents.
type Manager struct {
	// +checklocks:mu
	agents map[string]*Agent // ID -> Agent
	// +checklocks:mu
	projects map[string][]*Agent // Project name -> Agents
	// +checklocks:mu
	registry map[string]*project.Project // Project name -> Project

	events        event.Emitter[Event]
	projectEvents event.Emitter[ProjectEvent]

	mu sync.RWMutex
}

// NewManager creates a new agent manager.
func NewManager() *Manager {
	return &Manager{
		agents:   make(map[string]*Agent),
		projects: make(map[string][]*Agent),
		registry: make(map[string]*project.Project),
	}
}

// RegisterProject registers a project with the manager.
// This allows the manager to track agents by project.
func (m *Manager) RegisterProject(p *project.Project) {
	m.mu.Lock()
	m.registry[p.Name] = p
	if _, ok := m.projects[p.Name]; !ok {
		m.projects[p.Name] = make([]*Agent, 0)
	}
	m.mu.Unlock()

	m.emitProjectEvent(ProjectEvent{
		Type:    ProjectEventRegistered,
		Project: p,
	})
}

// UnregisterProject removes a project from the manager.
// Any agents for this project should be stopped first.
func (m *Manager) UnregisterProject(name string) {
	m.mu.Lock()
	proj := m.registry[name]
	delete(m.registry, name)
	delete(m.projects, name)
	m.mu.Unlock()

	if proj != nil {
		m.emitProjectEvent(ProjectEvent{
			Type:    ProjectEventUnregistered,
			Project: proj,
		})
	}
}

// OnEvent registers an event handler.
// Handlers are called synchronously when events occur.
func (m *Manager) OnEvent(handler EventHandler) {
	m.events.OnEvent(handler)
}

// OnProjectEvent registers a project event handler.
// Handlers are called synchronously when project events occur.
func (m *Manager) OnProjectEvent(handler ProjectEventHandler) {
	m.projectEvents.OnEvent(handler)
}

// emit sends an event to all registered handlers.
// Must not be called with lock held.
func (m *Manager) emit(e Event) {
	m.events.Emit(e)
}

// emitProjectEvent sends a project event to all registered handlers.
// Must not be called with lock held.
func (m *Manager) emitProjectEvent(e ProjectEvent) {
	m.projectEvents.Emit(e)
}

// Create creates a new agent for the given project.
// It creates a dedicated worktree for the agent and returns the new agent.
// Returns ErrNoCapacity if max agents reached.
func (m *Manager) Create(proj *project.Project) (*Agent, error) {
	id := generateID()

	// Create a dedicated worktree for this agent
	wt, err := proj.CreateWorktreeForAgent(id)
	if err != nil {
		if errors.Is(err, project.ErrNoWorktreeAvailable) {
			slog.Warn("max agents reached for project", "project", proj.Name)
			return nil, ErrNoCapacity
		}
		slog.Error("failed to create worktree", "project", proj.Name, "error", err)
		return nil, err
	}

	agent := New(id, proj, wt)

	// Register state change callback to emit events
	agent.OnStateChange(func(old, new State) {
		slog.Debug("agent state changed",
			"agent", agent.ID,
			"project", proj.Name,
			"from", old,
			"to", new,
		)
		m.emit(Event{
			Type:     EventStateChanged,
			Agent:    agent,
			OldState: old,
			NewState: new,
		})
	})

	// Register info change callback to emit events when task/description changes
	agent.OnInfoChange(func() {
		slog.Debug("agent info changed",
			"agent", agent.ID,
			"project", proj.Name,
			"task", agent.GetTask(),
			"description", agent.GetDescription(),
		)
		m.emit(Event{
			Type:  EventInfoChanged,
			Agent: agent,
		})
	})

	m.mu.Lock()
	m.agents[id] = agent
	m.projects[proj.Name] = append(m.projects[proj.Name], agent)
	m.mu.Unlock()

	slog.Info("agent created",
		"agent", id,
		"project", proj.Name,
		"worktree", wt.Path,
	)

	m.emit(Event{
		Type:  EventCreated,
		Agent: agent,
	})

	return agent, nil
}

// Get retrieves an agent by ID.
func (m *Manager) Get(id string) (*Agent, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	agent, ok := m.agents[id]
	if !ok {
		return nil, ErrAgentNotFound
	}
	return agent, nil
}

// List returns all agents, optionally filtered by project.
// If projectName is empty, returns all agents.
func (m *Manager) List(projectName string) []*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if projectName != "" {
		agents := m.projects[projectName]
		result := make([]*Agent, len(agents))
		copy(result, agents)
		return result
	}

	result := make([]*Agent, 0, len(m.agents))
	for _, agent := range m.agents {
		result = append(result, agent)
	}
	return result
}

// ListInfo returns AgentInfo for all agents, optionally filtered by project.
func (m *Manager) ListInfo(projectName string) []AgentInfo {
	agents := m.List(projectName)
	infos := make([]AgentInfo, len(agents))
	for i, agent := range agents {
		infos[i] = agent.Info()
	}
	return infos
}

// Count returns the total number of agents.
func (m *Manager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.agents)
}

// CountByProject returns the number of agents for a specific project.
func (m *Manager) CountByProject(projectName string) int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.projects[projectName])
}

// CountForProject is an alias for CountByProject.
func (m *Manager) CountForProject(projectName string) int {
	return m.CountByProject(projectName)
}

// CountByState returns counts of agents in each state.
func (m *Manager) CountByState() map[State]int {
	m.mu.RLock()
	defer m.mu.RUnlock()

	counts := make(map[State]int)
	for _, agent := range m.agents {
		state := agent.GetState()
		counts[state]++
	}
	return counts
}

// Delete removes an agent from the manager.
// The agent should be stopped before calling Delete.
// This deletes the agent's worktree from disk.
func (m *Manager) Delete(id string) error {
	m.mu.Lock()

	agent, ok := m.agents[id]
	if !ok {
		m.mu.Unlock()
		return ErrAgentNotFound
	}

	projectName := ""
	if agent.Project != nil {
		projectName = agent.Project.Name
	}

	// Remove from agents map
	delete(m.agents, id)

	// Remove from project list
	proj := agent.Project
	if proj != nil {
		agents := m.projects[projectName]
		for i, a := range agents {
			if a.ID == id {
				m.projects[projectName] = append(agents[:i], agents[i+1:]...)
				break
			}
		}
	}

	m.mu.Unlock()

	// Delete the agent's worktree
	if proj != nil {
		_ = proj.DeleteWorktreeForAgent(id)
	}

	slog.Info("agent deleted", "agent", id, "project", projectName)

	m.emit(Event{
		Type:  EventDeleted,
		Agent: agent,
	})

	return nil
}

// Stop stops an agent's process and marks it as done or error.
// If the agent is active, it's stopped gracefully.
func (m *Manager) Stop(id string) error {
	agent, err := m.Get(id)
	if err != nil {
		return err
	}

	slog.Debug("stopping agent", "agent", id)

	// Stop the process first (this unblocks any pending reads)
	if err := agent.Stop(); err != nil && !errors.Is(err, ErrProcessNotStarted) {
		slog.Error("failed to stop agent process", "agent", id, "error", err)
		_ = agent.MarkError()
		return err
	}

	// Stop the read loop goroutine (with timeout to avoid hanging)
	agent.StopReadLoop()

	// Mark as done if it was active
	if agent.IsActive() {
		_ = agent.MarkDone()
	}

	slog.Info("agent stopped", "agent", id, "state", agent.GetState())

	return nil
}

// StopAll stops all agents for a project.
func (m *Manager) StopAll(projectName string) {
	agents := m.List(projectName)
	for _, agent := range agents {
		_ = m.Stop(agent.ID)
	}
}

// DeleteAll deletes all agents for a project.
// Agents are stopped before deletion.
func (m *Manager) DeleteAll(projectName string) {
	agents := m.List(projectName)
	for _, agent := range agents {
		_ = m.Stop(agent.ID)
		_ = m.Delete(agent.ID)
	}
}

// ActiveAgents returns all agents in Starting, Running, or Idle state.
func (m *Manager) ActiveAgents() []*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var active []*Agent
	for _, agent := range m.agents {
		if agent.IsActive() {
			active = append(active, agent)
		}
	}
	return active
}

// RunningAgents returns all agents in Running state.
func (m *Manager) RunningAgents() []*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var running []*Agent
	for _, agent := range m.agents {
		if agent.GetState() == StateRunning {
			running = append(running, agent)
		}
	}
	return running
}

// IdleAgents returns all agents in Idle state.
func (m *Manager) IdleAgents() []*Agent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var idle []*Agent
	for _, agent := range m.agents {
		if agent.GetState() == StateIdle {
			idle = append(idle, agent)
		}
	}
	return idle
}

// generateID generates a random 6-character hex ID.
func generateID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
