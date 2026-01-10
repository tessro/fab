// Package agent provides the Agent type and lifecycle management for Claude Code instances.
package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// StreamMessage represents a parsed message from Claude Code's stream-json output.
// The format has type at the top level ("system", "assistant", "user", "result")
// with message content nested in a "message" field for assistant/user types.
type StreamMessage struct {
	Type    string         `json:"type"`              // "system", "assistant", "user", "result"
	Subtype string         `json:"subtype,omitempty"` // For system messages: "init", "hook_response"
	Message *NestedMessage `json:"message,omitempty"` // For assistant/user types
	Result  string         `json:"result,omitempty"`  // For result type
	IsError bool           `json:"is_error,omitempty"`
}

// NestedMessage contains the actual API message content.
type NestedMessage struct {
	Role       string         `json:"role"`                  // "assistant", "user"
	Content    []ContentBlock `json:"content"`               // Message content blocks
	Model      string         `json:"model,omitempty"`       // Model name
	StopReason string         `json:"stop_reason,omitempty"` // Reason for stopping
	Usage      *Usage         `json:"usage,omitempty"`       // Token usage info
}

// ContentBlock represents a single content item in a message.
type ContentBlock struct {
	Type      string          `json:"type"`                  // "text", "tool_use", "tool_result"
	Text      string          `json:"text,omitempty"`        // For text blocks
	ID        string          `json:"id,omitempty"`          // tool_use ID
	Name      string          `json:"name,omitempty"`        // Tool name (Bash, Read, etc.)
	Input     json.RawMessage `json:"input,omitempty"`       // Tool input as raw JSON
	Content   FlexContent     `json:"content,omitempty"`     // tool_result content (string or array)
	ToolUseID string          `json:"tool_use_id,omitempty"` // Links result to tool_use
	IsError   bool            `json:"is_error,omitempty"`    // tool_result error flag
}

// FlexContent handles the "content" field which can be either a string
// or an array of content parts (e.g., [{"type":"text","text":"..."}]).
type FlexContent string

// UnmarshalJSON implements custom unmarshaling for FlexContent.
func (f *FlexContent) UnmarshalJSON(data []byte) error {
	// Try string first (most common case)
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		*f = FlexContent(s)
		return nil
	}

	// Try array of content parts
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(data, &parts); err == nil {
		var texts []string
		for _, p := range parts {
			if p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		*f = FlexContent(strings.Join(texts, "\n"))
		return nil
	}

	// If neither works, just store the raw JSON as string for debugging
	*f = FlexContent(string(data))
	return nil
}

// Usage contains token usage information.
type Usage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// ChatEntry represents a displayable chat message for the TUI.
type ChatEntry struct {
	Role       string    // "assistant", "user", "tool"
	Content    string    // Rendered text for display
	ToolName   string    // For tool entries (e.g., "Bash")
	ToolInput  string    // Tool input summary
	ToolResult string    // Tool output
	Timestamp  time.Time // When the entry was created
}

// InputMessage is sent to Claude Code via stdin in stream-json mode.
type InputMessage struct {
	Type            string      `json:"type"`               // "user"
	Message         MessageBody `json:"message"`            // Message content
	SessionID       string      `json:"session_id"`         // Session identifier
	ParentToolUseID *string     `json:"parent_tool_use_id"` // null for regular messages
}

// MessageBody contains the actual message content.
type MessageBody struct {
	Role    string `json:"role"`    // "user"
	Content string `json:"content"` // Message text
}

// ParseStreamMessage parses a single JSONL line from Claude Code's stream-json output.
// Returns nil and an error if the line cannot be parsed.
func ParseStreamMessage(line []byte) (*StreamMessage, error) {
	if len(line) == 0 {
		return nil, nil
	}

	var msg StreamMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse stream message: %w", err)
	}

	return &msg, nil
}

// ToChatEntries converts a StreamMessage to displayable ChatEntry items.
// A single message may produce multiple entries (e.g., text + tool calls).
func (m *StreamMessage) ToChatEntries() []ChatEntry {
	if m == nil {
		return nil
	}

	// Skip system/result messages - only process assistant/user messages
	if m.Message == nil {
		return nil
	}

	now := time.Now()
	var entries []ChatEntry
	msg := m.Message

	for _, block := range msg.Content {
		switch block.Type {
		case "text":
			if block.Text != "" {
				entries = append(entries, ChatEntry{
					Role:      msg.Role,
					Content:   block.Text,
					Timestamp: now,
				})
			}

		case "tool_use":
			entries = append(entries, ChatEntry{
				Role:      "tool",
				ToolName:  block.Name,
				ToolInput: FormatToolInput(block.Name, block.Input),
				Timestamp: now,
			})

		case "tool_result":
			entries = append(entries, ChatEntry{
				Role:       "tool",
				ToolResult: string(block.Content),
				Timestamp:  now,
			})
		}
	}

	return entries
}

