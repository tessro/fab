// Package agent provides the Agent type and lifecycle management for Claude Code instances.
package agent

import (
	"encoding/json"

	"github.com/tessro/fab/internal/backend"
)

// Re-export types from backend package for backward compatibility.
// This allows existing code to continue using agent.StreamMessage, agent.ChatEntry, etc.
type (
	StreamMessage = backend.StreamMessage
	NestedMessage = backend.NestedMessage
	ContentBlock  = backend.ContentBlock
	FlexContent   = backend.FlexContent
	Usage         = backend.Usage
	ChatEntry     = backend.ChatEntry
	InputMessage  = backend.InputMessage
	MessageBody   = backend.MessageBody
)

// FormatToolInput formats tool input JSON for display.
// Delegates to the backend package.
func FormatToolInput(name string, input json.RawMessage) string {
	return backend.FormatToolInput(name, input)
}

// ParseStreamMessage parses a single JSONL line from Claude Code's stream-json output.
// Delegates to the ClaudeBackend for parsing.
func ParseStreamMessage(line []byte) (*StreamMessage, error) {
	b := &backend.ClaudeBackend{}
	return b.ParseStreamMessage(line)
}
