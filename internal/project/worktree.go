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

	// Create the .fab-worktrees directory
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

	// Remove the .fab-worktrees directory if empty
	wtDir := filepath.Join(p.Path, ".fab-worktrees")
	if entries, err := os.ReadDir(wtDir); err == nil && len(entries) == 0 {
		os.Remove(wtDir)
	}

	// Prune stale worktree references
	cmd := exec.Command("git", "worktree", "prune")
	cmd.Dir = p.Path
	cmd.Run() // Ignore errors from prune

	return lastErr
}
