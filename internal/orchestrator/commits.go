package orchestrator

import (
	"sync"
	"time"
)

// CommitRecord represents a successfully merged commit from an agent.
type CommitRecord struct {
	// SHA is the commit hash of the merge commit.
	SHA string

	// Branch is the agent branch that was merged (e.g., "fab/abc123").
	Branch string

	// AgentID is the agent that created the commits.
	AgentID string

	// TaskID is the ticket the agent was working on, if known.
	TaskID string

	// Description is the agent's description at the time of merge.
	Description string

	// MergedAt is when the merge occurred.
	MergedAt time.Time
}

// CommitLog stores successfully merged commits for a project.
// It is a bounded log that keeps the most recent commits.
type CommitLog struct {
	// +checklocks:mu
	commits []CommitRecord
	maxSize int
	mu      sync.RWMutex
}

// DefaultCommitLogSize is the default number of commits to keep.
const DefaultCommitLogSize = 100

// NewCommitLog creates a new commit log with the specified max size.
func NewCommitLog(maxSize int) *CommitLog {
	if maxSize <= 0 {
		maxSize = DefaultCommitLogSize
	}
	return &CommitLog{
		commits: make([]CommitRecord, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add records a new commit. If the log is at capacity, the oldest entry is removed.
func (l *CommitLog) Add(record CommitRecord) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Remove oldest if at capacity
	if len(l.commits) >= l.maxSize {
		copy(l.commits, l.commits[1:])
		l.commits = l.commits[:len(l.commits)-1]
	}

	l.commits = append(l.commits, record)
}

// List returns all commits in the log, oldest first.
func (l *CommitLog) List() []CommitRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()

	result := make([]CommitRecord, len(l.commits))
	copy(result, l.commits)
	return result
}

// ListRecent returns the n most recent commits, newest first.
func (l *CommitLog) ListRecent(n int) []CommitRecord {
	l.mu.RLock()
	defer l.mu.RUnlock()

	count := n
	if count <= 0 || count > len(l.commits) {
		count = len(l.commits)
	}

	// Build result in reverse order (newest first)
	result := make([]CommitRecord, count)
	for i := 0; i < count; i++ {
		result[i] = l.commits[len(l.commits)-1-i]
	}
	return result
}

// Len returns the number of commits in the log.
func (l *CommitLog) Len() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.commits)
}
