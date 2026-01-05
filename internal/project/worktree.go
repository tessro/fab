// Package project provides worktree management for supervised coding projects.
package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// RestoreWorktreePool scans for existing worktrees and populates the pool.
// This is used when loading projects from config to restore the worktree state.
// If no worktrees exist on disk, it creates them.
func (p *Project) RestoreWorktreePool() error {
	p.mu.Lock()

	// Skip if already populated
	if len(p.Worktrees) > 0 {
		p.mu.Unlock()
		return nil
	}

	wtDir := p.WorktreesDir()

	// Check if worktrees directory exists
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		// No worktrees directory - need to create pool
		p.mu.Unlock()
		_, err := p.CreateWorktreePool()
		return err
	}

	// Scan for existing worktrees
	for i := 1; i <= p.MaxAgents; i++ {
		wtPath := filepath.Join(wtDir, fmt.Sprintf("wt-%03d", i))
		if _, err := os.Stat(wtPath); err == nil {
			p.Worktrees = append(p.Worktrees, Worktree{Path: wtPath})
		}
	}

	// If we found no worktrees, create them
	if len(p.Worktrees) == 0 {
		p.mu.Unlock()
		_, err := p.CreateWorktreePool()
		return err
	}

	p.mu.Unlock()
	return nil
}

// CreateWorktreePool creates the worktree directory and git worktrees for the project.
// Returns the list of created worktree paths.
func (p *Project) CreateWorktreePool() ([]string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	wtDir := p.WorktreesDir()

	// Create the worktrees directory
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		return nil, fmt.Errorf("create worktrees directory: %w", err)
	}

	paths := make([]string, 0, p.MaxAgents)

	for i := 1; i <= p.MaxAgents; i++ {
		wtPath := filepath.Join(wtDir, fmt.Sprintf("wt-%03d", i))

		// Check if worktree already exists
		if _, err := os.Stat(wtPath); err == nil {
			// Already exists, add to pool
			p.Worktrees = append(p.Worktrees, Worktree{Path: wtPath})
			paths = append(paths, wtPath)
			continue
		}

		// Create git worktree with detached HEAD
		cmd := exec.Command("git", "worktree", "add", "--detach", wtPath)
		cmd.Dir = p.RepoDir()
		if output, err := cmd.CombinedOutput(); err != nil {
			// Clean up any worktrees we created
			_ = p.cleanupWorktrees()
			return nil, fmt.Errorf("create worktree %s: %w\n%s", wtPath, err, output)
		}

		p.Worktrees = append(p.Worktrees, Worktree{Path: wtPath})
		paths = append(paths, wtPath)
	}

	return paths, nil
}

// DeleteWorktreePool removes all git worktrees and the worktrees directory.
func (p *Project) DeleteWorktreePool() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.cleanupWorktrees()
}

// ensureWorktreeExists checks if a worktree exists and recreates it if missing.
// Only attempts recreation if the worktrees directory exists and the project path is a valid git repo.
// Must be called with lock held.
func (p *Project) ensureWorktreeExists(wtPath string) error {
	// Check if worktree directory exists
	if _, err := os.Stat(wtPath); err == nil {
		return nil // Already exists
	}

	// Only attempt recreation if the worktrees directory exists
	// (indicates pool was previously created via CreateWorktreePool)
	wtDir := p.WorktreesDir()
	if _, err := os.Stat(wtDir); os.IsNotExist(err) {
		// Worktrees directory doesn't exist - skip recreation
		return nil
	}

	// Verify the repo is a valid git repository before attempting worktree operations
	repoDir := p.RepoDir()
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Not a git repo - skip recreation (likely a test scenario)
		return nil
	}

	// Prune stale worktree references first (in case git still has a ref to the deleted path)
	pruneCmd := exec.Command("git", "worktree", "prune")
	pruneCmd.Dir = repoDir
	_ = pruneCmd.Run() // Ignore errors from prune

	// Recreate the git worktree with detached HEAD
	cmd := exec.Command("git", "worktree", "add", "--detach", wtPath)
	cmd.Dir = repoDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("recreate worktree %s: %w\n%s", wtPath, err, output)
	}

	return nil
}

// resetWorktree resets a worktree to origin/main with a clean working directory.
// Must be called with lock held.
func (p *Project) resetWorktree(wtPath string) error {
	// Verify the repo is a valid git repository
	repoDir := p.RepoDir()
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil // Not a git repo - skip (likely a test scenario)
	}

	// Fetch latest from origin (run in repo root - worktrees share refs)
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoDir
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("fetch origin: %w\n%s", err, output)
	}

	// Reset worktree to origin/main
	resetCmd := exec.Command("git", "reset", "--hard", "origin/main")
	resetCmd.Dir = wtPath
	if output, err := resetCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("reset to origin/main: %w\n%s", err, output)
	}

	// Clean untracked files and directories
	cleanCmd := exec.Command("git", "clean", "-fd")
	cleanCmd.Dir = wtPath
	if output, err := cleanCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("clean untracked files: %w\n%s", err, output)
	}

	return nil
}

