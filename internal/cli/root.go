package cli

import (
	"os"

	"github.com/spf13/cobra"
)

// fabDir is the global --fab-dir flag value.
var fabDir string

var rootCmd = &cobra.Command{
	Use:   "fab",
	Short: "Coding agent supervisor",
	Long:  "fab supervises multiple Claude Code agents across projects with automatic task orchestration.",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Set FAB_DIR environment variable if --fab-dir is provided.
		// This allows all path helpers to use the override.
		if fabDir != "" {
			if err := os.Setenv("FAB_DIR", fabDir); err != nil {
				return err
			}
		}
		return nil
	},
}

// FabDir returns the value of the --fab-dir flag.
func FabDir() string {
	return fabDir
}

func init() {
	rootCmd.PersistentFlags().StringVar(&fabDir, "fab-dir", "", "base directory for fab data (overrides ~/.fab)")
}

func Execute() error {
	return rootCmd.Execute()
}
