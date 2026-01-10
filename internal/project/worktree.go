// Package project provides worktree management for supervised coding projects.
package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// createWorktree creates a git worktree at the specified path.
// Must be called with lock held.
func (p *Project) createWorktree(wtPath string) error {
	repoDir := p.RepoDir()

	// Verify the repo is a valid git repository
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Not a git repo - skip (likely a test scenario)
		return nil
	}

	// Ensure worktrees directory exists
	wtDir := p.WorktreesDir()
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		return fmt.Errorf("create worktrees directory: %w", err)
	}

	// Prune stale worktree references first
	pruneCmd := exec.Command("git", "worktree", "prune")
	pruneCmd.Dir = repoDir
	_ = pruneCmd.Run()

	// Create git worktree with detached HEAD
	cmd := exec.Command("git", "worktree", "add", "--detach", wtPath)
	cmd.Dir = repoDir
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("create worktree %s: %w\n%s", wtPath, err, output)
	}

	return nil
}

// removeWorktree removes a git worktree from disk.
func (p *Project) removeWorktree(wtPath string) error {
	repoDir := p.RepoDir()

	// Verify the repo is a valid git repository
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Not a git repo - just remove the directory
		return os.RemoveAll(wtPath)
	}

	// Try git worktree remove first
	cmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
	cmd.Dir = repoDir
	if err := cmd.Run(); err != nil {
		// Fall back to manual removal
		if rmErr := os.RemoveAll(wtPath); rmErr != nil {
			return fmt.Errorf("remove worktree %s: %w", wtPath, rmErr)
		}
	}

	// Prune stale worktree references
	pruneCmd := exec.Command("git", "worktree", "prune")
	pruneCmd.Dir = repoDir
	_ = pruneCmd.Run()

	return nil
}

// DeleteAllWorktrees removes all git worktrees and the worktrees directory.
func (p *Project) DeleteAllWorktrees() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	return p.cleanupWorktrees()
}

// resetWorktree resets a worktree to origin/main with a clean working directory.
// Must be called with lock held.
func (p *Project) resetWorktree(wtPath string) error {
	return p.resetWorktreeUnlocked(wtPath)
}

// resetWorktreeUnlocked resets a worktree to origin/main with a clean working directory.
// This is safe to call without holding the lock since it only operates on the filesystem.
func (p *Project) resetWorktreeUnlocked(wtPath string) error {
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

	// Clean untracked files and directories (including ignored files like build artifacts)
	cleanCmd := exec.Command("git", "clean", "-fdx")
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

// MergeResult represents the outcome of a rebase-and-merge attempt.
type MergeResult struct {
	Merged     bool   // True if rebase succeeded and was pushed
	BranchName string // The branch that was rebased and merged
	SHA        string // Commit SHA of branch tip after rebase (only set if Merged is true)
	Error      error  // Conflict or other error if rebase failed
}

// MergeAgentBranch rebases an agent's branch onto main and fast-forwards main to include it.
// If rebase succeeds, pushes to origin/main.
// If rebase fails due to conflicts, aborts and returns error (caller should rebase worktree).
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

	// Get the worktree path for this agent
	wtPath := p.getWorktreePathForAgent(agentID)
	if wtPath == "" {
		return nil, fmt.Errorf("worktree not found for agent %s", agentID)
	}

	// Fetch latest from origin
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoDir
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("fetch: %w\n%s", err, output)
	}

	// Rebase the agent's branch onto origin/main directly in the worktree.
	// No need to detach - the branch stays checked out in the worktree throughout.
	rebaseCmd := exec.Command("git", "rebase", "origin/main")
	rebaseCmd.Dir = wtPath
	rebaseOutput, rebaseErr := rebaseCmd.CombinedOutput()

	if rebaseErr != nil {
		// Rebase failed - abort and return error (worktree stays on its branch)
		abortCmd := exec.Command("git", "rebase", "--abort")
		abortCmd.Dir = wtPath
		_ = abortCmd.Run()

		return &MergeResult{
			Merged:     false,
			BranchName: branchName,
			Error:      fmt.Errorf("rebase conflict: %s", string(rebaseOutput)),
		}, nil
	}

	// Get the SHA of the rebased branch tip
	shaCmd := exec.Command("git", "rev-parse", "HEAD")
	shaCmd.Dir = wtPath
	shaOutput, shaErr := shaCmd.Output()
	sha := ""
	if shaErr == nil {
		sha = string(shaOutput)
		// Trim newline
		if len(sha) > 0 && sha[len(sha)-1] == '\n' {
			sha = sha[:len(sha)-1]
		}
	}

	// Fast-forward main to the rebased branch.
	// This works even though the branch is checked out in the worktree -
	// we're just moving the main ref, not checking out the branch.
	ffCmd := exec.Command("git", "merge", "--ff-only", branchName)
	ffCmd.Dir = repoDir
	if output, err := ffCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("fast-forward main: %w\n%s", err, output)
	}

	// Push to origin
	pushCmd := exec.Command("git", "push", "origin", "main")
	pushCmd.Dir = repoDir
	if output, err := pushCmd.CombinedOutput(); err != nil {
		// Rollback: reset main to origin/main
		resetCmd := exec.Command("git", "reset", "--hard", "origin/main")
		resetCmd.Dir = repoDir
		_ = resetCmd.Run()
		return nil, fmt.Errorf("push main: %w\n%s", err, output)
	}

	return &MergeResult{
		Merged:     true,
		BranchName: branchName,
		SHA:        sha,
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

// getWorktreePathForAgent returns the worktree path for the given agent, or empty string if not found.
func (p *Project) getWorktreePathForAgent(agentID string) string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	for _, wt := range p.Worktrees {
		if wt.AgentID == agentID {
			return wt.Path
		}
	}
	return ""
}