// createAgentBranch creates and checks out a branch for an agent's work.
// Must be called with lock held.
func (p *Project) createAgentBranch(wtPath, agentID string) error {
	// Verify the repo is a valid git repository
	repoDir := p.RepoDir()
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil // Not a git repo - skip (likely a test scenario)
	}

	branchName := "fab/" + agentID

	// Create and checkout the branch
	cmd := exec.Command("git", "checkout", "-b", branchName)
	cmd.Dir = wtPath
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create branch %s: %w\n%s", branchName, err, output)
	}

	return nil
}

// MergeResult represents the outcome of a merge attempt.
type MergeResult struct {
	Merged     bool   // True if merge succeeded and was pushed
	BranchName string // The branch that was merged
	Error      error  // Conflict or other error if merge failed
}

// MergeAgentBranch attempts to merge an agent's branch into main in the repo directory.
// If merge succeeds, pushes to origin/main.
// If merge fails due to conflicts, aborts and returns error (caller should rebase worktree).
// This method serializes merge operations using mergeMu to prevent concurrent conflicts.
func (p *Project) MergeAgentBranch(agentID string) (*MergeResult, error) {
	p.mergeMu.Lock()
	defer p.mergeMu.Unlock()

	repoDir := p.RepoDir()
	branchName := "fab/" + agentID

	// Verify the repo is a valid git repository
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return nil, fmt.Errorf("repo not found: %s", repoDir)
	}

	// Fetch latest from origin
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoDir
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("fetch: %w\n%s", err, output)
	}

	// Checkout main in repo dir
	checkoutCmd := exec.Command("git", "checkout", "main")
	checkoutCmd.Dir = repoDir
	if output, err := checkoutCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("checkout main: %w\n%s", err, output)
	}

	// Pull latest main (fast-forward only)
	pullCmd := exec.Command("git", "pull", "--ff-only", "origin", "main")
	pullCmd.Dir = repoDir
	if output, err := pullCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("pull main: %w\n%s", err, output)
	}

	// Try to merge the agent's branch
	mergeMsg := fmt.Sprintf("Merge %s into main", branchName)
	mergeCmd := exec.Command("git", "merge", branchName, "--no-ff", "-m", mergeMsg)
	mergeCmd.Dir = repoDir
	mergeOutput, mergeErr := mergeCmd.CombinedOutput()

	if mergeErr != nil {
		// Merge failed - abort and return error
		abortCmd := exec.Command("git", "merge", "--abort")
		abortCmd.Dir = repoDir
		_ = abortCmd.Run()

		return &MergeResult{
			Merged:     false,
			BranchName: branchName,
			Error:      fmt.Errorf("merge conflict: %s", string(mergeOutput)),
		}, nil
	}

	// Merge succeeded - push to origin
	pushCmd := exec.Command("git", "push", "origin", "main")
	pushCmd.Dir = repoDir
	if output, err := pushCmd.CombinedOutput(); err != nil {
		// Rollback merge
		resetCmd := exec.Command("git", "reset", "--hard", "HEAD~1")
		resetCmd.Dir = repoDir
		_ = resetCmd.Run()
		return nil, fmt.Errorf("push main: %w\n%s", err, output)
	}

	return &MergeResult{
		Merged:     true,
		BranchName: branchName,
	}, nil
}

// RebaseWorktreeOnMain rebases a worktree's current branch onto origin/main.
// Used when merge fails to bring the agent's worktree up to date with latest main.
func (p *Project) RebaseWorktreeOnMain(agentID string) error {
	p.mu.RLock()
	var wtPath string
	for _, wt := range p.Worktrees {
		if wt.AgentID == agentID {
			wtPath = wt.Path
			break
		}
	}
	p.mu.RUnlock()

	if wtPath == "" {
		return ErrWorktreeNotFound
	}

	repoDir := p.RepoDir()

	// Fetch latest from origin
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoDir
	_ = fetchCmd.Run()

	// Rebase onto origin/main
	rebaseCmd := exec.Command("git", "rebase", "origin/main")
	rebaseCmd.Dir = wtPath
	if output, err := rebaseCmd.CombinedOutput(); err != nil {
		// Abort failed rebase
		abortCmd := exec.Command("git", "rebase", "--abort")
		abortCmd.Dir = wtPath
		_ = abortCmd.Run()
		return fmt.Errorf("rebase failed: %w\n%s", err, output)
	}

	return nil
}

// cleanupWorktrees removes all worktrees.
//
// +checklocks:p.mu
func (p *Project) cleanupWorktrees() error {
	var lastErr error
	repoDir := p.RepoDir()

	for _, wt := range p.Worktrees {
		// Remove git worktree
		cmd := exec.Command("git", "worktree", "remove", "--force", wt.Path)
		cmd.Dir = repoDir
		if err := cmd.Run(); err != nil {
			// Try manual removal if git worktree remove fails
			if rmErr := os.RemoveAll(wt.Path); rmErr != nil {
				lastErr = fmt.Errorf("remove worktree %s: %w", wt.Path, rmErr)
			}
		}
	}

	// Clear the worktrees slice
	p.Worktrees = p.Worktrees[:0]

	// Remove the worktrees directory if empty
	wtDir := p.WorktreesDir()
	if entries, err := os.ReadDir(wtDir); err == nil && len(entries) == 0 {
		_ = os.Remove(wtDir)
	}

	// Prune stale worktree references
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = repoDir
	_ = cmd.Run() // Ignore errors from prune

	return lastErr
}
