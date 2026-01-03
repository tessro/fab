// Package agent provides the Agent type and lifecycle management for Claude Code instances.
package agent

import (
	"regexp"
	"sync"
)

// Detector watches agent output for completion patterns.
// It is thread-safe and can be used concurrently.
type Detector struct {
	patterns []*Pattern
	mu       sync.RWMutex
}

// Pattern represents a completion detection pattern.
type Pattern struct {
	Name        string         // Descriptive name (e.g., "beads_close")
	Description string         // Human-readable description
	Regex       *regexp.Regexp // Compiled regex pattern
}

// Match represents a successful pattern match.
type Match struct {
	Pattern *Pattern // The pattern that matched
	Text    string   // The matched text
}

// DefaultPatterns returns the standard completion patterns for detecting
// when a Claude Code agent has finished its task.
func DefaultPatterns() []*Pattern {
	return []*Pattern{
		{
			Name:        "beads_close",
			Description: "Detects 'bd close' command execution",
			// Matches: bd close, bd close FAB-123, bd close fa-abc
			Regex: regexp.MustCompile(`bd\s+close(?:\s+[\w-]+)?`),
		},
		{
			Name:        "beads_skill_close",
			Description: "Detects /beads:close skill invocation",
			// Matches skill invocation patterns from Claude Code
			Regex: regexp.MustCompile(`/beads:close`),
		},
		{
			Name:        "task_completed",
			Description: "Detects explicit task completion messages",
			// Matches common completion phrases
			Regex: regexp.MustCompile(`(?i)task\s+completed|issue\s+closed|marked\s+as\s+completed`),
		},
	}
}

// NewDetector creates a detector with the given patterns.
// If patterns is nil or empty, DefaultPatterns are used.
func NewDetector(patterns []*Pattern) *Detector {
	if len(patterns) == 0 {
		patterns = DefaultPatterns()
	}
	return &Detector{
		patterns: patterns,
	}
}

// NewDefaultDetector creates a detector with the default completion patterns.
func NewDefaultDetector() *Detector {
	return NewDetector(DefaultPatterns())
}

// AddPattern adds a pattern to the detector.
func (d *Detector) AddPattern(p *Pattern) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.patterns = append(d.patterns, p)
}

// Patterns returns a copy of the current patterns.
func (d *Detector) Patterns() []*Pattern {
	d.mu.RLock()
	defer d.mu.RUnlock()

	result := make([]*Pattern, len(d.patterns))
	copy(result, d.patterns)
	return result
}

// Check scans the given text for any matching patterns.
// Returns the first match found, or nil if no patterns match.
func (d *Detector) Check(text string) *Match {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, p := range d.patterns {
		if match := p.Regex.FindString(text); match != "" {
			return &Match{
				Pattern: p,
				Text:    match,
			}
		}
	}
	return nil
}

// CheckAll scans the given text and returns all matches found.
func (d *Detector) CheckAll(text string) []*Match {
	d.mu.RLock()
	defer d.mu.RUnlock()

	var matches []*Match
	for _, p := range d.patterns {
		if match := p.Regex.FindString(text); match != "" {
			matches = append(matches, &Match{
				Pattern: p,
				Text:    match,
			})
		}
	}
	return matches
}

// CheckBuffer scans a RingBuffer for completion patterns.
// This is the primary method for detecting agent completion.
// Returns the first match found, or nil if no patterns match.
func (d *Detector) CheckBuffer(buf *RingBuffer) *Match {
	if buf == nil {
		return nil
	}

	// Get all buffered content and check it
	content := buf.String()
	return d.Check(content)
}

// CheckLines scans specific lines from a RingBuffer.
// Useful for incremental checking of new output.
func (d *Detector) CheckLines(lines [][]byte) *Match {
	d.mu.RLock()
	defer d.mu.RUnlock()

	for _, line := range lines {
		for _, p := range d.patterns {
			if match := p.Regex.Find(line); match != nil {
				return &Match{
					Pattern: p,
					Text:    string(match),
				}
			}
		}
	}
	return nil
}

// MustCompilePattern creates a Pattern with the given regex, panicking on error.
// Useful for defining patterns at package initialization.
func MustCompilePattern(name, description, pattern string) *Pattern {
	return &Pattern{
		Name:        name,
		Description: description,
		Regex:       regexp.MustCompile(pattern),
	}
}

// CompilePattern creates a Pattern with the given regex.
// Returns an error if the regex is invalid.
func CompilePattern(name, description, pattern string) (*Pattern, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	return &Pattern{
		Name:        name,
		Description: description,
		Regex:       re,
	}, nil
}
