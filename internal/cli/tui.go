package cli

import (
	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/config"
	"github.com/tessro/fab/internal/logging"
	"github.com/tessro/fab/internal/tui"
)

var tuiCmd = &cobra.Command{
	Use:   "tui",
	Short: "Launch the terminal user interface",
	Long:  "Launch the interactive TUI for monitoring and managing fab agents.",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Load global config for log level
		cfg, _ := config.LoadGlobalConfig()
		logLevel := logging.ParseLevel(cfg.GetLogLevel())

		// Set up file logging for TUI debugging
		cleanup, err := logging.Setup("", logLevel)
		if err == nil {
			defer cleanup()
		}

		client, err := ConnectClient()
		if err != nil {
			return err
		}
		defer client.Close()
		return tui.RunWithClient(client, nil)
	},
}

func init() {
	rootCmd.AddCommand(tuiCmd)
}
