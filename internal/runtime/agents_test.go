package runtime

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestStore_UpsertAndGet(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreWithPath(filepath.Join(tmpDir, "agents.json"))

	agent := AgentRuntime{
		ID:           "abc123",
		Project:      "test-project",
		Kind:         KindCoding,
		Backend:      "claude",
		PID:          12345,
		StartedAt:    time.Now(),
		WorktreePath: "/path/to/worktree",
		ThreadID:     "",
		LastState:    "starting",
		LastUpdate:   time.Now(),
	}

	// Insert
	if err := store.Upsert(agent); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Get
	got, err := store.Get("abc123")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.ID != agent.ID {
		t.Errorf("got ID %q, want %q", got.ID, agent.ID)
	}
	if got.Project != agent.Project {
		t.Errorf("got Project %q, want %q", got.Project, agent.Project)
	}
	if got.Kind != agent.Kind {
		t.Errorf("got Kind %q, want %q", got.Kind, agent.Kind)
	}
	if got.Backend != agent.Backend {
		t.Errorf("got Backend %q, want %q", got.Backend, agent.Backend)
	}
	if got.PID != agent.PID {
		t.Errorf("got PID %d, want %d", got.PID, agent.PID)
	}
	if got.WorktreePath != agent.WorktreePath {
		t.Errorf("got WorktreePath %q, want %q", got.WorktreePath, agent.WorktreePath)
	}
	if got.LastState != agent.LastState {
		t.Errorf("got LastState %q, want %q", got.LastState, agent.LastState)
	}
}

func TestStore_Update(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreWithPath(filepath.Join(tmpDir, "agents.json"))

	agent := AgentRuntime{
		ID:        "abc123",
		Project:   "test-project",
		Kind:      KindCoding,
		LastState: "starting",
	}

	// Insert
	if err := store.Upsert(agent); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Update
	agent.LastState = "running"
	agent.ThreadID = "thread-456"
	if err := store.Upsert(agent); err != nil {
		t.Fatalf("Update Upsert failed: %v", err)
	}

	// Verify update
	got, err := store.Get("abc123")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if got.LastState != "running" {
		t.Errorf("got LastState %q, want %q", got.LastState, "running")
	}
	if got.ThreadID != "thread-456" {
		t.Errorf("got ThreadID %q, want %q", got.ThreadID, "thread-456")
	}
}

func TestStore_Remove(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreWithPath(filepath.Join(tmpDir, "agents.json"))

	agent := AgentRuntime{
		ID:      "abc123",
		Project: "test-project",
		Kind:    KindCoding,
	}

	// Insert
	if err := store.Upsert(agent); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Remove
	if err := store.Remove("abc123"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}

	// Verify removed
	_, err := store.Get("abc123")
	if err != ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}

	// Remove non-existent (should not error)
	if err := store.Remove("nonexistent"); err != nil {
		t.Errorf("Remove non-existent should not error: %v", err)
	}
}

func TestStore_List(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreWithPath(filepath.Join(tmpDir, "agents.json"))

	// Insert multiple agents
	agents := []AgentRuntime{
		{ID: "agent1", Project: "project-a", Kind: KindCoding},
		{ID: "agent2", Project: "project-a", Kind: KindPlanner},
		{ID: "agent3", Project: "project-b", Kind: KindManager},
	}

	for _, a := range agents {
		if err := store.Upsert(a); err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	// List all
	all, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("got %d agents, want 3", len(all))
	}
}

