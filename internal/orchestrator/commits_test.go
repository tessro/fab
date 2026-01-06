package orchestrator

import (
	"testing"
	"time"
)

func TestCommitLog_Add(t *testing.T) {
	log := NewCommitLog(3)

	// Add first commit
	log.Add(CommitRecord{SHA: "abc123", Branch: "fab/agent1", AgentID: "agent1", MergedAt: time.Now()})
	if log.Len() != 1 {
		t.Errorf("expected 1 commit, got %d", log.Len())
	}

	// Add more commits
	log.Add(CommitRecord{SHA: "def456", Branch: "fab/agent2", AgentID: "agent2", MergedAt: time.Now()})
	log.Add(CommitRecord{SHA: "ghi789", Branch: "fab/agent3", AgentID: "agent3", MergedAt: time.Now()})
	if log.Len() != 3 {
		t.Errorf("expected 3 commits, got %d", log.Len())
	}

	// Adding 4th commit should evict oldest
	log.Add(CommitRecord{SHA: "jkl012", Branch: "fab/agent4", AgentID: "agent4", MergedAt: time.Now()})
	if log.Len() != 3 {
		t.Errorf("expected 3 commits (max size), got %d", log.Len())
	}

	// Verify oldest was removed
	commits := log.List()
	if commits[0].SHA != "def456" {
		t.Errorf("expected oldest to be def456, got %s", commits[0].SHA)
	}
	if commits[2].SHA != "jkl012" {
		t.Errorf("expected newest to be jkl012, got %s", commits[2].SHA)
	}
}

func TestCommitLog_List(t *testing.T) {
	log := NewCommitLog(10)

	log.Add(CommitRecord{SHA: "abc123", Branch: "fab/agent1", AgentID: "agent1", MergedAt: time.Now()})
	log.Add(CommitRecord{SHA: "def456", Branch: "fab/agent2", AgentID: "agent2", MergedAt: time.Now()})

	commits := log.List()
	if len(commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(commits))
	}

	// Verify order (oldest first)
	if commits[0].SHA != "abc123" {
		t.Errorf("expected first to be abc123, got %s", commits[0].SHA)
	}
	if commits[1].SHA != "def456" {
		t.Errorf("expected second to be def456, got %s", commits[1].SHA)
	}
}

func TestCommitLog_ListRecent(t *testing.T) {
	log := NewCommitLog(10)

	log.Add(CommitRecord{SHA: "abc123", Branch: "fab/agent1", AgentID: "agent1", MergedAt: time.Now()})
	log.Add(CommitRecord{SHA: "def456", Branch: "fab/agent2", AgentID: "agent2", MergedAt: time.Now()})
	log.Add(CommitRecord{SHA: "ghi789", Branch: "fab/agent3", AgentID: "agent3", MergedAt: time.Now()})

	// Get last 2
	commits := log.ListRecent(2)
	if len(commits) != 2 {
		t.Errorf("expected 2 commits, got %d", len(commits))
	}

	if commits[0].SHA != "def456" {
		t.Errorf("expected first to be def456, got %s", commits[0].SHA)
	}
	if commits[1].SHA != "ghi789" {
		t.Errorf("expected second to be ghi789, got %s", commits[1].SHA)
	}

	// Request more than exists
	commits = log.ListRecent(10)
	if len(commits) != 3 {
		t.Errorf("expected 3 commits, got %d", len(commits))
	}

	// Request zero or negative returns all
	commits = log.ListRecent(0)
	if len(commits) != 3 {
		t.Errorf("expected 3 commits for n=0, got %d", len(commits))
	}
}

func TestCommitLog_DefaultSize(t *testing.T) {
	log := NewCommitLog(0)
	if log.maxSize != DefaultCommitLogSize {
		t.Errorf("expected default size %d, got %d", DefaultCommitLogSize, log.maxSize)
	}
}

func TestCommitLog_TaskID(t *testing.T) {
	log := NewCommitLog(10)

	log.Add(CommitRecord{
		SHA:      "abc123",
		Branch:   "fab/agent1",
		AgentID:  "agent1",
		TaskID:   "fa-123",
		MergedAt: time.Now(),
	})

	commits := log.List()
	if commits[0].TaskID != "fa-123" {
		t.Errorf("expected TaskID fa-123, got %s", commits[0].TaskID)
	}
}
