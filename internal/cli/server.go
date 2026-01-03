package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var serverCmd = &cobra.Command{
	Use:   "server",
	Short: "Manage the fab daemon server",
	Long:  "Commands for managing the fab daemon server lifecycle.",
}

var serverStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the fab daemon server",
	Long:  "Stop the running fab daemon server. This will terminate all agents.",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		if err := client.Shutdown(); err != nil {
			return fmt.Errorf("shutdown daemon: %w", err)
		}

		fmt.Println("🚌 fab daemon stopped")
		return nil
	},
}

func init() {
	serverCmd.AddCommand(serverStopCmd)
	rootCmd.AddCommand(serverCmd)
}
