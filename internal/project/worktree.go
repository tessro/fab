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

	// Detach the worktree from its branch so we can checkout the branch in the repo.
	// Git doesn't allow the same branch to be checked out in multiple places.
	if wtPath := p.getWorktreePathForAgent(agentID); wtPath != "" {
		detachCmd := exec.Command("git", "checkout", "--detach", "HEAD")
		detachCmd.Dir = wtPath
		if output, err := detachCmd.CombinedOutput(); err != nil {
			return nil, fmt.Errorf("detach worktree: %w\n%s", err, output)
		}
	}

	// Fetch latest from origin
	fetchCmd := exec.Command("git", "fetch", "origin")
	fetchCmd.Dir = repoDir
	if output, err := fetchCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("fetch: %w\n%s", err, output)
	}

	// Checkout the agent's branch to rebase it
	checkoutBranchCmd := exec.Command("git", "checkout", branchName)
	checkoutBranchCmd.Dir = repoDir
	if output, err := checkoutBranchCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("checkout %s: %w\n%s", branchName, err, output)
	}

	// Rebase the agent's branch onto origin/main
	rebaseCmd := exec.Command("git", "rebase", "origin/main")
	rebaseCmd.Dir = repoDir
	rebaseOutput, rebaseErr := rebaseCmd.CombinedOutput()

	if rebaseErr != nil {
		// Rebase failed - abort and return error
		abortCmd := exec.Command("git", "rebase", "--abort")
		abortCmd.Dir = repoDir
		_ = abortCmd.Run()

		return &MergeResult{
			Merged:     false,
			BranchName: branchName,
			Error:      fmt.Errorf("rebase conflict: %s", string(rebaseOutput)),
		}, nil
	}

	// Get the SHA of the rebased branch tip before fast-forwarding main
	shaCmd := exec.Command("git", "rev-parse", "HEAD")
	shaCmd.Dir = repoDir
	shaOutput, shaErr := shaCmd.Output()
	sha := ""
	if shaErr == nil {
		sha = string(shaOutput)
		// Trim newline
		if len(sha) > 0 && sha[len(sha)-1] == '\n' {
			sha = sha[:len(sha)-1]
		}
	}

	// Checkout main and fast-forward to the rebased branch
	checkoutMainCmd := exec.Command("git", "checkout", "main")
	checkoutMainCmd.Dir = repoDir
	if output, err := checkoutMainCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("checkout main: %w\n%s", err, output)
	}

	// Fast-forward main to the rebased branch
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

// ResizeWorktreePool adjusts the worktree pool to match the target size.
// If target > current, creates new worktrees.
// If target < current, removes unused worktrees (those with InUse=false).
// Returns ErrWorktreeInUse if trying to shrink below the number of in-use worktrees.
func (p *Project) ResizeWorktreePool(targetSize int) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	currentSize := len(p.Worktrees)

	if targetSize == currentSize {
		return nil
	}

	if targetSize > currentSize {
		// Grow: create additional worktrees
		return p.growPool(targetSize)
	}

	// Shrink: remove unused worktrees
	return p.shrinkPool(targetSize)
}

// growPool adds worktrees to reach the target size.
// Must be called with lock held.
func (p *Project) growPool(targetSize int) error {
	wtDir := p.WorktreesDir()

	// Ensure worktrees directory exists
	if err := os.MkdirAll(wtDir, 0755); err != nil {
		return fmt.Errorf("create worktrees directory: %w", err)
	}

	repoDir := p.RepoDir()
	gitDir := filepath.Join(repoDir, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Not a git repo - skip (likely a test scenario)
		return nil
	}

	// Find the highest existing worktree number
	maxNum := 0
	for _, wt := range p.Worktrees {
		var num int
		base := filepath.Base(wt.Path)
		if _, err := fmt.Sscanf(base, "wt-%03d", &num); err == nil && num > maxNum {
			maxNum = num
		}
	}

	// Create new worktrees starting from maxNum+1
	for i := len(p.Worktrees); i < targetSize; i++ {
		maxNum++
		wtPath := filepath.Join(wtDir, fmt.Sprintf("wt-%03d", maxNum))

		// Check if worktree already exists on disk
		if _, err := os.Stat(wtPath); err == nil {
			p.Worktrees = append(p.Worktrees, Worktree{Path: wtPath})
			continue
		}

		// Create git worktree with detached HEAD
		cmd := exec.Command("git", "worktree", "add", "--detach", wtPath)
		cmd.Dir = repoDir
		if output, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("create worktree %s: %w\n%s", wtPath, err, output)
		}

		p.Worktrees = append(p.Worktrees, Worktree{Path: wtPath})
	}

	return nil
}

// shrinkPool removes unused worktrees to reach the target size.
// Must be called with lock held.
// Returns error if target would require removing in-use worktrees.
func (p *Project) shrinkPool(targetSize int) error {
	// Count in-use worktrees
	inUseCount := 0
	for _, wt := range p.Worktrees {
		if wt.InUse {
			inUseCount++
		}
	}

	// Cannot shrink below number of in-use worktrees
	if targetSize < inUseCount {
		return fmt.Errorf("cannot resize: %d worktrees in use, target size is %d", inUseCount, targetSize)
	}

	repoDir := p.RepoDir()
	toRemove := len(p.Worktrees) - targetSize

	// Remove unused worktrees from the end
	newWorktrees := make([]Worktree, 0, targetSize)
	removed := 0

	// First pass: keep all in-use worktrees and some unused ones
	for i := range p.Worktrees {
		if p.Worktrees[i].InUse {
			newWorktrees = append(newWorktrees, p.Worktrees[i])
		} else if removed < toRemove {
			// Remove this worktree
			wtPath := p.Worktrees[i].Path

			// Try git worktree remove first
			cmd := exec.Command("git", "worktree", "remove", "--force", wtPath)
			cmd.Dir = repoDir
			if err := cmd.Run(); err != nil {
				// Fall back to manual removal
				_ = os.RemoveAll(wtPath)
			}
			removed++
		} else {
			newWorktrees = append(newWorktrees, p.Worktrees[i])
		}
	}

	p.Worktrees = newWorktrees

	// Prune stale worktree references
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = repoDir
	_ = cmd.Run()

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
