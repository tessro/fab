package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var stopAll bool

var stopCmd = &cobra.Command{
	Use:   "stop [project]",
	Short: "Stop orchestration for a project",
	Long:  "Stop agent orchestration for a project. Running agents will be gracefully stopped.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStop,
}

func runStop(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && !stopAll {
		return fmt.Errorf("specify a project name or use --all")
	}

	client := MustConnect()
	defer client.Close()

	var project string
	if len(args) > 0 {
		project = args[0]
	}

	if err := client.Stop(project, stopAll); err != nil {
		return fmt.Errorf("stop: %w", err)
	}

	if stopAll {
		fmt.Println("🚌 Stopped orchestration for all projects")
	} else {
		fmt.Printf("🚌 Stopped orchestration for project: %s\n", project)
	}
	return nil
}

func init() {
	stopCmd.Flags().BoolVarP(&stopAll, "all", "a", false, "Stop all projects")
	rootCmd.AddCommand(stopCmd)
}
