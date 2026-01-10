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
	"github.com/tessro/fab/internal/logging"
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

// PreToolUseOutput is the output structure for PreToolUse hooks.
type PreToolUseOutput struct {
	HookSpecificOutput PreToolUseSpecificOutput `json:"hookSpecificOutput"`
}

// PreToolUseSpecificOutput contains the PreToolUse-specific response.
type PreToolUseSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`                 // "allow", "deny", or "ask"
	PermissionDecisionReason string `json:"permissionDecisionReason,omitempty"` // reason for the decision
}

// PermissionRequestOutput is the output structure for PermissionRequest hooks.
type PermissionRequestOutput struct {
	HookSpecificOutput PermissionRequestSpecificOutput `json:"hookSpecificOutput"`
}

// PermissionRequestSpecificOutput contains the PermissionRequest-specific response.
type PermissionRequestSpecificOutput struct {
	HookEventName string                    `json:"hookEventName"`
	Decision      PermissionRequestDecision `json:"decision"`
}

// PermissionRequestDecision contains the permission decision for PermissionRequest hooks.
type PermissionRequestDecision struct {
	Behavior  string `json:"behavior"` // "allow" or "deny"
	Message   string `json:"message,omitempty"`
	Interrupt bool   `json:"interrupt,omitempty"`
}

// AskUserQuestionInput is the tool_input structure for the AskUserQuestion tool.
type AskUserQuestionInput struct {
	Questions []daemon.QuestionItem `json:"questions"`
	Answers   map[string]string     `json:"answers,omitempty"`
}

// PreToolUseOutputWithInput is the output structure for PreToolUse hooks that modify input.
type PreToolUseOutputWithInput struct {
	HookSpecificOutput PreToolUseSpecificOutputWithInput `json:"hookSpecificOutput"`
}

// PreToolUseSpecificOutputWithInput contains PreToolUse output with updated input.
type PreToolUseSpecificOutputWithInput struct {
	HookEventName            string          `json:"hookEventName"`
	PermissionDecision       string          `json:"permissionDecision"`                 // "allow", "deny", or "ask"
	PermissionDecisionReason string          `json:"permissionDecisionReason,omitempty"` // reason for the decision
	UpdatedInput             json.RawMessage `json:"updatedInput,omitempty"`             // modified tool input
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
	// Setup file logging so logs are visible (stdout is for Claude Code JSON response)
	cleanup, err := logging.Setup("")
	if err == nil {
		defer cleanup()
	}

	hookName := args[0]

	// Read stdin first so we can log everything together
	input, err := io.ReadAll(os.Stdin)
	if err != nil {
		return fmt.Errorf("read stdin: %w", err)
	}

	slog.Debug("hook invoked",
		"hook", hookName,
		"stdin", string(input),
	)

	// Validate hook name
	if hookName != "PreToolUse" && hookName != "PermissionRequest" {
		// Pass through for unsupported hook types
		return outputHookResponse(hookName, "allow", "", false)
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

	// Handle AskUserQuestion tool specially - this needs user interaction via TUI
	if hookInput.ToolName == "AskUserQuestion" {
		return handleAskUserQuestion(hookName, hookInput)
	}

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

// handleAskUserQuestion processes the AskUserQuestion tool via the TUI.
// It sends the questions to the daemon for display in the TUI, waits for the user
// to select answers, and returns the answers in the updatedInput field.
func handleAskUserQuestion(hookName string, hookInput HookInput) error {
	// Parse the tool input to get the questions
	var askInput AskUserQuestionInput
	if err := json.Unmarshal(hookInput.ToolInput, &askInput); err != nil {
		slog.Warn("failed to parse AskUserQuestion input", "error", err)
		return outputHookResponse(hookName, "deny", "failed to parse questions", false)
	}

	if len(askInput.Questions) == 0 {
		slog.Warn("AskUserQuestion has no questions")
		return outputHookResponse(hookName, "deny", "no questions provided", false)
	}

	// Connect to daemon
	client, err := ConnectClient()
	if err != nil {
		slog.Warn("daemon not running for AskUserQuestion")
		return outputHookResponse(hookName, "deny", "fab daemon is not running", false)
	}
	defer client.Close()

	// Get agent ID from environment
	agentID := os.Getenv("FAB_AGENT_ID")

	slog.Info("user question request sent to daemon",
		"agent", agentID,
		"question_count", len(askInput.Questions),
	)

	// Send user question request to daemon and wait for response
	resp, err := client.RequestUserQuestion(&daemon.UserQuestionRequestPayload{
		AgentID:   agentID,
		Questions: askInput.Questions,
	})
	if err != nil {
		slog.Warn("user question request failed",
			"agent", agentID,
			"error", err,
		)
		return outputHookResponse(hookName, "deny", fmt.Sprintf("user question failed: %v", err), false)
	}

	slog.Info("user question response received",
		"agent", agentID,
		"answers", resp.Answers,
	)

	// Update the tool input with the answers
	askInput.Answers = resp.Answers

	// Marshal the updated input
	updatedInput, err := json.Marshal(askInput)
	if err != nil {
		slog.Warn("failed to marshal updated input", "error", err)
		return outputHookResponse(hookName, "deny", "failed to marshal answers", false)
	}

	// Return the response with updated input
	return outputAskUserQuestionResponse(hookName, updatedInput)
}

// outputAskUserQuestionResponse writes the hook response with updated input for AskUserQuestion.
func outputAskUserQuestionResponse(hookName string, updatedInput json.RawMessage) error {
	output := PreToolUseOutputWithInput{
		HookSpecificOutput: PreToolUseSpecificOutputWithInput{
			HookEventName:      hookName,
			PermissionDecision: "allow",
			UpdatedInput:       updatedInput,
		},
	}

	data, err := json.Marshal(output)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	slog.Debug("AskUserQuestion response", "stdout", string(data))
	fmt.Println(string(data))
	return nil
}

// outputHookResponse writes the hook response to stdout in Claude Code format.
// Note: PreToolUse and PermissionRequest hooks have different response formats.
func outputHookResponse(hookName, behavior, message string, interrupt bool) error {
	var data []byte
	var err error

	if hookName == "PreToolUse" {
		// PreToolUse uses permissionDecision directly
		output := PreToolUseOutput{
			HookSpecificOutput: PreToolUseSpecificOutput{
				HookEventName:            hookName,
				PermissionDecision:       behavior,
				PermissionDecisionReason: message,
			},
		}
		data, err = json.Marshal(output)
	} else {
		// PermissionRequest uses decision.behavior
		output := PermissionRequestOutput{
			HookSpecificOutput: PermissionRequestSpecificOutput{
				HookEventName: hookName,
				Decision: PermissionRequestDecision{
					Behavior:  behavior,
					Message:   message,
					Interrupt: interrupt,
				},
			},
		}
		data, err = json.Marshal(output)
	}

	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}

	slog.Debug("hook response", "stdout", string(data))
	fmt.Println(string(data))
	return nil
}

func init() {
	rootCmd.AddCommand(hookCmd)
}
