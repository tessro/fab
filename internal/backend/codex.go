// Package backend provides an abstraction layer for different agent CLI implementations.
package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
)

// CodexBackend implements Backend for OpenAI Codex CLI.
type CodexBackend struct{}

// Name returns the backend identifier.
func (b *CodexBackend) Name() string { return "codex" }

// BuildCommand creates the exec.Cmd for the Codex CLI.
func (b *CodexBackend) BuildCommand(cfg CommandConfig) (*exec.Cmd, error) {
	args := []string{"exec", "--json"}

	// Use full-auto mode for automated operation (workspace-write + on-request approval)
	args = append(args, "--full-auto")

	// Add initial prompt if provided
	if cfg.InitialPrompt != "" {
		args = append(args, cfg.InitialPrompt)
	}

	cmd := exec.Command("codex", args...)
	cmd.Dir = cfg.WorkDir
	cmd.Env = append(os.Environ(), "FAB_AGENT_ID="+cfg.AgentID)

	return cmd, nil
}

// ParseStreamMessage parses a JSONL line from Codex CLI's output.
// Codex uses an event-based protocol with type-discriminated messages.
func (b *CodexBackend) ParseStreamMessage(line []byte) (*StreamMessage, error) {
	if len(line) == 0 {
		return nil, nil
	}

	// Parse the Codex event wrapper
	var event codexEvent
	if err := json.Unmarshal(line, &event); err != nil {
		return nil, fmt.Errorf("failed to parse codex event: %w", err)
	}

	// Convert Codex event to canonical StreamMessage
	return b.convertEvent(&event)
}

// FormatInputMessage formats a user message for Codex stdin.
// Codex uses a submission queue protocol with id and op fields.
func (b *CodexBackend) FormatInputMessage(content string, sessionID string) ([]byte, error) {
	submission := codexSubmission{
		ID: sessionID,
		Op: codexOp{
			Type: "user_input",
			Items: []codexInputItem{
				{
					Type: "text",
					Text: content,
				},
			},
		},
	}

	data, err := json.Marshal(submission)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal codex submission: %w", err)
	}

	// Add newline for JSONL format
	return append(data, '\n'), nil
}

// HookSettings returns CLI-specific hook configuration.
// Codex uses built-in approval modes rather than external hooks.
func (b *CodexBackend) HookSettings(fabPath string) map[string]any {
	// Codex doesn't use fab-style hooks; approval is handled via --ask-for-approval flag
	return nil
}

// convertEvent converts a Codex event to a canonical StreamMessage.
func (b *CodexBackend) convertEvent(event *codexEvent) (*StreamMessage, error) {
	switch event.Type {
	case "thread.started":
		// Session initialization - capture thread_id for session management
		return &StreamMessage{
			Type:    "system",
			Subtype: "init",
		}, nil

	case "turn.started":
		// New turn beginning - no action needed
		return nil, nil

	case "turn.completed":
		// Turn complete with usage stats
		if event.Usage != nil {
			return &StreamMessage{
				Type: "assistant",
				Message: &NestedMessage{
					Role: "assistant",
					Usage: &Usage{
						InputTokens:          event.Usage.InputTokens,
						OutputTokens:         event.Usage.OutputTokens,
						CacheReadInputTokens: event.Usage.CachedInputTokens,
					},
				},
			}, nil
		}
		return &StreamMessage{
			Type:   "result",
			Result: "",
		}, nil

	case "item.started":
		// Tool use beginning
		if event.Item != nil && event.Item.Type == "command_execution" {
			cmdInput, _ := json.Marshal(map[string]any{
				"command": event.Item.Command,
			})
			return &StreamMessage{
				Type: "assistant",
				Message: &NestedMessage{
					Role: "assistant",
					Content: []ContentBlock{
						{
							Type:  "tool_use",
							ID:    event.Item.ID,
							Name:  "Bash",
							Input: cmdInput,
						},
					},
				},
			}, nil
		}
		return nil, nil

	case "item.completed":
		if event.Item == nil {
			return nil, nil
		}
		switch event.Item.Type {
		case "reasoning":
			// Agent thinking/reasoning
			return &StreamMessage{
				Type: "assistant",
				Message: &NestedMessage{
					Role: "assistant",
					Content: []ContentBlock{
						{
							Type: "text",
							Text: event.Item.Text,
						},
					},
				},
			}, nil
		case "command_execution":
			// Command completed
			isError := event.Item.ExitCode != nil && *event.Item.ExitCode != 0
			return &StreamMessage{
				Type: "user",
				Message: &NestedMessage{
					Role: "user",
					Content: []ContentBlock{
						{
							Type:      "tool_result",
							ToolUseID: event.Item.ID,
							Content:   FlexContent(event.Item.AggregatedOutput),
							IsError:   isError,
						},
					},
				},
			}, nil
		case "agent_message":
			// Agent text response
			return &StreamMessage{
				Type: "assistant",
				Message: &NestedMessage{
					Role: "assistant",
					Content: []ContentBlock{
						{
							Type: "text",
							Text: event.Item.Text,
						},
					},
				},
			}, nil
		}
		return nil, nil

	case "error":
		return &StreamMessage{
			Type:    "result",
			Result:  event.Message,
			IsError: true,
		}, nil

	case "warning":
		// Emit warnings as system messages
		return &StreamMessage{
			Type:    "system",
			Subtype: "warning",
			Result:  event.Message,
		}, nil

	default:
		// Unknown event type, skip
		return nil, nil
	}
}

// Codex protocol types

// codexEvent represents a Codex event (flat structure with type at top level).
type codexEvent struct {
	Type     string      `json:"type"`      // "thread.started", "item.completed", etc.
	ThreadID string      `json:"thread_id"` // For thread.started
	Item     *codexItem  `json:"item"`      // For item.* events
	Usage    *codexUsage `json:"usage"`     // For turn.completed
	Message  string      `json:"message"`   // For error/warning
}

// codexItem represents an item in item.started/item.completed events.
type codexItem struct {
	ID               string `json:"id"`
	Type             string `json:"type"`             // "reasoning", "command_execution", "agent_message"
	Text             string `json:"text"`             // For reasoning, agent_message
	Command          string `json:"command"`          // For command_execution
	AggregatedOutput string `json:"aggregated_output"`
	ExitCode         *int   `json:"exit_code"`        // Pointer to distinguish 0 from absent
	Status           string `json:"status"`           // "in_progress", "completed", "failed"
}

// codexUsage contains token usage from turn.completed events.
type codexUsage struct {
	InputTokens       int `json:"input_tokens"`
	CachedInputTokens int `json:"cached_input_tokens"`
	OutputTokens      int `json:"output_tokens"`
}

// codexSubmission represents a submission to Codex stdin.
type codexSubmission struct {
	ID string   `json:"id"`
	Op codexOp  `json:"op"`
}

// codexOp represents an operation in a submission.
type codexOp struct {
	Type  string           `json:"type"`
	Items []codexInputItem `json:"items"`
}

// codexInputItem represents an input item in a submission.
type codexInputItem struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Compile-time check that CodexBackend implements Backend.
var _ Backend = (*CodexBackend)(nil)

func init() {
	Register("codex", &CodexBackend{})
}
