package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/version"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  "Print the version, commit, and build date of fab.",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("🚌 fab %s (commit: %s, built: %s)\n",
			version.Version, version.Commit, version.Date)
	},
}

func init() {
	rootCmd.AddCommand(versionCmd)
}
