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
	Name      string // Unique identifier (e.g., "myapp")
	RemoteURL string // Git remote URL (e.g., "git@github.com:user/repo.git")
	MaxAgents int    // Max concurrent agents (default: 3)
	BaseDir   string // Base directory for project storage (default: ~/.fab/projects)
	// +checklocks:mu
	Running bool // Whether orchestration is active
	// +checklocks:mu
	Worktrees []Worktree // Pool of worktrees for agents

	mu      sync.RWMutex // Protects Running and Worktrees
	mergeMu sync.Mutex   // Serializes merge operations
}

// AddWorktree appends a worktree to the pool (for testing).
func (p *Project) AddWorktree(wt Worktree) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Worktrees = append(p.Worktrees, wt)
}

// Worktree represents a git worktree used by an agent.
type Worktree struct {
	Path    string // Absolute path (e.g., "~/.fab/projects/myapp/worktrees/wt-001")
	InUse   bool   // Whether assigned to an agent
	AgentID string // Agent ID if in use (empty if available)
}

// NewProject creates a new Project with default settings.
func NewProject(name, remoteURL string) *Project {
	return &Project{
		Name:      name,
		RemoteURL: remoteURL,
		MaxAgents: DefaultMaxAgents,
		Running:   false,
		Worktrees: make([]Worktree, 0, DefaultMaxAgents),
	}
}

// ProjectDir returns the path to the project directory.
// Returns BaseDir/<projectName>/ if BaseDir is set, otherwise ~/.fab/projects/<projectName>/
func (p *Project) ProjectDir() string {
	if p.BaseDir != "" {
		return filepath.Join(p.BaseDir, p.Name)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join("/tmp", ".fab", "projects", p.Name)
	}
	return filepath.Join(home, ".fab", "projects", p.Name)
}

// RepoDir returns the path to fab's clone of the repository.
// Returns ~/.fab/projects/<projectName>/repo/
func (p *Project) RepoDir() string {
	return filepath.Join(p.ProjectDir(), "repo")
}

// WorktreesDir returns the path to the worktrees directory.
// Returns ~/.fab/projects/<projectName>/worktrees/
func (p *Project) WorktreesDir() string {
	return filepath.Join(p.ProjectDir(), "worktrees")
}

// GetAvailableWorktree returns an available worktree and marks it as in use.
// If the worktree directory is missing, it will be recreated.
// Returns ErrNoWorktreeAvailable if all worktrees are occupied.
func (p *Project) GetAvailableWorktree(agentID string) (*Worktree, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i := range p.Worktrees {
		if !p.Worktrees[i].InUse {
			// Ensure the worktree directory exists, recreate if missing
			if err := p.ensureWorktreeExists(p.Worktrees[i].Path); err != nil {
				// Skip this worktree if recreation fails, try next
				continue
			}
			// Reset worktree to pristine state (origin/main)
			// Log errors but don't fail - agent can still work with non-pristine state
			_ = p.resetWorktree(p.Worktrees[i].Path)
			// Create a branch for this agent's work
			_ = p.createAgentBranch(p.Worktrees[i].Path, agentID)
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

// ReturnWorktreeToPool releases a worktree and resets it to a clean state.
// This performs cleanup immediately rather than lazily on next allocation.
// Returns ErrWorktreeNotFound if no worktree is assigned to that agent.
func (p *Project) ReturnWorktreeToPool(agentID string) error {
	p.mu.Lock()

	var wtPath string
	for i := range p.Worktrees {
		if p.Worktrees[i].AgentID == agentID {
			wtPath = p.Worktrees[i].Path
			p.Worktrees[i].InUse = false
			p.Worktrees[i].AgentID = ""
			break
		}
	}
	p.mu.Unlock()

	if wtPath == "" {
		return ErrWorktreeNotFound
	}

	// Reset worktree to pristine state (origin/main) outside the lock.
	// Ignore errors - worktree is already marked available, and reset
	// will be retried on next allocation if needed.
	_ = p.resetWorktreeUnlocked(wtPath)

	return nil
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
