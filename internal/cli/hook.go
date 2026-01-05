package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/rules"
)

// HookInput is the input structure from Claude Code's PermissionRequest hook.
type HookInput struct {
	SessionID      string          `json:"session_id"`
	TranscriptPath string          `json:"transcript_path"`
	Cwd            string          `json:"cwd"`
	PermissionMode string          `json:"permission_mode"`
	HookEventName  string          `json:"hook_event_name"`
	ToolName       string          `json:"tool_name"`
	ToolInput      json.RawMessage `json:"tool_input"`
	ToolUseID      string          `json:"tool_use_id"`
}

// HookOutput is the output structure expected by Claude Code.
type HookOutput struct {
	HookSpecificOutput HookSpecificOutput `json:"hookSpecificOutput"`
}

// HookSpecificOutput contains the hook-specific response.
type HookSpecificOutput struct {
	HookEventName string       `json:"hookEventName"`
	Decision      HookDecision `json:"decision"`
}

// HookDecision contains the permission decision.
type HookDecision struct {
	Behavior  string `json:"behavior"` // "allow" or "deny"
	Message   string `json:"message,omitempty"`
	Interrupt bool   `json:"interrupt,omitempty"`
}

var hookCmd = &cobra.Command{
	Use:   "hook <hook-name>",
	Short: "Handle Claude Code hook callbacks",
	Long: `Handle permission request hooks from Claude Code.

This command is invoked by Claude Code when it needs permission to use a tool.
It reads the hook input from stdin, forwards it to the fab daemon for user
approval via the TUI, and returns the decision to Claude Code.

Supported hook names:
  - PreToolUse: Called before a tool is used
  - PermissionRequest: Called when permission is requested (legacy)

Example Claude settings.json:
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "fab hook PreToolUse"
          }
        ]
      }
    ],
    "PermissionRequest": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "fab hook PermissionRequest"
          }
        ]
      }
    ]
  }
}`,
	Args:   cobra.ExactArgs(1),
	RunE:   runHook,
	Hidden: true, // Hide from main help since it's for Claude Code integration
}

func runHook(cmd *cobra.Command, args []string) error {
	hookName := args[0]

	slog.Debug("hook invoked", "hook", hookName)

	// Validate hook name
	if hookName != "PreToolUse" && hookName != "PermissionRequest" {
		// Pass through for unsupported hook types
		return outputHookResponse(hookName, "allow", "", false)
	}

	// Read hook input from stdin
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	// Parse the hook input
	var hookInput HookInput
	if err := json.Unmarshal(input, &hookInput); err != nil {
		return fmt.Errorf("parse hook input: %w", err)
	}

	slog.Debug("hook received",
		"hook", hookName,
		"event", hookInput.HookEventName,
		"tool", hookInput.ToolName,
		"input", string(hookInput.ToolInput),
	)

	// Evaluate permission rules before contacting daemon
	evaluator := rules.NewEvaluator()

	// Try to find the project name from the working directory
	projectName, err := rules.FindProjectName(hookInput.Cwd)
	if err != nil {
		slog.Debug("failed to find project name", "cwd", hookInput.Cwd, "error", err)
	}

	ctx := context.Background()
	action, matched, err := evaluator.Evaluate(ctx, projectName, hookInput.ToolName, hookInput.ToolInput, hookInput.Cwd)
	if err != nil {
		slog.Debug("rule evaluation error", "error", err)
	} else if matched {
		switch action {
		case rules.ActionAllow:
			return outputHookResponse(hookName, "allow", "", false)
		case rules.ActionDeny:
			return outputHookResponse(hookName, "deny", "blocked by permission rule", false)
			// ActionPass falls through to daemon
		}
	}

	// No matching rule or pass effect - proceed to daemon for TUI prompt

	// Connect to daemon
	client, err := ConnectClient()
	if err != nil {
		// If daemon is not running, deny by default for safety
		return outputHookResponse(hookName, "deny", "fab daemon is not running", false)
	}
	defer client.Close()

	// Get agent ID from environment
	agentID := os.Getenv("FAB_AGENT_ID")

	slog.Info("permission request sent to daemon",
		"agent", agentID,
		"tool", hookInput.ToolName,
		"input", string(hookInput.ToolInput),
	)

	// Send permission request to daemon and wait for response
	resp, err := client.RequestPermission(&daemon.PermissionRequestPayload{
		AgentID:   agentID,
		ToolName:  hookInput.ToolName,
		ToolInput: hookInput.ToolInput,
		ToolUseID: hookInput.ToolUseID,
	})
	if err != nil {
		slog.Warn("permission request failed",
			"agent", agentID,
			"tool", hookInput.ToolName,
			"error", err,
		)
		// On error, deny for safety
		return outputHookResponse(hookName, "deny", fmt.Sprintf("permission request failed: %v", err), false)
	}

	slog.Info("permission response received",
		"hook", hookName,
		"agent", agentID,
		"tool", hookInput.ToolName,
		"behavior", resp.Behavior,
		"message", resp.Message,
		"interrupt", resp.Interrupt,
	)

	// Output the response
	return outputHookResponse(hookName, resp.Behavior, resp.Message, resp.Interrupt)
}

// outputHookResponse writes the hook response to stdout in Claude Code format.
func outputHookResponse(hookName, behavior, message string, interrupt bool) error {
	output := HookOutput{
		HookSpecificOutput: HookSpecificOutput{
			HookEventName: hookName,
			Decision: HookDecision{
				Behavior:  behavior,
				Message:   message,
				Interrupt: interrupt,
			},
		},
	}

	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	fmt.Println(string(data))
	return nil
}

func init() {
	rootCmd.AddCommand(hookCmd)
}
