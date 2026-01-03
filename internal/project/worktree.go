// Package project provides worktree management for supervised coding projects.
package project

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

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
		cmd.Dir = p.Path
		if output, err := cmd.CombinedOutput(); err != nil {
			// Clean up any worktrees we created
			p.cleanupWorktrees()
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

	// Verify the project path is a valid git repository before attempting worktree operations
	gitDir := filepath.Join(p.Path, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		// Not a git repo - skip recreation (likely a test scenario)
		return nil
	}

	// Prune stale worktree references first (in case git still has a ref to the deleted path)
	pruneCmd := exec.Command("git", "worktree", "prune")
	pruneCmd.Dir = p.Path
	pruneCmd.Run() // Ignore errors from prune

	// Recreate the git worktree with detached HEAD
	cmd := exec.Command("git", "worktree", "add", "--detach", wtPath)
	cmd.Dir = p.Path
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("recreate worktree %s: %w\n%s", wtPath, err, output)
	}

	return nil
}

// cleanupWorktrees removes all worktrees. Must be called with lock held.
func (p *Project) cleanupWorktrees() error {
	var lastErr error

	for _, wt := range p.Worktrees {
		// Remove git worktree
		cmd := exec.Command("git", "worktree", "remove", "--force", wt.Path)
		cmd.Dir = p.Path
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
		os.Remove(wtDir)
	}

	// Prune stale worktree references
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = p.Path
	cmd.Run() // Ignore errors from prune

	return lastErr
}
