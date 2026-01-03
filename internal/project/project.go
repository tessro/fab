// Package project provides the Project type for managing supervised coding projects.
package project

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
)

// DefaultMaxAgents is the default number of concurrent agents per project.
const DefaultMaxAgents = 3

// ErrNoWorktreeAvailable is returned when all worktrees are in use.
var ErrNoWorktreeAvailable = errors.New("no worktree available")

// ErrWorktreeNotFound is returned when a worktree is not found.
var ErrWorktreeNotFound = errors.New("worktree not found")

// Project represents a supervised coding project.
type Project struct {
	Name      string     // Unique identifier (e.g., "myapp")
	Path      string     // Absolute path to project root
	MaxAgents int        // Max concurrent agents (default: 3)
	Running   bool       // Whether orchestration is active
	Worktrees []Worktree // Pool of worktrees for agents

	mu sync.RWMutex // Protects Worktrees and Running
}

// Worktree represents a git worktree used by an agent.
type Worktree struct {
	Path    string // Absolute path (e.g., "~/.fab/worktrees/myapp/wt-001")
	InUse   bool   // Whether assigned to an agent
	AgentID string // Agent ID if in use (empty if available)
}

// NewProject creates a new Project with default settings.
func NewProject(name, path string) *Project {
	return &Project{
		Name:      name,
		Path:      path,
		MaxAgents: DefaultMaxAgents,
		Running:   false,
		Worktrees: make([]Worktree, 0, DefaultMaxAgents),
	}
}

// WorktreesDir returns the path to the worktrees directory.
// Returns ~/.fab/worktrees/<projectName> or falls back to <project>/.fab-worktrees on error.
func (p *Project) WorktreesDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(p.Path, ".fab-worktrees")
	}
	return filepath.Join(home, ".fab", "worktrees", p.Name)
}

// GetAvailableWorktree returns an available worktree and marks it as in use.
// Returns ErrNoWorktreeAvailable if all worktrees are occupied.
func (p *Project) GetAvailableWorktree(agentID string) (*Worktree, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.Worktrees {
		if !p.Worktrees[i].InUse {
			p.Worktrees[i].InUse = true
			p.Worktrees[i].AgentID = agentID
			return &p.Worktrees[i], nil
		}
	}
	return nil, ErrNoWorktreeAvailable
}

// ReleaseWorktree marks a worktree as available.
// Returns ErrWorktreeNotFound if the worktree path doesn't match any in the pool.
func (p *Project) ReleaseWorktree(path string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.Worktrees {
		if p.Worktrees[i].Path == path {
			p.Worktrees[i].InUse = false
			p.Worktrees[i].AgentID = ""
			return nil
		}
	}
	return ErrWorktreeNotFound
}

// ReleaseWorktreeByAgent marks a worktree as available by agent ID.
// Returns ErrWorktreeNotFound if no worktree is assigned to that agent.
func (p *Project) ReleaseWorktreeByAgent(agentID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.Worktrees {
		if p.Worktrees[i].AgentID == agentID {
			p.Worktrees[i].InUse = false
			p.Worktrees[i].AgentID = ""
			return nil
		}
	}
	return ErrWorktreeNotFound
}

// AvailableWorktreeCount returns the number of available worktrees.
func (p *Project) AvailableWorktreeCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, wt := range p.Worktrees {
		if !wt.InUse {
			count++
		}
	}
	return count
}

// ActiveAgentCount returns the number of agents currently using worktrees.
func (p *Project) ActiveAgentCount() int {
	p.mu.RLock()
	defer p.mu.RUnlock()

	count := 0
	for _, wt := range p.Worktrees {
		if wt.InUse {
			count++
		}
	}
	return count
}

// SetRunning sets the orchestration state.
func (p *Project) SetRunning(running bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Running = running
}

// IsRunning returns whether orchestration is active.
func (p *Project) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.Running
}
