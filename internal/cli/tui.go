package cli

import (
	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the terminal user interface",
	Long:  "Launch the interactive TUI for monitoring and managing fab agents.",
	RunE: func(cmd *cobra.Command, args []string) error {
		client, err := ConnectClient()
		if err != nil {
			return err
		}
		defer client.Close()
		return tui.RunWithClient(client)
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
