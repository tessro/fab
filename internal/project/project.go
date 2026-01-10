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

// ManagerWorktreeID is the worktree ID for the project manager.
const ManagerWorktreeID = "manager"

// ErrNoWorktreeAvailable is returned when all worktrees are in use.
var ErrNoWorktreeAvailable = errors.New("no worktree available")

// ErrWorktreeNotFound is returned when a worktree is not found.
var ErrWorktreeNotFound = errors.New("worktree not found")

// Project represents a supervised coding project.
type Project struct {
	Name           string   // Unique identifier (e.g., "myapp")
	RemoteURL      string   // Git remote URL (e.g., "git@github.com:user/repo.git")
	MaxAgents      int      // Max concurrent agents (default: 3)
	IssueBackend   string   // Issue backend type: "tk" (default), "linear", "github", "gh"
	AllowedAuthors []string // GitHub usernames allowed to create issues (empty = infer from remote URL)
	Autostart      bool     // Start orchestration when daemon starts
	PermissionsChecker string // Permission checker type: "manual" (default, TUI prompts), "llm" (LLM-based)
	BaseDir        string   // Base directory for project storage (default: ~/.fab/projects)
	// +checklocks:mu
	Running bool // Whether orchestration is active
	// +checklocks:mu
	Worktrees []Worktree // Active worktrees for agents

	mu      sync.RWMutex // Protects Running and Worktrees
	mergeMu sync.Mutex   // Serializes merge operations
}

// AddWorktree appends a worktree to the list (for testing).
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

// worktreePathForAgent returns the path for an agent's worktree.
// Returns ~/.fab/projects/<projectName>/worktrees/wt-{agentID}
func (p *Project) worktreePathForAgent(agentID string) string {
	return filepath.Join(p.WorktreesDir(), "wt-"+agentID)
}

// IssuesDir returns the path to the issues directory within the repo.
// Returns ~/.fab/projects/<projectName>/repo/.tickets/
func (p *Project) IssuesDir() string {
	return filepath.Join(p.RepoDir(), ".tickets")
}

// CreateWorktreeForAgent creates a dedicated worktree for an agent.
// The worktree is named wt-{agentID} and checked out on a fab/{agentID} branch.
// Returns ErrNoWorktreeAvailable if MaxAgents is reached.
func (p *Project) CreateWorktreeForAgent(agentID string) (*Worktree, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check capacity
	if len(p.Worktrees) >= p.MaxAgents {
		return nil, ErrNoWorktreeAvailable
	}

	// Create worktree path
	wtPath := p.worktreePathForAgent(agentID)

	// Create the git worktree
	if err := p.createWorktree(wtPath); err != nil {
		return nil, err
	}

	// Reset worktree to pristine state (origin/main)
	_ = p.resetWorktree(wtPath)
	// Create a branch for this agent's work
	_ = p.createAgentBranch(wtPath, agentID)

	wt := Worktree{
		Path:    wtPath,
		InUse:   true,
		AgentID: agentID,
	}
	p.Worktrees = append(p.Worktrees, wt)

	return &wt, nil
}

// DeleteWorktreeForAgent removes an agent's worktree from disk and the tracking list.
// Returns ErrWorktreeNotFound if no worktree is assigned to that agent.
func (p *Project) DeleteWorktreeForAgent(agentID string) error {
	p.mu.Lock()

	var wtPath string
	wtIndex := -1
	for i := range p.Worktrees {
		if p.Worktrees[i].AgentID == agentID {
			wtPath = p.Worktrees[i].Path
			wtIndex = i
			break
		}
	}

	if wtIndex == -1 {
		p.mu.Unlock()
		return ErrWorktreeNotFound
	}

	// Remove from tracking list
	p.Worktrees = append(p.Worktrees[:wtIndex], p.Worktrees[wtIndex+1:]...)
	p.mu.Unlock()

	// Delete the worktree from disk outside the lock
	return p.removeWorktree(wtPath)
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

// ManagerWorktreePath returns the path to the manager's worktree.
func (p *Project) ManagerWorktreePath() string {
	return filepath.Join(p.WorktreesDir(), "wt-"+ManagerWorktreeID)
}

// CreateManagerWorktree creates the worktree for the project manager.
// Unlike agent worktrees, the manager worktree persists across sessions.
func (p *Project) CreateManagerWorktree() error {
	wtPath := p.ManagerWorktreePath()

	// Check if worktree already exists
	if _, err := os.Stat(wtPath); err == nil {
		// Already exists, just ensure it's up to date
		_ = p.resetWorktreeUnlocked(wtPath)
		return nil
	}

	// Create the worktree
	if err := p.createWorktree(wtPath); err != nil {
		return err
	}

	// Reset to pristine state
	_ = p.resetWorktreeUnlocked(wtPath)

	return nil
}

// PlannerWorktreePath returns the path to a planner's worktree.
func (p *Project) PlannerWorktreePath(plannerID string) string {
	return filepath.Join(p.WorktreesDir(), "wt-plan-"+plannerID)
}

// CreatePlannerWorktree creates a dedicated worktree for a planner.
// Unlike agent worktrees, planner worktrees are NOT subject to MaxAgents limits.
// The planner gets read-only access to the codebase for exploration.
func (p *Project) CreatePlannerWorktree(plannerID string) (string, error) {
	wtPath := p.PlannerWorktreePath(plannerID)

	// Check if worktree already exists
	if _, err := os.Stat(wtPath); err == nil {
		// Already exists, just ensure it's up to date
		_ = p.resetWorktreeUnlocked(wtPath)
		return wtPath, nil
	}

	// Create the worktree
	if err := p.createWorktree(wtPath); err != nil {
		return "", err
	}

	// Reset to pristine state (planners work off origin/main)
	_ = p.resetWorktreeUnlocked(wtPath)

	return wtPath, nil
}

// DeletePlannerWorktree removes a planner's worktree from disk.
func (p *Project) DeletePlannerWorktree(plannerID string) error {
	wtPath := p.PlannerWorktreePath(plannerID)
	return p.removeWorktree(wtPath)
}
