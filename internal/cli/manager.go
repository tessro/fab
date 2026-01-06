package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/tui"
)

var managerCmd = &cobra.Command{
	Use:   "manager",
	Short: "Manage the manager agent",
	Long:  "The manager agent is a dedicated Claude Code instance for interactive conversation that knows about all projects and can coordinate work across the fleet.",
}

var managerStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the manager agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		if err := client.ManagerStart(); err != nil {
			return fmt.Errorf("start manager: %w", err)
		}

		fmt.Println("🚌 Manager agent started")
		return nil
	},
}

var managerStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the manager agent",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		if err := client.ManagerStop(); err != nil {
			return fmt.Errorf("stop manager: %w", err)
		}

		fmt.Println("🚌 Manager agent stopped")
		return nil
	},
}

var managerStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show manager agent status",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		status, err := client.ManagerStatus()
		if err != nil {
			return fmt.Errorf("get status: %w", err)
		}

		if status.Running {
			fmt.Printf("🚌 Manager agent is %s (started at %s)\n", status.State, status.StartedAt)
		} else {
			fmt.Println("🚌 Manager agent is stopped")
		}
		return nil
	},
}

var managerChatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Open interactive chat with the manager agent",
	Long:  "Opens the TUI in manager mode for interactive conversation with the manager agent.",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()

		// Check if manager is running, start if not
		status, err := client.ManagerStatus()
		if err != nil {
			client.Close()
			return fmt.Errorf("get status: %w", err)
		}

		if !status.Running {
			if err := client.ManagerStart(); err != nil {
				client.Close()
				return fmt.Errorf("start manager: %w", err)
			}
			fmt.Println("🚌 Manager agent started")
		}

		// Run TUI in manager mode
		return tui.RunManagerMode(client)
	},
}

func init() {
	rootCmd.AddCommand(managerCmd)
	managerCmd.AddCommand(managerStartCmd)
	managerCmd.AddCommand(managerStopCmd)
	managerCmd.AddCommand(managerStatusCmd)
	managerCmd.AddCommand(managerChatCmd)
}
