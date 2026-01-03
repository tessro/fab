package cli

import (
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "fab",
	Short: "Coding agent supervisor",
	Long:  "fab supervises multiple Claude Code agents across projects with automatic task orchestration.",
}

func Execute() error {
	return rootCmd.Execute()
}
