package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var directorCmd = &cobra.Command{
	Use:   "director",
	Short: "Manage the global director agent",
	Long:  "The director agent is a dedicated Claude Code instance for cross-project coordination. It can monitor activity across all projects, help with resource allocation, and facilitate communication between project managers.",
}

var directorStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the director agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		if err := client.DirectorStart(); err != nil {
			return fmt.Errorf("start director: %w", err)
		}

		fmt.Println("🚌 Director agent started")
		return nil
	},
}

var directorStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the director agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		if err := client.DirectorStop(); err != nil {
			return fmt.Errorf("stop director: %w", err)
		}

		fmt.Println("🚌 Director agent stopped")
		return nil
	},
}

var directorStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show director agent status",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		status, err := client.DirectorStatus()
		if err != nil {
			return fmt.Errorf("get status: %w", err)
		}

		if status.Running {
			fmt.Printf("🚌 Director agent is %s (started at %s)\n", status.State, status.StartedAt)
			fmt.Printf("   Working directory: %s\n", status.WorkDir)
		} else {
			fmt.Println("🚌 Director agent is stopped")
		}
		return nil
	},
}

var directorClearCmd = &cobra.Command{
	Use:   "clear",
	Short: "Clear the director's context window",
	Long:  "Clears the director agent's chat history. The director will lose all conversation context but remain running.",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		if err := client.DirectorClearHistory(); err != nil {
			return fmt.Errorf("clear history: %w", err)
		}

		fmt.Println("🚌 Director context cleared")
		return nil
	},
}

func init() {
	rootCmd.AddCommand(directorCmd)
	directorCmd.AddCommand(directorStartCmd)
	directorCmd.AddCommand(directorStopCmd)
	directorCmd.AddCommand(directorStatusCmd)
	directorCmd.AddCommand(directorClearCmd)
}
