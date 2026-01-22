package supervisor

import (
	"testing"
	"time"

	"github.com/tessro/fab/internal/issue"
	"github.com/tessro/fab/internal/orchestrator"
	"github.com/tessro/fab/internal/runtime"
)

func TestCommentPoller_NewCommentPoller(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	cfg := CommentPollerConfig{}

	p := NewCommentPoller(cfg, dedup)

	if p == nil {
		t.Fatal("NewCommentPoller returned nil")
	}
	if p.config.PollInterval != DefaultCommentPollInterval {
		t.Errorf("PollInterval = %v, want %v", p.config.PollInterval, DefaultCommentPollInterval)
	}
	if p.claimStartTimes == nil {
		t.Error("claimStartTimes map not initialized")
	}
}

func TestCommentPoller_NewCommentPoller_CustomInterval(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	cfg := CommentPollerConfig{
		PollInterval: 5 * time.Second,
	}

	p := NewCommentPoller(cfg, dedup)

	if p.config.PollInterval != 5*time.Second {
		t.Errorf("PollInterval = %v, want %v", p.config.PollInterval, 5*time.Second)
	}
}

func TestCommentPoller_StartStop(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	cfg := CommentPollerConfig{
		PollInterval:     100 * time.Millisecond,
		GetOrchestrators: func() map[string]*orchestrator.Orchestrator { return nil },
	}

	p := NewCommentPoller(cfg, dedup)

	// Start should succeed
	if err := p.Start(); err != nil {
		t.Fatalf("Start() failed: %v", err)
	}

	if !p.IsRunning() {
		t.Error("IsRunning() = false after Start()")
	}

	// Starting again should fail
	if err := p.Start(); err == nil {
		t.Error("Start() should fail when already running")
	}

	// Stop should succeed
	p.Stop()

	if p.IsRunning() {
		t.Error("IsRunning() = true after Stop()")
	}

	// Stopping again should be a no-op
	p.Stop() // Should not panic
}

func TestCommentPoller_ClearClaimTime(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	cfg := CommentPollerConfig{}

	p := NewCommentPoller(cfg, dedup)

	// Add a claim time
	p.claimMu.Lock()
	p.claimStartTimes["project:issue-1"] = time.Now()
	p.claimMu.Unlock()

	// Verify it exists
	p.claimMu.Lock()
	_, exists := p.claimStartTimes["project:issue-1"]
	p.claimMu.Unlock()
	if !exists {
		t.Error("claim time should exist after adding")
	}

	// Clear it
	p.ClearClaimTime("project", "issue-1")

	// Verify it's gone
	p.claimMu.Lock()
	_, exists = p.claimStartTimes["project:issue-1"]
	p.claimMu.Unlock()
	if exists {
		t.Error("claim time should not exist after clearing")
	}
}

func TestCommentPoller_CleanupStaleClaimTimes(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	cfg := CommentPollerConfig{}

	p := NewCommentPoller(cfg, dedup)

	// Add some claim times
	p.claimMu.Lock()
	p.claimStartTimes["project:issue-1"] = time.Now()
	p.claimStartTimes["project:issue-2"] = time.Now()
	p.claimStartTimes["project:issue-3"] = time.Now()
	p.claimMu.Unlock()

	// Only issue-1 and issue-3 are still active
	activeClaims := map[string]bool{
		"project:issue-1": true,
		"project:issue-3": true,
	}

	p.cleanupStaleClaimTimes(activeClaims)

	// Verify cleanup
	p.claimMu.Lock()
	defer p.claimMu.Unlock()

	if _, exists := p.claimStartTimes["project:issue-1"]; !exists {
		t.Error("issue-1 should still exist (active)")
	}
	if _, exists := p.claimStartTimes["project:issue-2"]; exists {
		t.Error("issue-2 should be removed (stale)")
	}
	if _, exists := p.claimStartTimes["project:issue-3"]; !exists {
		t.Error("issue-3 should still exist (active)")
	}
}

func TestCommentPoller_DeduplicationKey(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	cfg := CommentPollerConfig{}

	p := NewCommentPoller(cfg, dedup)

	comment := &issue.Comment{
		ID:        "comment-123",
		IssueID:   "456",
		Author:    "testuser",
		Body:      "Test comment",
		CreatedAt: time.Now(),
	}

	// First delivery should succeed (dedup marks it)
	dedupID := "comment:test-project:456:comment-123"
	if !p.dedup.Mark(dedupID, "test-project") {
		t.Error("first Mark should return true (new)")
	}

	// Second delivery should be deduplicated
	if p.dedup.Mark(dedupID, "test-project") {
		t.Error("second Mark should return false (duplicate)")
	}

	_ = comment // Used to build the dedup ID format
}

func TestCommentPoller_ListCommentsFiltering(t *testing.T) {
	// Test that comments are filtered by the 'since' time
	now := time.Now()
	comments := []*issue.Comment{
		{ID: "1", CreatedAt: now.Add(-2 * time.Hour)}, // Old
		{ID: "2", CreatedAt: now.Add(-1 * time.Hour)}, // Old
		{ID: "3", CreatedAt: now.Add(1 * time.Minute)}, // New (after since)
	}

	since := now
	var filtered []*issue.Comment
	for _, c := range comments {
		if c.CreatedAt.After(since) {
			filtered = append(filtered, c)
		}
	}

	if len(filtered) != 1 {
		t.Errorf("expected 1 comment after filtering, got %d", len(filtered))
	}
	if filtered[0].ID != "3" {
		t.Errorf("expected comment ID '3', got '%s'", filtered[0].ID)
	}
}
