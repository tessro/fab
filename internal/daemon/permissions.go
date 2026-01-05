// Package daemon provides the fab daemon server and IPC protocol.
package daemon

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

// Permission manager errors.
var (
	ErrPermissionNotFound = errors.New("permission request not found")
	ErrPermissionExpired  = errors.New("permission request expired")
)

// PermissionManager tracks pending permission requests with response channels.
// The hook command blocks waiting for a response, which is sent via the channel.
type PermissionManager struct {
	mu      sync.RWMutex
	pending map[string]*pendingPermission
	timeout time.Duration
}

// pendingPermission holds a request and its response channel.
type pendingPermission struct {
	request  *PermissionRequest
	response chan *PermissionResponse
}

// NewPermissionManager creates a new permission manager with the given timeout.
func NewPermissionManager(timeout time.Duration) *PermissionManager {
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	return &PermissionManager{
		pending: make(map[string]*pendingPermission),
		timeout: timeout,
	}
}

// Add registers a new permission request and returns a channel that will receive the response.
// The caller should block on the returned channel.
// Returns the generated request ID and the response channel.
func (m *PermissionManager) Add(req *PermissionRequest) (string, <-chan *PermissionResponse) {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Generate ID if not set
	if req.ID == "" {
		req.ID = generatePermissionID()
	}

	// Set timestamp if not set
	if req.RequestedAt.IsZero() {
		req.RequestedAt = time.Now()
	}

	// Create response channel (buffered to avoid blocking on send)
	respCh := make(chan *PermissionResponse, 1)

	m.pending[req.ID] = &pendingPermission{
		request:  req,
		response: respCh,
	}

	return req.ID, respCh
}

// Respond sends a response to a pending permission request.
// This unblocks the waiting hook command.
func (m *PermissionManager) Respond(id string, resp *PermissionResponse) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	pending, ok := m.pending[id]
	if !ok {
		return ErrPermissionNotFound
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

// Get retrieves a pending permission request by ID.
func (m *PermissionManager) Get(id string) *PermissionRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if pending, ok := m.pending[id]; ok {
		return pending.request
	}
	return nil
}

// List returns all pending permission requests.
func (m *PermissionManager) List() []*PermissionRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	requests := make([]*PermissionRequest, 0, len(m.pending))
	for _, pending := range m.pending {
		requests = append(requests, pending.request)
	}
	return requests
}

// ListForProject returns pending permission requests for a specific project.
func (m *PermissionManager) ListForProject(project string) []*PermissionRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var requests []*PermissionRequest
	for _, pending := range m.pending {
		if pending.request.Project == project {
			requests = append(requests, pending.request)
		}
	}
	return requests
}

// ListForAgent returns pending permission requests for a specific agent.
func (m *PermissionManager) ListForAgent(agentID string) []*PermissionRequest {
	m.mu.RLock()
	defer m.mu.RUnlock()

	var requests []*PermissionRequest
	for _, pending := range m.pending {
		if pending.request.AgentID == agentID {
			requests = append(requests, pending.request)
		}
	}
	return requests
}

// Remove cancels a pending permission request without sending a response.
// The waiting hook will receive nil on the channel when it's closed.
func (m *PermissionManager) Remove(id string) bool {
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

// RemoveForAgent removes all pending permission requests for an agent.
func (m *PermissionManager) RemoveForAgent(agentID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	var removed int
	for id, pending := range m.pending {
		if pending.request.AgentID == agentID {
			close(pending.response)
			delete(m.pending, id)
			removed++
		}
	}
	return removed
}

// Cleanup removes expired permission requests.
// Should be called periodically to prevent memory leaks.
func (m *PermissionManager) Cleanup() int {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	var removed int

	for id, pending := range m.pending {
		if now.Sub(pending.request.RequestedAt) > m.timeout {
			// Close channel without sending a response - this causes the agent to fail
			// rather than receiving a rejection that it might try to work around
			close(pending.response)
			delete(m.pending, id)
			removed++
		}
	}
	return removed
}

// Count returns the number of pending permission requests.
func (m *PermissionManager) Count() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.pending)
}

// generatePermissionID generates a random 8-character hex ID.
func generatePermissionID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
