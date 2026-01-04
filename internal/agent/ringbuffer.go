// Package agent provides the Agent type and lifecycle management for Claude Code instances.
package agent

import (
	"bytes"
	"sync"
)

// DefaultBufferSize is the default number of lines to retain.
const DefaultBufferSize = 10000

// RingBuffer is a thread-safe circular buffer for storing terminal output lines.
// It stores a fixed number of lines and overwrites the oldest when full.
type RingBuffer struct {
	// +checklocks:mu
	lines [][]byte // Circular buffer of lines
	size  int      // Maximum number of lines (immutable after creation)
	// +checklocks:mu
	head int // Next write position
	// +checklocks:mu
	count int // Current number of lines stored
	// +checklocks:mu
	partial []byte // Incomplete line being accumulated
	mu      sync.RWMutex
	// +checklocks:mu
	totalIn int64 // Total bytes written (for stats)
	// +checklocks:mu
	totalOut int64 // Total bytes read (for stats)
}

// NewRingBuffer creates a ring buffer with the specified capacity.
// If size <= 0, DefaultBufferSize is used.
func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = DefaultBufferSize
	}
	return &RingBuffer{
		lines:   make([][]byte, size),
		size:    size,
		partial: make([]byte, 0, 256),
	}
}

// Write appends data to the buffer, splitting on newlines.
// Implements io.Writer.
func (rb *RingBuffer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}

	rb.mu.Lock()
	defer rb.mu.Unlock()

	rb.totalIn += int64(len(p))

	// Process bytes, accumulating partial lines
	for _, b := range p {
		if b == '\n' {
			// Complete line - store it
			rb.storeLine(rb.partial)
			rb.partial = rb.partial[:0]
		} else {
			rb.partial = append(rb.partial, b)
		}
	}

	return len(p), nil
}

// storeLine adds a completed line to the buffer.
//
// +checklocks:rb.mu
func (rb *RingBuffer) storeLine(line []byte) {
	// Make a copy to avoid retaining references
	stored := make([]byte, len(line))
	copy(stored, line)

	rb.lines[rb.head] = stored
	rb.head = (rb.head + 1) % rb.size
	if rb.count < rb.size {
		rb.count++
	}
}

// Flush forces any partial line to be stored as a complete line.
// Useful when the stream ends without a trailing newline.
func (rb *RingBuffer) Flush() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	if len(rb.partial) > 0 {
		rb.storeLine(rb.partial)
		rb.partial = rb.partial[:0]
	}
}

// Lines returns the last n lines from the buffer.
// If n <= 0 or n > count, returns all stored lines.
// Lines are returned in chronological order (oldest first).
func (rb *RingBuffer) Lines(n int) [][]byte {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if n <= 0 || n > rb.count {
		n = rb.count
	}
	if n == 0 {
		return nil
	}

	result := make([][]byte, n)

	// Calculate starting position for the requested lines
	// head points to next write position, so oldest is at head (if full)
	// or at 0 (if not full)
	start := 0
	if rb.count == rb.size {
		// Buffer is full, oldest line is at head
		start = (rb.head - n + rb.size) % rb.size
	} else {
		// Buffer not full, lines start at 0
		start = rb.count - n
	}

	for i := 0; i < n; i++ {
		idx := (start + i) % rb.size
		// Return copies to prevent external modification
		line := make([]byte, len(rb.lines[idx]))
		copy(line, rb.lines[idx])
		result[i] = line
	}

	return result
}

// Last returns the most recent n lines as a single byte slice with newlines.
// Convenient for rendering output.
func (rb *RingBuffer) Last(n int) []byte {
	lines := rb.Lines(n)
	if len(lines) == 0 {
		return nil
	}

	// Calculate total size
	total := 0
	for _, line := range lines {
		total += len(line) + 1 // +1 for newline
	}

	result := make([]byte, 0, total)
	for _, line := range lines {
		result = append(result, line...)
		result = append(result, '\n')
	}

	return result
}

// String returns all buffered content as a string.
func (rb *RingBuffer) String() string {
	return string(rb.Last(rb.Len()))
}

// Len returns the number of lines currently stored.
func (rb *RingBuffer) Len() int {
	rb.mu.RLock()
	defer rb.mu.RUnlock()
	return rb.count
}

// Cap returns the maximum number of lines the buffer can hold.
func (rb *RingBuffer) Cap() int {
	return rb.size
}

// Clear removes all lines from the buffer.
func (rb *RingBuffer) Clear() {
	rb.mu.Lock()
	defer rb.mu.Unlock()

	for i := range rb.lines {
		rb.lines[i] = nil
	}
	rb.head = 0
	rb.count = 0
	rb.partial = rb.partial[:0]
}

// Stats returns buffer statistics.
func (rb *RingBuffer) Stats() BufferStats {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	return BufferStats{
		Lines:      rb.count,
		Capacity:   rb.size,
		BytesIn:    rb.totalIn,
		BytesOut:   rb.totalOut,
		HasPartial: len(rb.partial) > 0,
	}
}

// BufferStats contains ring buffer statistics.
type BufferStats struct {
	Lines      int   // Current number of lines stored
	Capacity   int   // Maximum lines capacity
	BytesIn    int64 // Total bytes written
	BytesOut   int64 // Total bytes read
	HasPartial bool  // Whether there's an incomplete line pending
}

// WriteString writes a string to the buffer.
// Convenience method.
func (rb *RingBuffer) WriteString(s string) (int, error) {
	return rb.Write([]byte(s))
}

// Contains returns true if the buffer contains the given substring.
// Searches all stored lines.
func (rb *RingBuffer) Contains(substr []byte) bool {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	for i := 0; i < rb.count; i++ {
		idx := i
		if rb.count == rb.size {
			idx = (rb.head + i) % rb.size
		}
		if bytes.Contains(rb.lines[idx], substr) {
			return true
		}
	}

	// Also check partial line
	if bytes.Contains(rb.partial, substr) {
		return true
	}

	return false
}

// ContainsString returns true if the buffer contains the given substring.
func (rb *RingBuffer) ContainsString(substr string) bool {
	return rb.Contains([]byte(substr))
}

// Partial returns the current incomplete line being accumulated (if any).
// Returns nil if there's no partial line.
func (rb *RingBuffer) Partial() []byte {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if len(rb.partial) == 0 {
		return nil
	}

	result := make([]byte, len(rb.partial))
	copy(result, rb.partial)
	return result
}
