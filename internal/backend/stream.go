package backend

import (
	"encoding/json"
	"strings"
)

// StreamMessage represents a parsed message from an agent CLI's streaming output.
// This is a canonical representation that backends translate their CLI-specific
// output into.
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
