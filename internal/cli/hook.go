package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/daemon"
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
	Use:   "hook",
	Short: "Handle Claude Code hook callbacks",
	Long: `Handle permission request hooks from Claude Code.

This command is invoked by Claude Code when it needs permission to use a tool.
It reads the hook input from stdin, forwards it to the fab daemon for user
approval via the TUI, and returns the decision to Claude Code.

Example Claude settings.json:
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "fab hook"
          }
        ]
      }
    ]
  }
}`,
	Args:   cobra.NoArgs,
	RunE:   runHook,
	Hidden: true, // Hide from main help since it's for Claude Code integration
}

func runHook(cmd *cobra.Command, args []string) error {
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

	// Only handle PermissionRequest hooks
	if hookInput.HookEventName != "PreToolUse" && hookInput.HookEventName != "PermissionRequest" {
		// Pass through for other hook types
		return outputHookResponse("allow", "", false)
	}

	// Connect to daemon
	client, err := ConnectClient()
	if err != nil {
		// If daemon is not running, deny by default for safety
		return outputHookResponse("deny", "fab daemon is not running", false)
	}
	defer client.Close()

	// Get agent ID from environment
	agentID := os.Getenv("FAB_AGENT_ID")

	// Send permission request to daemon and wait for response
	resp, err := client.RequestPermission(&daemon.PermissionRequestPayload{
		AgentID:   agentID,
		ToolName:  hookInput.ToolName,
		ToolInput: hookInput.ToolInput,
		ToolUseID: hookInput.ToolUseID,
	})
	if err != nil {
		// On error, deny for safety
		return outputHookResponse("deny", fmt.Sprintf("permission request failed: %v", err), false)
	}

	// Output the response
	return outputHookResponse(resp.Behavior, resp.Message, resp.Interrupt)
}

// outputHookResponse writes the hook response to stdout in Claude Code format.
func outputHookResponse(behavior, message string, interrupt bool) error {
	output := HookOutput{
		HookSpecificOutput: HookSpecificOutput{
			HookEventName: "PreToolUse",
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
