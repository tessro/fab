// Package daemon provides the fab daemon server and IPC protocol.
package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// User question manager errors.
var (
	ErrQuestionNotFound = errors.New("user question not found")
	ErrQuestionExpired  = errors.New("user question expired")
)

// UserQuestionManager tracks pending user questions with response channels.
// The hook command blocks waiting for a response, which is sent via the channel.
type UserQuestionManager struct {
	mu sync.RWMutex
	// +checklocks:mu
	pending map[string]*pendingQuestion
	timeout time.Duration
}

// pendingQuestion holds a question and its response channel.
type pendingQuestion struct {
	question *UserQuestion
	response chan *UserQuestionResponse
}

// NewUserQuestionManager creates a new user question manager with the given timeout.
func NewUserQuestionManager(timeout time.Duration) *UserQuestionManager {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &UserQuestionManager{
		pending: make(map[string]*pendingQuestion),
		timeout: timeout,
	}
}

// Add registers a new user question and returns a channel that will receive the response.
// The caller should block on the returned channel.
// Returns the generated question ID and the response channel.
func (m *UserQuestionManager) Add(q *UserQuestion) (string, <-chan *UserQuestionResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate ID if not set
	if q.ID == "" {
		q.ID = generateQuestionID()
	}

	// Set timestamp if not set
	if q.RequestedAt.IsZero() {
		q.RequestedAt = time.Now()
	}

	// Create response channel (buffered to avoid blocking on send)
	respCh := make(chan *UserQuestionResponse, 1)

	m.pending[q.ID] = &pendingQuestion{
		question: q,
		response: respCh,
	}

	return q.ID, respCh
}

// Respond sends a response to a pending user question.
// This unblocks the waiting hook command.
func (m *UserQuestionManager) Respond(id string, resp *UserQuestionResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pending, ok := m.pending[id]
	if !ok {
		return ErrQuestionNotFound
	}

	// Ensure response has correct ID
	resp.ID = id

	// Send response (non-blocking due to buffered channel)
	select {
	case pending.response <- resp:
	default:
		// Channel full - should not happen with buffer size 1
	}

	// Remove from pending
	delete(m.pending, id)

	return nil
}

// Get retrieves a pending user question by ID.
func (m *UserQuestionManager) Get(id string) *UserQuestion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if pending, ok := m.pending[id]; ok {
		return pending.question
	}
	return nil
}

// List returns all pending user questions.
func (m *UserQuestionManager) List() []*UserQuestion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	questions := make([]*UserQuestion, 0, len(m.pending))
	for _, pending := range m.pending {
		questions = append(questions, pending.question)
	}
	return questions
}

// ListForAgent returns pending user questions for a specific agent.
func (m *UserQuestionManager) ListForAgent(agentID string) []*UserQuestion {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var questions []*UserQuestion
	for _, pending := range m.pending {
		if pending.question.AgentID == agentID {
			questions = append(questions, pending.question)
		}
	}
	return questions
}

// Remove cancels a pending user question without sending a response.
// The waiting hook will receive nil on the channel when it's closed.
func (m *UserQuestionManager) Remove(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	pending, ok := m.pending[id]
	if !ok {
		return false
	}

	// Close the channel to unblock any waiters
	close(pending.response)
	delete(m.pending, id)
	return true
}

// RemoveForAgent removes all pending user questions for an agent.
func (m *UserQuestionManager) RemoveForAgent(agentID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	var removed int
	for id, pending := range m.pending {
		if pending.question.AgentID == agentID {
			close(pending.response)
			delete(m.pending, id)
			removed++
		}
	}
	return removed
}

// Cleanup removes expired user questions.
// Should be called periodically to prevent memory leaks.
func (m *UserQuestionManager) Cleanup() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var removed int

	for id, pending := range m.pending {
		if now.Sub(pending.question.RequestedAt) > m.timeout {
			// Close channel without sending a response - this causes the agent to fail
			// rather than receiving a rejection that it might try to work around
			close(pending.response)
			delete(m.pending, id)
			removed++
		}
	}
	return removed
}

// Count returns the number of pending user questions.
func (m *UserQuestionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pending)
}

// generateQuestionID generates a random 8-character hex ID.
func generateQuestionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
