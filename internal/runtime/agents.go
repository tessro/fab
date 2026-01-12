// Package runtime provides persistent runtime metadata storage for agent processes.
// This allows the daemon to discover and reconnect to agents after restart.
package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tessro/fab/internal/paths"
)

// AgentKind represents the type of agent.
type AgentKind string

const (
	KindCoding  AgentKind = "coding"
	KindManager AgentKind = "manager"
	KindPlanner AgentKind = "planner"
)

// AgentRuntime contains the runtime metadata needed to reconnect to an agent.
type AgentRuntime struct {
	// Core identification
	ID      string `json:"id"`
	Project string `json:"project"`

	// Agent classification
	Kind    AgentKind `json:"kind"`
	Backend string    `json:"backend,omitempty"` // e.g., "claude", "codex"

	// Process info
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`

	// Worktree for coding agents
	WorktreePath string `json:"worktree_path,omitempty"`

	// Conversation state
	ThreadID   string    `json:"thread_id,omitempty"` // For Codex conversation resumption
	LastState  string    `json:"last_state"`          // starting, running, idle, done, error
	LastUpdate time.Time `json:"last_update"`

	// Host connection info (for future agent host support)
	HostSocketPath string `json:"host_socket_path,omitempty"`
	StreamID       string `json:"stream_id,omitempty"`
}

// Errors returned by the runtime store.
var (
	ErrAgentNotFound = errors.New("agent not found in runtime store")
)

// Store manages persistent agent runtime metadata.
// It provides atomic read/write operations with file locking.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore creates a new runtime store using the default path.
func NewStore() (*Store, error) {
	path, err := paths.AgentsRuntimePath()
	if err != nil {
		return nil, fmt.Errorf("get runtime path: %w", err)
	}
	return &Store{path: path}, nil
}

// NewStoreWithPath creates a new runtime store with a custom path.
// This is useful for testing.
func NewStoreWithPath(path string) *Store {
	return &Store{path: path}
}

// Path returns the path to the runtime file.
func (s *Store) Path() string {
	return s.path
}

// Upsert adds or updates an agent's runtime metadata.
// If the agent already exists, it is updated; otherwise, it is added.
func (s *Store) Upsert(agent AgentRuntime) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	agents, err := s.readLocked()
	if err != nil {
		return err
	}

	// Update or append
	found := false
	for i, a := range agents {
		if a.ID == agent.ID {
			agents[i] = agent
			found = true
			break
		}
	}
	if !found {
		agents = append(agents, agent)
	}

	return s.writeLocked(agents)
}

// Remove deletes an agent from the runtime store.
// Returns nil if the agent was not found (idempotent).
func (s *Store) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	agents, err := s.readLocked()
	if err != nil {
		return err
	}

	// Filter out the agent
	filtered := make([]AgentRuntime, 0, len(agents))
	for _, a := range agents {
		if a.ID != id {
			filtered = append(filtered, a)
		}
	}

	return s.writeLocked(filtered)
}

// Get retrieves an agent's runtime metadata by ID.
// Returns ErrAgentNotFound if the agent does not exist.
func (s *Store) Get(id string) (*AgentRuntime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	agents, err := s.readLocked()
	if err != nil {
		return nil, err
	}

	for _, a := range agents {
		if a.ID == id {
			return &a, nil
		}
	}

	return nil, ErrAgentNotFound
}

// List returns all agents in the runtime store.
func (s *Store) List() ([]AgentRuntime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.readLocked()
}

// ListByProject returns all agents for a specific project.
func (s *Store) ListByProject(project string) ([]AgentRuntime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	agents, err := s.readLocked()
	if err != nil {
		return nil, err
	}

	filtered := make([]AgentRuntime, 0)
	for _, a := range agents {
		if a.Project == project {
			filtered = append(filtered, a)
		}
	}

	return filtered, nil
}

// ListByKind returns all agents of a specific kind.
func (s *Store) ListByKind(kind AgentKind) ([]AgentRuntime, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	agents, err := s.readLocked()
	if err != nil {
		return nil, err
	}

	filtered := make([]AgentRuntime, 0)
	for _, a := range agents {
		if a.Kind == kind {
			filtered = append(filtered, a)
		}
	}

	return filtered, nil
}

// UpdateThreadID updates the thread ID for an agent.
// This is a convenience method for partial updates.
func (s *Store) UpdateThreadID(id, threadID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	agents, err := s.readLocked()
	if err != nil {
		return err
	}

	for i, a := range agents {
		if a.ID == id {
			agents[i].ThreadID = threadID
			agents[i].LastUpdate = time.Now()
			return s.writeLocked(agents)
		}
	}

	return ErrAgentNotFound
}

// UpdateState updates the last known state for an agent.
// This is a convenience method for partial updates.
func (s *Store) UpdateState(id, state string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	agents, err := s.readLocked()
	if err != nil {
		return err
	}

	for i, a := range agents {
		if a.ID == id {
			agents[i].LastState = state
			agents[i].LastUpdate = time.Now()
			return s.writeLocked(agents)
		}
	}

	return ErrAgentNotFound
}

// Clear removes all agents from the runtime store.
func (s *Store) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.writeLocked([]AgentRuntime{})
}

// readLocked reads the runtime file. Must be called with mu held.
func (s *Store) readLocked() ([]AgentRuntime, error) {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return []AgentRuntime{}, nil
		}
		return nil, fmt.Errorf("read runtime file: %w", err)
	}

	if len(data) == 0 {
		return []AgentRuntime{}, nil
	}

	var agents []AgentRuntime
	if err := json.Unmarshal(data, &agents); err != nil {
		return nil, fmt.Errorf("parse runtime file: %w", err)
	}

	return agents, nil
}

// writeLocked writes the runtime file atomically. Must be called with mu held.
// Uses write-to-temp-then-rename pattern for atomicity.
func (s *Store) writeLocked(agents []AgentRuntime) error {
	// Ensure directory exists
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create runtime dir: %w", err)
	}

	// Marshal data
	data, err := json.MarshalIndent(agents, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal agents: %w", err)
	}

	// Write to temp file in same directory (for atomic rename)
	tmpFile := s.path + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	// Atomic rename
	if err := os.Rename(tmpFile, s.path); err != nil {
		os.Remove(tmpFile) // Clean up on failure
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}
