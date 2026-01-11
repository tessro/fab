// Package backend provides an abstraction layer for different agent CLI implementations.
// This allows fab to support multiple agent backends such as Claude Code, OpenAI Codex CLI, etc.
package backend

import "os/exec"

// Backend defines the interface for agent CLI implementations.
// Each backend handles the specific details of building commands, parsing streams,
// and configuring hooks for its respective CLI tool.
type Backend interface {
	// Name returns the backend identifier (e.g., "claude", "codex").
	Name() string

	// BuildCommand creates the exec.Cmd for this backend.
	// The command is configured with the appropriate flags and settings
	// for the backend's CLI tool.
	BuildCommand(cfg CommandConfig) (*exec.Cmd, error)

	// ParseStreamMessage parses a JSONL line from the CLI's output.
	// Returns the parsed message and any parsing error.
	// Returns nil, nil for empty lines.
	ParseStreamMessage(line []byte) (*StreamMessage, error)

	// FormatInputMessage formats a user message for stdin.
	// Returns the formatted message as bytes ready to be written to the CLI's stdin.
	FormatInputMessage(content string, sessionID string) ([]byte, error)

	// HookSettings returns CLI-specific hook configuration.
	// The returned map is merged into the CLI's settings.
	HookSettings(fabPath string) map[string]any
}

// CommandConfig contains parameters for building the CLI command.
type CommandConfig struct {
	// WorkDir is the working directory for the CLI process.
	WorkDir string

	// AgentID is the unique identifier for this agent instance.
	// This is typically set as an environment variable for hooks.
	AgentID string

	// InitialPrompt is the optional initial prompt to send to the agent.
	// If empty, no initial message is sent.
	InitialPrompt string

	// PluginDir is the directory containing CLI plugins.
	PluginDir string

	// HookSettings contains CLI-specific hook configuration.
	// This is typically the result of calling Backend.HookSettings().
	HookSettings map[string]any
}
