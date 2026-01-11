package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"

	"github.com/tessro/fab/internal/plugin"
)

// ClaudeBackend implements the Backend interface for Claude Code CLI.
type ClaudeBackend struct{}

// NewClaudeBackend creates a new Claude Code backend.
func NewClaudeBackend() *ClaudeBackend {
	return &ClaudeBackend{}
}

// Verify ClaudeBackend implements Backend interface.
var _ Backend = (*ClaudeBackend)(nil)

// Name returns the backend identifier.
func (b *ClaudeBackend) Name() string {
	return "claude"
}

// BuildCommand creates the exec.Cmd for launching Claude Code.
func (b *ClaudeBackend) BuildCommand(cfg CommandConfig) (*exec.Cmd, error) {
	// Get fab binary path for hook configuration
	fabPath, err := os.Executable()
	if err != nil {
		fabPath = "fab" // Fall back to PATH lookup
	}

	// Build settings with hooks
	settings := b.HookSettings(fabPath)
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal settings: %w", err)
	}

	// Determine plugin directory
	pluginDir := cfg.PluginDir
	if pluginDir == "" {
		pluginDir = plugin.DefaultInstallDir()
	}

	// Build claude command with stream-json mode
	// --verbose is required when using --output-format stream-json
	cmd := exec.Command("claude",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "default",
		"--plugin-dir", pluginDir,
		"--settings", string(settingsJSON))

	if cfg.WorkDir != "" {
		cmd.Dir = cfg.WorkDir
	}

	// Set environment variable for agent identification
	cmd.Env = append(os.Environ(), "FAB_AGENT_ID="+cfg.AgentID)

	return cmd, nil
}

// ParseStreamMessage parses a JSONL line from Claude Code's output.
func (b *ClaudeBackend) ParseStreamMessage(line []byte) (*StreamMessage, error) {
	if len(line) == 0 {
		return nil, nil
	}

	var msg StreamMessage
	if err := json.Unmarshal(line, &msg); err != nil {
		return nil, fmt.Errorf("failed to parse stream message: %w", err)
	}

	return &msg, nil
}

// FormatInputMessage formats a user message for stdin.
func (b *ClaudeBackend) FormatInputMessage(content string, sessionID string) ([]byte, error) {
	if sessionID == "" {
		sessionID = "default"
	}
	msg := InputMessage{
		Type: "user",
		Message: MessageBody{
			Role:    "user",
			Content: content,
		},
		SessionID:       sessionID,
		ParentToolUseID: nil,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, err
	}

	// Append newline for JSONL format
	return append(data, '\n'), nil
}

// HookSettings returns Claude Code-specific hook configuration.
// Hook timeout matches our permission timeout (5 minutes) since hooks may
// block waiting for user input via the permission manager.
func (b *ClaudeBackend) HookSettings(fabPath string) map[string]any {
	hookTimeoutSec := 5 * 60 // 5 minutes in seconds

	return map[string]any{
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": fabPath + " hook PreToolUse",
							"timeout": hookTimeoutSec,
						},
					},
				},
			},
			"PermissionRequest": []any{
				map[string]any{
					"matcher": "*",
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": fabPath + " hook PermissionRequest",
							"timeout": hookTimeoutSec,
						},
					},
				},
			},
			"Stop": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": fabPath + " hook Stop",
							"timeout": 10, // Short timeout for idle notification
						},
					},
				},
			},
		},
	}
}
