package orchestrator

import (
	"errors"
	"sync"
)

// Errors for claim operations.
var (
	ErrAlreadyClaimed = errors.New("ticket already claimed by another agent")
	ErrNotClaimed     = errors.New("ticket not claimed")
)

// ClaimRegistry tracks which tickets are claimed by which agents.
// Claims are held in memory and cleared on daemon restart.
// All methods are safe for concurrent use.
type ClaimRegistry struct {
	mu sync.RWMutex
	// +checklocks:mu
	claims map[string]string // ticketID -> agentID
}

// NewClaimRegistry creates a new ClaimRegistry.
func NewClaimRegistry() *ClaimRegistry {
	return &ClaimRegistry{
		claims: make(map[string]string),
	}
}

// Claim attempts to claim a ticket for an agent.
// Returns ErrAlreadyClaimed if another agent already holds the claim.
// Claiming a ticket already held by the same agent is idempotent (returns nil).
func (r *ClaimRegistry) Claim(ticketID, agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if existing, ok := r.claims[ticketID]; ok {
		if existing == agentID {
			return nil // Idempotent - already claimed by same agent
		}
		return ErrAlreadyClaimed
	}
	r.claims[ticketID] = agentID
	return nil
}

// Release releases a claim on a specific ticket.
func (r *ClaimRegistry) Release(ticketID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.claims, ticketID)
}

// ReleaseByAgent releases all claims held by an agent.
// Returns the number of claims released.
func (r *ClaimRegistry) ReleaseByAgent(agentID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	count := 0
	for tid, aid := range r.claims {
		if aid == agentID {
			delete(r.claims, tid)
			count++
		}
	}
	return count
}

// ClaimedBy returns the agent ID holding the claim on a ticket, or empty string if unclaimed.
func (r *ClaimRegistry) ClaimedBy(ticketID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.claims[ticketID]
}

// IsClaimed returns true if the ticket is claimed by any agent.
func (r *ClaimRegistry) IsClaimed(ticketID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.claims[ticketID]
	return ok
}

// List returns a copy of all current claims (ticketID -> agentID).
func (r *ClaimRegistry) List() map[string]string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]string, len(r.claims))
	for k, v := range r.claims {
		result[k] = v
	}
	return result
}

// Count returns the number of active claims.
func (r *ClaimRegistry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.claims)
}