// FormatToolInput formats tool input JSON for display.
// Returns a human-readable summary of the tool invocation.
func FormatToolInput(name string, input json.RawMessage) string {
	if len(input) == 0 {
		return ""
	}

	var data map[string]any
	if err := json.Unmarshal(input, &data); err != nil {
		return string(input)
	}

	switch name {
	case "Bash":
		return formatBashInput(data)
	case "Read":
		return formatReadInput(data)
	case "Write":
		return formatWriteInput(data)
	case "Edit":
		return formatEditInput(data)
	case "Glob":
		return formatGlobInput(data)
	case "Grep":
		return formatGrepInput(data)
	default:
		return formatGenericInput(data)
	}
}

// formatBashInput formats Bash tool input.
func formatBashInput(data map[string]any) string {
	if cmd, ok := data["command"].(string); ok {
		// Truncate long commands
		if len(cmd) > 100 {
			return cmd[:97] + "..."
		}
		return cmd
	}
	return formatGenericInput(data)
}

// formatReadInput formats Read tool input.
func formatReadInput(data map[string]any) string {
	if path, ok := data["file_path"].(string); ok {
		return path
	}
	return formatGenericInput(data)
}

// formatWriteInput formats Write tool input.
func formatWriteInput(data map[string]any) string {
	if path, ok := data["file_path"].(string); ok {
		return path
	}
	return formatGenericInput(data)
}

// formatEditInput formats Edit tool input.
func formatEditInput(data map[string]any) string {
	path, _ := data["file_path"].(string)
	if path != "" {
		return path
	}
	return formatGenericInput(data)
}

// formatGlobInput formats Glob tool input.
func formatGlobInput(data map[string]any) string {
	pattern, _ := data["pattern"].(string)
	path, _ := data["path"].(string)
	if pattern != "" {
		if path != "" {
			return fmt.Sprintf("%s in %s", pattern, path)
		}
		return pattern
	}
	return formatGenericInput(data)
}

// formatGrepInput formats Grep tool input.
func formatGrepInput(data map[string]any) string {
	pattern, _ := data["pattern"].(string)
	path, _ := data["path"].(string)
	if pattern != "" {
		if path != "" {
			return fmt.Sprintf("%q in %s", pattern, path)
		}
		return fmt.Sprintf("%q", pattern)
	}
	return formatGenericInput(data)
}

// formatGenericInput formats any tool input as a compact JSON summary.
func formatGenericInput(data map[string]any) string {
	// Build a simplified summary
	var parts []string
	for k, v := range data {
		switch val := v.(type) {
		case string:
			if len(val) > 50 {
				val = val[:47] + "..."
			}
			parts = append(parts, fmt.Sprintf("%s=%q", k, val))
		case float64:
			parts = append(parts, fmt.Sprintf("%s=%v", k, val))
		case bool:
			parts = append(parts, fmt.Sprintf("%s=%v", k, val))
		default:
			parts = append(parts, fmt.Sprintf("%s=...", k))
		}
	}
	return strings.Join(parts, ", ")
}

// IsToolUse returns true if the message contains any tool_use blocks.
func (m *StreamMessage) IsToolUse() bool {
	if m.Message == nil {
		return false
	}
	for _, block := range m.Message.Content {
		if block.Type == "tool_use" {
			return true
		}
	}
	return false
}

// IsToolResult returns true if the message contains any tool_result blocks.
func (m *StreamMessage) IsToolResult() bool {
	if m.Message == nil {
		return false
	}
	for _, block := range m.Message.Content {
		if block.Type == "tool_result" {
			return true
		}
	}
	return false
}

// GetText returns all text content from the message, concatenated.
func (m *StreamMessage) GetText() string {
	if m.Message == nil {
		return ""
	}
	var texts []string
	for _, block := range m.Message.Content {
		if block.Type == "text" && block.Text != "" {
			texts = append(texts, block.Text)
		}
	}
	return strings.Join(texts, "\n")
}

// GetToolUses returns all tool_use blocks from the message.
func (m *StreamMessage) GetToolUses() []ContentBlock {
	if m.Message == nil {
		return nil
	}
	var tools []ContentBlock
	for _, block := range m.Message.Content {
		if block.Type == "tool_use" {
			tools = append(tools, block)
		}
	}
	return tools
}

// GetToolResults returns all tool_result blocks from the message.
func (m *StreamMessage) GetToolResults() []ContentBlock {
	if m.Message == nil {
		return nil
	}
	var results []ContentBlock
	for _, block := range m.Message.Content {
		if block.Type == "tool_result" {
			results = append(results, block)
		}
	}
	return results
}
