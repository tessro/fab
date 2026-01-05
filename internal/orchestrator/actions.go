package orchestrator

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Errors returned by orchestrator operations.
var (
	ErrAlreadyRunning    = errors.New("orchestrator is already running")
	ErrNotRunning        = errors.New("orchestrator is not running")
	ErrActionNotFound    = errors.New("action not found")
	ErrUnknownActionType = errors.New("unknown action type")
)

// ActionType identifies the type of staged orchestrator action.
type ActionType string

const (
	// ActionSendMessage sends a message to an agent's PTY.
	ActionSendMessage ActionType = "send_message"

	// ActionQuit sends /quit to gracefully end the agent session.
	ActionQuit ActionType = "quit"
)

// StagedAction represents an orchestrator action pending user approval.
type StagedAction struct {
	ID        string
	AgentID   string
	Project   string
	Type      ActionType
	Payload   string // Action-specific data (e.g., message text)
	CreatedAt time.Time
}

// ActionQueue manages staged actions for manual mode.
type ActionQueue struct {
	// +checklocks:mu
	actions []StagedAction
	mu      sync.RWMutex
}

// NewActionQueue creates a new action queue.
func NewActionQueue() *ActionQueue {
	return &ActionQueue{
		actions: make([]StagedAction, 0),
	}
}

// Add adds a new action to the queue.
// The action ID will be generated if not set.
func (q *ActionQueue) Add(action StagedAction) {
	q.mu.Lock()
	defer q.mu.Unlock()

	if action.ID == "" {
		action.ID = generateActionID()
	}

	q.actions = append(q.actions, action)
}

// List returns all pending actions.
func (q *ActionQueue) List() []StagedAction {
	q.mu.RLock()
	defer q.mu.RUnlock()

	result := make([]StagedAction, len(q.actions))
	copy(result, q.actions)
	return result
}

// ListForProject returns pending actions for a specific project.
func (q *ActionQueue) ListForProject(project string) []StagedAction {
	q.mu.RLock()
	defer q.mu.RUnlock()

	result := make([]StagedAction, 0)
	for _, a := range q.actions {
		if a.Project == project {
			result = append(result, a)
		}
	}
	return result
}

// Get returns an action by ID.
func (q *ActionQueue) Get(id string) (StagedAction, bool) {
	q.mu.RLock()
	defer q.mu.RUnlock()

	for _, a := range q.actions {
		if a.ID == id {
			return a, true
		}
	}
	return StagedAction{}, false
}

// Remove removes and returns an action by ID.
func (q *ActionQueue) Remove(id string) (StagedAction, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()

	for i, a := range q.actions {
		if a.ID == id {
			// Remove by swapping with last and truncating
			q.actions[i] = q.actions[len(q.actions)-1]
			q.actions = q.actions[:len(q.actions)-1]
			return a, true
		}
	}
	return StagedAction{}, false
}

// RemoveForAgent removes all actions for a specific agent.
func (q *ActionQueue) RemoveForAgent(agentID string) []StagedAction {
	q.mu.Lock()
	defer q.mu.Unlock()

	removed := make([]StagedAction, 0)
	remaining := make([]StagedAction, 0, len(q.actions))

	for _, a := range q.actions {
		if a.AgentID == agentID {
			removed = append(removed, a)
		} else {
			remaining = append(remaining, a)
		}
	}

	q.actions = remaining
	return removed
}

// Len returns the number of pending actions.
func (q *ActionQueue) Len() int {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return len(q.actions)
}

// Clear removes all pending actions.
func (q *ActionQueue) Clear() {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.actions = make([]StagedAction, 0)
}

// generateActionID generates a unique action ID.
func generateActionID() string {
	bytes := make([]byte, 4)
	_, _ = rand.Read(bytes)
	return hex.EncodeToString(bytes)
}