func TestStore_ListByProject(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreWithPath(filepath.Join(tmpDir, "agents.json"))

	// Insert multiple agents
	agents := []AgentRuntime{
		{ID: "agent1", Project: "project-a", Kind: KindCoding},
		{ID: "agent2", Project: "project-a", Kind: KindPlanner},
		{ID: "agent3", Project: "project-b", Kind: KindManager},
	}

	for _, a := range agents {
		if err := store.Upsert(a); err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	// List by project
	projectA, err := store.ListByProject("project-a")
	if err != nil {
		t.Fatalf("ListByProject failed: %v", err)
	}
	if len(projectA) != 2 {
		t.Errorf("got %d agents for project-a, want 2", len(projectA))
	}

	projectB, err := store.ListByProject("project-b")
	if err != nil {
		t.Fatalf("ListByProject failed: %v", err)
	}
	if len(projectB) != 1 {
		t.Errorf("got %d agents for project-b, want 1", len(projectB))
	}
}

func TestStore_ListByKind(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreWithPath(filepath.Join(tmpDir, "agents.json"))

	// Insert multiple agents
	agents := []AgentRuntime{
		{ID: "agent1", Project: "project-a", Kind: KindCoding},
		{ID: "agent2", Project: "project-a", Kind: KindCoding},
		{ID: "agent3", Project: "project-b", Kind: KindPlanner},
		{ID: "agent4", Project: "project-b", Kind: KindManager},
	}

	for _, a := range agents {
		if err := store.Upsert(a); err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	// List by kind
	coding, err := store.ListByKind(KindCoding)
	if err != nil {
		t.Fatalf("ListByKind failed: %v", err)
	}
	if len(coding) != 2 {
		t.Errorf("got %d coding agents, want 2", len(coding))
	}

	planners, err := store.ListByKind(KindPlanner)
	if err != nil {
		t.Fatalf("ListByKind failed: %v", err)
	}
	if len(planners) != 1 {
		t.Errorf("got %d planner agents, want 1", len(planners))
	}
}

func TestStore_UpdateThreadID(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreWithPath(filepath.Join(tmpDir, "agents.json"))

	agent := AgentRuntime{
		ID:       "abc123",
		Project:  "test-project",
		Kind:     KindCoding,
		ThreadID: "",
	}

	if err := store.Upsert(agent); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Update thread ID
	if err := store.UpdateThreadID("abc123", "thread-789"); err != nil {
		t.Fatalf("UpdateThreadID failed: %v", err)
	}

	// Verify
	got, err := store.Get("abc123")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.ThreadID != "thread-789" {
		t.Errorf("got ThreadID %q, want %q", got.ThreadID, "thread-789")
	}

	// Update non-existent
	if err := store.UpdateThreadID("nonexistent", "thread"); err != ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}

func TestStore_UpdateState(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreWithPath(filepath.Join(tmpDir, "agents.json"))

	agent := AgentRuntime{
		ID:        "abc123",
		Project:   "test-project",
		Kind:      KindCoding,
		LastState: "starting",
	}

	if err := store.Upsert(agent); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Update state
	if err := store.UpdateState("abc123", "running"); err != nil {
		t.Fatalf("UpdateState failed: %v", err)
	}

	// Verify
	got, err := store.Get("abc123")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if got.LastState != "running" {
		t.Errorf("got LastState %q, want %q", got.LastState, "running")
	}

	// Update non-existent
	if err := store.UpdateState("nonexistent", "running"); err != ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}

func TestStore_Clear(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreWithPath(filepath.Join(tmpDir, "agents.json"))

	// Insert multiple agents
	agents := []AgentRuntime{
		{ID: "agent1", Project: "project-a", Kind: KindCoding},
		{ID: "agent2", Project: "project-a", Kind: KindPlanner},
	}

	for _, a := range agents {
		if err := store.Upsert(a); err != nil {
			t.Fatalf("Upsert failed: %v", err)
		}
	}

	// Clear
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	// Verify empty
	all, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("got %d agents after clear, want 0", len(all))
	}
}

func TestStore_NonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreWithPath(filepath.Join(tmpDir, "nonexistent", "agents.json"))

	// List should return empty (creates file)
	all, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("got %d agents, want 0", len(all))
	}

	// Get non-existent
	_, err = store.Get("abc123")
	if err != ErrAgentNotFound {
		t.Errorf("expected ErrAgentNotFound, got %v", err)
	}
}

func TestStore_AtomicWrite(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "agents.json")
	store := NewStoreWithPath(path)

	// Write initial data
	agent := AgentRuntime{
		ID:      "abc123",
		Project: "test-project",
		Kind:    KindCoding,
	}
	if err := store.Upsert(agent); err != nil {
		t.Fatalf("Upsert failed: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Error("expected file to exist")
	}

	// Verify temp file is cleaned up
	tmpFile := path + ".tmp"
	if _, err := os.Stat(tmpFile); !os.IsNotExist(err) {
		t.Error("expected temp file to be cleaned up")
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	store := NewStoreWithPath(filepath.Join(tmpDir, "agents.json"))

	const numGoroutines = 10
	const numOperations = 100

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()

			for j := 0; j < numOperations; j++ {
				agentID := "agent-" + string(rune('a'+id))
				agent := AgentRuntime{
					ID:        agentID,
					Project:   "test-project",
					Kind:      KindCoding,
					LastState: "running",
				}

				// Concurrent writes
				_ = store.Upsert(agent)

				// Concurrent reads
				_, _ = store.Get(agentID)

				// Concurrent state updates
				_ = store.UpdateState(agentID, "idle")

				// Concurrent thread ID updates
				_ = store.UpdateThreadID(agentID, "thread-xyz")

				// Concurrent list
				_, _ = store.List()
			}
		}(i)
	}

	wg.Wait()

	// Verify final state is consistent
	all, err := store.List()
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	// Should have numGoroutines agents
	if len(all) != numGoroutines {
		t.Errorf("got %d agents, want %d", len(all), numGoroutines)
	}
}

func TestStore_Path(t *testing.T) {
	path := "/some/path/agents.json"
	store := NewStoreWithPath(path)

	if got := store.Path(); got != path {
		t.Errorf("got path %q, want %q", got, path)
	}
}
