package tk

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
)

// commitAndPush stages ticket changes, commits, and pushes to origin.
func (b *Backend) commitAndPush(message string) error {
	// Acquire file lock to prevent concurrent commits
	lockPath := filepath.Join(b.ticketsDir, ".lock")
	lock, err := acquireLock(lockPath)
	if err != nil {
		return fmt.Errorf("acquire lock: %w", err)
	}
	defer lock.release()

	// Stage ticket changes
	addCmd := exec.Command("git", "add", b.ticketsDir)
	addCmd.Dir = b.repoDir
	if output, err := addCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git add: %w\n%s", err, output)
	}

	// Check if there are changes to commit
	diffCmd := exec.Command("git", "diff", "--cached", "--quiet")
	diffCmd.Dir = b.repoDir
	if err := diffCmd.Run(); err == nil {
		// No changes to commit
		return nil
	}

	// Commit
	commitCmd := exec.Command("git", "commit", "-m", message)
	commitCmd.Dir = b.repoDir
	if output, err := commitCmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git commit: %w\n%s", err, output)
	}

	// Push to origin
	pushCmd := exec.Command("git", "push", "origin", "HEAD")
	pushCmd.Dir = b.repoDir
	if output, err := pushCmd.CombinedOutput(); err != nil {
		// Rollback the commit on push failure
		resetCmd := exec.Command("git", "reset", "--soft", "HEAD~1")
		resetCmd.Dir = b.repoDir
		_ = resetCmd.Run()
		return fmt.Errorf("git push: %w\n%s", err, output)
	}

	return nil
}

// fileLock provides simple file-based locking.
type fileLock struct {
	file *os.File
}

// acquireLock creates a file lock.
func acquireLock(path string) (*fileLock, error) {
	// Ensure directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		return nil, err
	}

	// Try to acquire exclusive lock
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, fmt.Errorf("flock: %w", err)
	}

	return &fileLock{file: file}, nil
}

// release releases the file lock.
func (l *fileLock) release() {
	if l.file != nil {
		_ = syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
		l.file.Close()
	}
}
