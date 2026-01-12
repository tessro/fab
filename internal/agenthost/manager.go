package agenthost

import (
	"container/ring"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/tessro/fab/internal/agent"
)

// HistoryBufferSize is the number of stream events to keep in the ring buffer.
const HistoryBufferSize = 1000

// Errors returned by manager operations.
var (
	ErrAgentNotRunning = errors.New("agent is not running")
	ErrAgentNotSet     = errors.New("agent not set")
)

// Manager manages a single agent and its output streaming.
// It wraps an agent.Agent and provides methods for the host server.
type Manager struct {
	mu sync.RWMutex

	// +checklocks:mu
	agent *agent.Agent
	// +checklocks:mu
	server *Server

	// Agent metadata
	// +checklocks:mu
	agentID string
	// +checklocks:mu
	project string
	// +checklocks:mu
	worktree string
	// +checklocks:mu
	backend string

	// Stream management
	// +checklocks:mu
	streamOffset int64
	// +checklocks:mu
	historyBuffer *ring.Ring
}

// NewManager creates a new manager for the given agent.
func NewManager(ag *agent.Agent) *Manager {
	info := ag.Info()
	m := &Manager{
		agent:         ag,
		agentID:       ag.ID,
		project:       info.Project,
		worktree:      info.Worktree,
		backend:       info.Backend,
		streamOffset:  0,
		historyBuffer: ring.New(HistoryBufferSize),
	}

	// Set up callbacks for state changes
	ag.OnStateChange(func(old, new agent.State) {
		m.onStateChange(old, new)
	})

	ag.OnInfoChange(func() {
		m.onInfoChange()
	})

	return m
}

// SetServer sets the server for broadcasting events.
func (m *Manager) SetServer(s *Server) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.server = s
}

// AgentID returns the agent ID.
func (m *Manager) AgentID() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.agentID
}

// StreamOffset returns the current stream offset.
func (m *Manager) StreamOffset() int64 {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.streamOffset
}

// AgentInfo returns the current agent info for status reporting.
func (m *Manager) AgentInfo() AgentInfo {
	m.mu.RLock()
	ag := m.agent
	m.mu.RUnlock()

	if ag == nil {
		return AgentInfo{
			ID:    m.agentID,
			State: "error",
		}
	}

	info := ag.Info()
	return AgentInfo{
		ID:          info.ID,
		Project:     info.Project,
		State:       string(info.State),
		PID:         ag.PID(),
		Worktree:    info.Worktree,
		StartedAt:   info.StartedAt,
		Task:        info.Task,
		Description: info.Description,
		Backend:     info.Backend,
	}
}

// SendMessage sends a message to the agent.
func (m *Manager) SendMessage(content string) error {
	m.mu.RLock()
	ag := m.agent
	m.mu.RUnlock()

	if ag == nil {
		return ErrAgentNotSet
	}

	// Mark that user sent input (for intervention detection)
	ag.MarkUserInput()

	return ag.SendMessage(content)
}

// Stop stops the agent.
func (m *Manager) Stop(force bool, timeout time.Duration) error {
	m.mu.RLock()
	ag := m.agent
	m.mu.RUnlock()

	if ag == nil {
		return ErrAgentNotSet
	}

	if force {
		return ag.StopWithTimeout(0)
	}
	return ag.StopWithTimeout(timeout)
}

// StartReadLoop starts the agent's read loop with callbacks for streaming.
func (m *Manager) StartReadLoop() error {
	m.mu.RLock()
	ag := m.agent
	m.mu.RUnlock()

	if ag == nil {
		return ErrAgentNotSet
	}

	cfg := agent.ReadLoopConfig{
		OnEntry: func(entry agent.ChatEntry) {
			m.onChatEntry(entry)
		},
		OnOutput: func(data []byte) {
			m.onOutput(data)
		},
	}

	return ag.StartReadLoop(cfg)
}

// onStateChange is called when the agent state changes.
func (m *Manager) onStateChange(old, new agent.State) {
	event := m.createEvent("state", nil)
	event.State = string(new)
	m.bufferAndBroadcast(event)
}

// onInfoChange is called when agent task or description changes.
func (m *Manager) onInfoChange() {
	m.mu.RLock()
	ag := m.agent
	m.mu.RUnlock()

	if ag == nil {
		return
	}

	info := ag.Info()
	event := m.createEvent("state", nil)
	event.State = string(info.State)
	// Info changes are bundled with state events
	m.bufferAndBroadcast(event)
}

// onChatEntry is called when a chat entry is parsed.
func (m *Manager) onChatEntry(entry agent.ChatEntry) {
	event := m.createEvent("chat_entry", nil)
	event.ChatEntry = entry
	m.bufferAndBroadcast(event)
}

// onOutput is called with raw JSONL output.
func (m *Manager) onOutput(data []byte) {
	event := m.createEvent("output", data)
	m.bufferAndBroadcast(event)
}

// createEvent creates a new stream event with the next offset.
func (m *Manager) createEvent(eventType string, data []byte) *StreamEvent {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.streamOffset++
	return &StreamEvent{
		Type:      eventType,
		AgentID:   m.agentID,
		Offset:    m.streamOffset,
		Timestamp: time.Now().Format(time.RFC3339),
		Data:      string(data),
	}
}

// bufferAndBroadcast adds an event to the history buffer and broadcasts it.
func (m *Manager) bufferAndBroadcast(event *StreamEvent) {
	// Add to ring buffer
	m.mu.Lock()
	m.historyBuffer.Value = event
	m.historyBuffer = m.historyBuffer.Next()
	server := m.server
	m.mu.Unlock()

	// Broadcast to attached clients
	if server != nil {
		server.Broadcast(event)
	}
}

// GetBufferedEvents returns events from the history buffer starting from the given offset.
// Events are returned in chronological order (sorted by offset).
func (m *Manager) GetBufferedEvents(fromOffset int64) []*StreamEvent {
	m.mu.RLock()
	defer m.mu.RUnlock()

	events := make([]*StreamEvent, 0)

	// Traverse the ring buffer
	m.historyBuffer.Do(func(v any) {
		if v == nil {
			return
		}
		event, ok := v.(*StreamEvent)
		if !ok {
			return
		}
		if event.Offset > fromOffset {
			events = append(events, event)
		}
	})

	// Sort by offset to ensure chronological order (ring buffer traversal order
	// may not be chronological when the buffer wraps)
	sort.Slice(events, func(i, j int) bool {
		return events[i].Offset < events[j].Offset
	})

	return events
}
