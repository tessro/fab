package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage fab agents",
	Long:  "Commands for managing Claude Code agents.",
}

var doneReason string

var agentDoneCmd = &cobra.Command{
	Use:   "done",
	Short: "Signal task completion to the orchestrator",
	Long: `Signal to the orchestrator that this agent has completed its task.

This command is typically called by agents after finishing their work:
  1. Run quality gates (tests, linting, etc.)
  2. Push changes
  3. Close the task with 'bd close <id>'
  4. Run 'fab agent done'

The orchestrator will clean up the agent and spawn a new one if capacity is available.`,
	RunE: runAgentDone,
}

func runAgentDone(cmd *cobra.Command, args []string) error {
	client := MustConnect()
	defer client.Close()

	if err := client.AgentDone(doneReason); err != nil {
		return fmt.Errorf("agent done: %w", err)
	}

	fmt.Println("🚌 Agent done signaled")
	return nil
}

func init() {
	agentDoneCmd.Flags().StringVar(&doneReason, "reason", "", "Optional completion reason")
	agentCmd.AddCommand(agentDoneCmd)
	rootCmd.AddCommand(agentCmd)
}
