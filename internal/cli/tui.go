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
		return tui.Run()
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
