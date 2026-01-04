// Package agent provides the Agent type and lifecycle management for Claude Code instances.
package agent

import (
	"sync"
)

// DefaultChatHistorySize is the default number of chat entries to retain.
const DefaultChatHistorySize = 1000

// ChatHistory stores parsed chat messages in a circular buffer.
type ChatHistory struct {
	// +checklocks:mu
	entries []ChatEntry
	maxSize int // Maximum number of entries (immutable after creation)
	// +checklocks:mu
	head int // Next write position
	// +checklocks:mu
	count int // Current number of entries stored
	mu    sync.RWMutex
}

// NewChatHistory creates a new chat history with the given max size.
// If maxSize <= 0, DefaultChatHistorySize is used.
func NewChatHistory(maxSize int) *ChatHistory {
	if maxSize <= 0 {
		maxSize = DefaultChatHistorySize
	}
	return &ChatHistory{
		entries: make([]ChatEntry, maxSize),
		maxSize: maxSize,
	}
}

// Add appends a chat entry, evicting oldest if at capacity.
func (h *ChatHistory) Add(entry ChatEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries[h.head] = entry
	h.head = (h.head + 1) % h.maxSize
	if h.count < h.maxSize {
		h.count++
	}
}

// Entries returns the last n entries (or all if n <= 0).
// Entries are returned in chronological order (oldest first).
func (h *ChatHistory) Entries(n int) []ChatEntry {
	h.mu.RLock()
	defer h.mu.RUnlock()

	if n <= 0 || n > h.count {
		n = h.count
	}
	if n == 0 {
		return nil
	}

	result := make([]ChatEntry, n)

	// Calculate starting position for the requested entries
	// head points to next write position, so oldest is at head (if full)
	// or at 0 (if not full)
	start := 0
	if h.count == h.maxSize {
		// Buffer is full, oldest entry is at head
		start = (h.head - n + h.maxSize) % h.maxSize
	} else {
		// Buffer not full, entries start at 0
		start = h.count - n
	}

	for i := 0; i < n; i++ {
		idx := (start + i) % h.maxSize
		result[i] = h.entries[idx]
	}

	return result
}

// All returns all entries in chronological order.
func (h *ChatHistory) All() []ChatEntry {
	return h.Entries(-1)
}

// Len returns the current number of entries.
func (h *ChatHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.count
}

// Clear removes all entries.
func (h *ChatHistory) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := range h.entries {
		h.entries[i] = ChatEntry{}
	}
	h.head = 0
	h.count = 0
}
