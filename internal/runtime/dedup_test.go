package runtime

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDedupStore_Mark(t *testing.T) {
	// Test in-memory store
	store := NewDedupStore("")

	// First mark should return true (new)
	if !store.Mark("event-1", "project-a") {
		t.Error("first Mark should return true")
	}

	// Second mark of same ID should return false (duplicate)
	if store.Mark("event-1", "project-a") {
		t.Error("duplicate Mark should return false")
	}

	// Different ID should return true
	if !store.Mark("event-2", "project-a") {
		t.Error("new ID Mark should return true")
	}

	// Check count
	if count := store.Count(); count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
}

func TestDedupStore_Seen(t *testing.T) {
	store := NewDedupStore("")

	// Should not be seen before marking
	if store.Seen("event-1") {
		t.Error("event should not be seen before marking")
	}

	// Mark the event
	store.Mark("event-1", "project-a")

	// Should be seen after marking
	if !store.Seen("event-1") {
		t.Error("event should be seen after marking")
	}

	// Different event should not be seen
	if store.Seen("event-2") {
		t.Error("different event should not be seen")
	}
}

func TestDedupStore_Cleanup(t *testing.T) {
	store := NewDedupStore("")
	store.SetMaxAge(50 * time.Millisecond)

	// Add an entry
	store.Mark("event-1", "project-a")

	// Should be visible immediately
	if !store.Seen("event-1") {
		t.Error("event should be visible immediately")
	}

	// Wait for expiration
	time.Sleep(60 * time.Millisecond)

	// Cleanup should remove it
	removed := store.Cleanup()
	if removed != 1 {
		t.Errorf("expected 1 removed, got %d", removed)
	}

	// Should no longer be seen
	if store.Seen("event-1") {
		t.Error("event should not be seen after cleanup")
	}
}

func TestDedupStore_MaxEntries(t *testing.T) {
	store := NewDedupStore("")
	store.SetMaxEntries(3)
	store.SetMaxAge(24 * time.Hour) // Long age to prevent age-based cleanup

	// Add 4 entries (exceeds max of 3)
	for i := 0; i < 4; i++ {
		time.Sleep(10 * time.Millisecond) // Ensure different timestamps
		store.Mark("event-"+string(rune('a'+i)), "project")
	}

	// Should have at most 3 entries
	if count := store.Count(); count > 3 {
		t.Errorf("expected at most 3 entries, got %d", count)
	}
}

func TestDedupStore_Clear(t *testing.T) {
	store := NewDedupStore("")

	// Add some entries
	store.Mark("event-1", "project-a")
	store.Mark("event-2", "project-b")

	// Clear
	if err := store.Clear(); err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	// Should have no entries
	if count := store.Count(); count != 0 {
		t.Errorf("expected 0 entries after clear, got %d", count)
	}

	// Events should no longer be seen
	if store.Seen("event-1") || store.Seen("event-2") {
		t.Error("events should not be seen after clear")
	}
}

func TestDedupStore_Persistence(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "dedup-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "dedup.json")

	// Create store and add entries
	store1 := NewDedupStore(path)
	store1.Mark("event-1", "project-a")
	store1.Mark("event-2", "project-b")

	// Create new store from same path (simulates daemon restart)
	store2 := NewDedupStore(path)

	// Should have loaded the entries
	if count := store2.Count(); count != 2 {
		t.Errorf("expected 2 entries after reload, got %d", count)
	}

	// Events should be seen
	if !store2.Seen("event-1") {
		t.Error("event-1 should be seen after reload")
	}
	if !store2.Seen("event-2") {
		t.Error("event-2 should be seen after reload")
	}

	// New marks should be rejected
	if store2.Mark("event-1", "project-a") {
		t.Error("duplicate Mark should return false after reload")
	}
}

func TestDedupStore_CleanupOnLoad(t *testing.T) {
	// Create a temp directory
	tmpDir, err := os.MkdirTemp("", "dedup-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	path := filepath.Join(tmpDir, "dedup.json")

	// Create store with short max age and add entries
	store1 := NewDedupStore(path)
	store1.SetMaxAge(50 * time.Millisecond)
	store1.Mark("event-1", "project-a")

	// Wait for entry to expire
	time.Sleep(60 * time.Millisecond)

	// Create new store with same short max age
	// Note: The store uses default maxAge on load, so we set it and trigger cleanup
	store2 := NewDedupStore(path)
	store2.SetMaxAge(50 * time.Millisecond)

	// Trigger cleanup explicitly after setting short maxAge
	store2.Cleanup()

	// Entry should have been cleaned up
	if store2.Seen("event-1") {
		t.Error("expired event should be cleaned up after explicit cleanup")
	}
}

func TestDedupStore_ConcurrentAccess(t *testing.T) {
	store := NewDedupStore("")

	// Run concurrent marks
	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(n int) {
			for j := 0; j < 100; j++ {
				store.Mark("event-"+string(rune('a'+n))+"-"+string(rune('0'+j%10)), "project")
			}
			done <- true
		}(i)
	}

	// Wait for all goroutines
	for i := 0; i < 10; i++ {
		<-done
	}

	// Store should still be consistent
	if count := store.Count(); count == 0 {
		t.Error("store should have entries after concurrent access")
	}
}
