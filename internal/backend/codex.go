// Package backend provides an abstraction layer for different agent CLI implementations.
package backend

import (
	"encoding/base64"
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
	switch event.Msg.Type {
	case "task_started":
		return &StreamMessage{
			Type:    "system",
			Subtype: "init",
		}, nil

	case "task_complete":
		return &StreamMessage{
			Type:   "result",
			Result: event.Msg.LastAgentMessage,
		}, nil

	case "agent_message":
		return &StreamMessage{
			Type: "assistant",
			Message: &NestedMessage{
				Role: "assistant",
				Content: []ContentBlock{
					{
						Type: "text",
						Text: event.Msg.Message,
					},
				},
			},
		}, nil

	case "agent_message_delta":
		// For streaming deltas, we still emit as assistant message
		return &StreamMessage{
			Type: "assistant",
			Message: &NestedMessage{
				Role: "assistant",
				Content: []ContentBlock{
					{
						Type: "text",
						Text: event.Msg.Delta,
					},
				},
			},
		}, nil

	case "exec_command_begin":
		// Convert to tool_use format
		cmdInput, _ := json.Marshal(map[string]any{
			"command": event.Msg.Command,
			"cwd":     event.Msg.Cwd,
		})
		return &StreamMessage{
			Type: "assistant",
			Message: &NestedMessage{
				Role: "assistant",
				Content: []ContentBlock{
					{
						Type:  "tool_use",
						ID:    event.Msg.CallID,
						Name:  "Bash",
						Input: cmdInput,
					},
				},
			},
		}, nil

	case "exec_command_output_delta":
		// Decode base64 chunk and emit as partial tool result
		output := ""
		if event.Msg.Chunk != "" {
			decoded, err := base64.StdEncoding.DecodeString(event.Msg.Chunk)
			if err == nil {
				output = string(decoded)
			}
		}
		return &StreamMessage{
			Type: "user",
			Message: &NestedMessage{
				Role: "user",
				Content: []ContentBlock{
					{
						Type:      "tool_result",
						ToolUseID: event.Msg.CallID,
						Content:   FlexContent(output),
					},
				},
			},
		}, nil

	case "exec_command_end":
		// Emit complete tool result
		output := event.Msg.AggregatedOutput
		if output == "" {
			output = event.Msg.FormattedOutput
		}
		isError := event.Msg.ExitCode != 0
		return &StreamMessage{
			Type: "user",
			Message: &NestedMessage{
				Role: "user",
				Content: []ContentBlock{
					{
						Type:      "tool_result",
						ToolUseID: event.Msg.CallID,
						Content:   FlexContent(output),
						IsError:   isError,
					},
				},
			},
		}, nil

	case "token_count":
		// Extract token usage if available
		var usage *Usage
		if event.Msg.Info != nil && event.Msg.Info.LastTokenUsage != nil {
			tu := event.Msg.Info.LastTokenUsage
			usage = &Usage{
				InputTokens:         tu.InputTokens,
				OutputTokens:        tu.OutputTokens,
				CacheReadInputTokens: tu.CachedInputTokens,
			}
		}
		if usage != nil {
			return &StreamMessage{
				Type: "assistant",
				Message: &NestedMessage{
					Role:  "assistant",
					Usage: usage,
				},
			}, nil
		}
		return nil, nil

	case "error":
		return &StreamMessage{
			Type:    "result",
			Result:  event.Msg.Message,
			IsError: true,
		}, nil

	case "warning":
		// Emit warnings as system messages
		return &StreamMessage{
			Type:    "system",
			Subtype: "warning",
			Result:  event.Msg.Message,
		}, nil

	default:
		// Unknown event type, skip
		return nil, nil
	}
}

// Codex protocol types

// codexEvent represents a Codex event wrapper.
type codexEvent struct {
	ID  string       `json:"id"`
	Msg codexEventMsg `json:"msg"`
}

// codexEventMsg represents the inner event message.
type codexEventMsg struct {
	Type string `json:"type"`

	// TurnStarted
	ModelContextWindow int64 `json:"model_context_window,omitempty"`

	// TurnComplete
	LastAgentMessage string `json:"last_agent_message,omitempty"`

	// AgentMessage
	Message string `json:"message,omitempty"`

	// AgentMessageDelta
	Delta string `json:"delta,omitempty"`

	// ExecCommandBegin/End
	CallID    string   `json:"call_id,omitempty"`
	ProcessID string   `json:"process_id,omitempty"`
	TurnID    string   `json:"turn_id,omitempty"`
	Command   []string `json:"command,omitempty"`
	Cwd       string   `json:"cwd,omitempty"`

	// ExecCommandOutputDelta
	Stream string `json:"stream,omitempty"` // "stdout" or "stderr"
	Chunk  string `json:"chunk,omitempty"`  // base64-encoded

	// ExecCommandEnd
	Stdout           string `json:"stdout,omitempty"`
	Stderr           string `json:"stderr,omitempty"`
	AggregatedOutput string `json:"aggregated_output,omitempty"`
	ExitCode         int    `json:"exit_code,omitempty"`
	FormattedOutput  string `json:"formatted_output,omitempty"`

	// TokenCount
	Info *codexTokenInfo `json:"info,omitempty"`

	// Note: Error and Warning events use "message" field which is already
	// captured by the Message field above (AgentMessage events use the same key).
}

// codexTokenInfo contains token usage information.
type codexTokenInfo struct {
	TotalTokenUsage    *codexTokenUsage `json:"total_token_usage,omitempty"`
	LastTokenUsage     *codexTokenUsage `json:"last_token_usage,omitempty"`
	ModelContextWindow int64            `json:"model_context_window,omitempty"`
}

// codexTokenUsage contains token counts.
type codexTokenUsage struct {
	InputTokens          int `json:"input_tokens"`
	CachedInputTokens    int `json:"cached_input_tokens"`
	OutputTokens         int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens          int `json:"total_tokens"`
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
