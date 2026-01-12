package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var managerCmd = &cobra.Command{
	Use:   "manager",
	Short: "Manage the project manager agent",
	Long:  "The manager agent is a dedicated Claude Code instance for interactive conversation about a specific project. It can answer questions about the codebase, help with issue management, and coordinate agent work.",
}

var managerStartCmd = &cobra.Command{
	Use:   "start <project>",
	Short: "Start the manager agent for a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project := args[0]

		client := MustConnect()
		defer client.Close()

		if err := client.ManagerStart(project); err != nil {
			return fmt.Errorf("start manager: %w", err)
		}

		fmt.Printf("🚌 Manager agent started for project %s\n", project)
		return nil
	},
}

var managerStopCmd = &cobra.Command{
	Use:   "stop <project>",
	Short: "Stop the manager agent for a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project := args[0]

		client := MustConnect()
		defer client.Close()

		if err := client.ManagerStop(project); err != nil {
			return fmt.Errorf("stop manager: %w", err)
		}

		fmt.Printf("🚌 Manager agent stopped for project %s\n", project)
		return nil
	},
}

var managerStatusCmd = &cobra.Command{
	Use:   "status <project>",
	Short: "Show manager agent status for a project",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project := args[0]

		client := MustConnect()
		defer client.Close()

		status, err := client.ManagerStatus(project)
		if err != nil {
			return fmt.Errorf("get status: %w", err)
		}

		if status.Running {
			fmt.Printf("🚌 Manager agent for %s is %s (started at %s)\n", project, status.State, status.StartedAt)
			fmt.Printf("   Working directory: %s\n", status.WorkDir)
		} else {
			fmt.Printf("🚌 Manager agent for %s is stopped\n", project)
		}
		return nil
	},
}

var managerClearCmd = &cobra.Command{
	Use:   "clear <project>",
	Short: "Clear the manager agent's context window for a project",
	Long:  "Clears the manager agent's chat history. The manager will lose all conversation context but remain running.",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		project := args[0]

		client := MustConnect()
		defer client.Close()

		if err := client.ManagerClearHistory(project); err != nil {
			return fmt.Errorf("clear history: %w", err)
		}

		fmt.Printf("🚌 Manager context cleared for project %s\n", project)
		return nil
	},
}

func init() {
	rootCmd.AddCommand(managerCmd)
	managerCmd.AddCommand(managerStartCmd)
	managerCmd.AddCommand(managerStopCmd)
	managerCmd.AddCommand(managerStatusCmd)
	managerCmd.AddCommand(managerClearCmd)
}
