package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

var startAll bool

var startCmd = &cobra.Command{
	Use:   "start [project]",
	Short: "Start orchestration for a project",
	Long:  "Start agent orchestration for a registered project. Agents will pick up tasks from beads and work on them.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runStart,
}

func runStart(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && !startAll {
		return fmt.Errorf("specify a project name or use --all")
	}

	client := MustConnect()
	defer client.Close()

	var project string
	if len(args) > 0 {
		project = args[0]
	}

	if err := client.Start(project, startAll); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	if startAll {
		fmt.Println("🚌 Started orchestration for all projects")
	} else {
		fmt.Printf("🚌 Started orchestration for project: %s\n", project)
	}
	return nil
}

func init() {
	startCmd.Flags().BoolVarP(&startAll, "all", "a", false, "Start all projects")
	rootCmd.AddCommand(startCmd)
}
