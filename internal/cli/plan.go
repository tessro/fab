package cli

import (
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Manage implementation plans",
	Long: `Commands for managing implementation plans.

Plan storage commands allow you to write, read, and list implementation plans
created by planning agents.

To start a planning agent, use: fab agent plan start <prompt>
`,
}

func init() {
	rootCmd.AddCommand(planCmd)
}
