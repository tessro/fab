// Package runtime provides persistent runtime metadata storage.
package runtime

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tessro/fab/internal/paths"
)

// DedupEntry represents a processed event ID with its timestamp.
type DedupEntry struct {
	ID        string    `json:"id"`
	Project   string    `json:"project,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// DedupStore tracks processed event IDs to prevent duplicate processing.
// It maintains both in-memory state and optional persistent storage.
type DedupStore struct {
	mu   sync.Mutex
	path string

	// entries maps event IDs to their creation time
	// +checklocks:mu
	entries map[string]DedupEntry

	// maxAge is the maximum age of entries before they're eligible for cleanup
	maxAge time.Duration

	// maxEntries is the maximum number of entries to keep
	maxEntries int
}

// DefaultDedupMaxAge is the default maximum age for dedup entries.
const DefaultDedupMaxAge = 24 * time.Hour

// DefaultDedupMaxEntries is the default maximum number of entries.
const DefaultDedupMaxEntries = 10000

// NewDedupStore creates a new dedup store with optional persistence.
// If path is empty, the store is in-memory only.
func NewDedupStore(path string) *DedupStore {
	s := &DedupStore{
		path:       path,
		entries:    make(map[string]DedupEntry),
		maxAge:     DefaultDedupMaxAge,
		maxEntries: DefaultDedupMaxEntries,
	}

	// Load existing entries from disk if path is provided
	if path != "" {
		if err := s.load(); err != nil {
			slog.Debug("failed to load dedup store", "path", path, "error", err)
		}
	}

	return s
}

// NewDedupStoreDefault creates a dedup store using the default path.
func NewDedupStoreDefault() (*DedupStore, error) {
	path, err := DedupStorePath()
	if err != nil {
		return nil, err
	}
	return NewDedupStore(path), nil
}

// DedupStorePath returns the default path for the dedup store.
func DedupStorePath() (string, error) {
	dir, err := paths.RuntimeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dedup.json"), nil
}

// SetMaxAge sets the maximum age for entries.
func (s *DedupStore) SetMaxAge(d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxAge = d
}

// SetMaxEntries sets the maximum number of entries.
func (s *DedupStore) SetMaxEntries(n int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.maxEntries = n
}

// Seen returns true if the event ID has been seen (processed).
// This does NOT mark the ID as processed.
func (s *DedupStore) Seen(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, exists := s.entries[id]
	return exists
}

// Mark records an event ID as processed.
// Returns true if this is a new ID, false if it was already processed.
func (s *DedupStore) Mark(id, project string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.entries[id]; exists {
		return false
	}

	s.entries[id] = DedupEntry{
		ID:        id,
		Project:   project,
		CreatedAt: time.Now(),
	}

	// Cleanup if we exceed maxEntries
	if len(s.entries) > s.maxEntries {
		s.cleanupLocked()
	}

	// Persist if we have a path
	if s.path != "" {
		if err := s.saveLocked(); err != nil {
			slog.Debug("failed to save dedup store", "path", s.path, "error", err)
		}
	}

	return true
}

// Cleanup removes entries older than maxAge.
func (s *DedupStore) Cleanup() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.cleanupLocked()
}

// cleanupLocked removes old entries. Must be called with mu held.
func (s *DedupStore) cleanupLocked() int {
	cutoff := time.Now().Add(-s.maxAge)
	removed := 0

	for id, entry := range s.entries {
		if entry.CreatedAt.Before(cutoff) {
			delete(s.entries, id)
			removed++
		}
	}

	// If still over limit, remove oldest entries
	if len(s.entries) > s.maxEntries {
		// Find oldest entries to remove
		excess := len(s.entries) - s.maxEntries
		type aged struct {
			id  string
			age time.Time
		}
		var oldest []aged
		for id, entry := range s.entries {
			oldest = append(oldest, aged{id, entry.CreatedAt})
		}

		// Simple selection of oldest (not most efficient but simple)
		for i := 0; i < excess; i++ {
			oldestIdx := 0
			for j := 1; j < len(oldest); j++ {
				if oldest[j].age.Before(oldest[oldestIdx].age) {
					oldestIdx = j
				}
			}
			delete(s.entries, oldest[oldestIdx].id)
			oldest = append(oldest[:oldestIdx], oldest[oldestIdx+1:]...)
			removed++
		}
	}

	return removed
}

// Count returns the number of tracked event IDs.
func (s *DedupStore) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}

// Clear removes all entries.
func (s *DedupStore) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.entries = make(map[string]DedupEntry)

	if s.path != "" {
		return s.saveLocked()
	}
	return nil
}

// load reads entries from disk. Must be called with mu held.
func (s *DedupStore) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read dedup file: %w", err)
	}

	if len(data) == 0 {
		return nil
	}

	var entries []DedupEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return fmt.Errorf("parse dedup file: %w", err)
	}

	// Convert to map
	s.entries = make(map[string]DedupEntry, len(entries))
	for _, e := range entries {
		s.entries[e.ID] = e
	}

	// Cleanup old entries on load
	s.cleanupLocked()

	return nil
}

// saveLocked writes entries to disk. Must be called with mu held.
func (s *DedupStore) saveLocked() error {
	if s.path == "" {
		return nil
	}

	// Ensure directory exists
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("create dedup dir: %w", err)
	}

	// Convert map to slice for JSON
	entries := make([]DedupEntry, 0, len(s.entries))
	for _, e := range s.entries {
		entries = append(entries, e)
	}

	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dedup entries: %w", err)
	}

	// Write to temp file then rename for atomicity
	tmpFile := s.path + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return fmt.Errorf("write temp file: %w", err)
	}

	if err := os.Rename(tmpFile, s.path); err != nil {
		os.Remove(tmpFile)
		return fmt.Errorf("rename temp file: %w", err)
	}

	return nil
}
