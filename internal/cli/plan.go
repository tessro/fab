package cli

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/tui"
)

var planProject string

var planCmd = &cobra.Command{
	Use:   "plan [prompt]",
	Short: "Start a planning agent",
	Long: `Start a planning agent in plan mode to create implementation plans.

The planning agent will explore the codebase, design an implementation approach,
and write the plan to .fab/plans/<agent-id>.md when complete.

Planning agents:
- Are visible in the TUI
- Can ask questions via AskUserQuestion
- Run in a worktree if --project is specified
- Are not subject to max-agents limit

Examples:
  fab plan "Add user authentication"
  fab plan --project myapp "Implement dark mode"
`,
	RunE: runPlan,
}

func runPlan(cmd *cobra.Command, args []string) error {
	// Get prompt from args or stdin
	var prompt string
	if len(args) > 0 {
		prompt = strings.Join(args, " ")
	} else {
		return fmt.Errorf("prompt is required: fab plan \"your planning task\"")
	}

	client := MustConnect()

	// Start the planning agent
	resp, err := client.PlanStart(planProject, prompt)
	if err != nil {
		client.Close()
		return fmt.Errorf("start planner: %w", err)
	}

	fmt.Printf("🚌 Planning agent started (ID: %s)\n", resp.ID)
	if resp.Project != "" {
		fmt.Printf("   Project: %s\n", resp.Project)
	}
	fmt.Printf("   Working directory: %s\n", resp.WorkDir)

	// Run TUI in planner mode
	return tui.RunPlannerMode(client, resp.ID)
}

var planListCmd = &cobra.Command{
	Use:   "list",
	Short: "List planning agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		resp, err := client.PlanList(planProject)
		if err != nil {
			return fmt.Errorf("list planners: %w", err)
		}

		if len(resp.Planners) == 0 {
			fmt.Println("No planning agents running")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, "ID\tPROJECT\tSTATE\tAGE\tPLAN FILE")

		for _, p := range resp.Planners {
			startedAt, _ := time.Parse(time.RFC3339, p.StartedAt)
			age := formatDuration(time.Since(startedAt))
			project := p.Project
			if project == "" {
				project = "-"
			}
			planFile := p.PlanFile
			if planFile == "" {
				planFile = "-"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", p.ID, project, p.State, age, planFile)
		}

		_ = w.Flush()
		return nil
	},
}

var planStopCmd = &cobra.Command{
	Use:   "stop <id>",
	Short: "Stop a planning agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]

		client := MustConnect()
		defer client.Close()

		if err := client.PlanStop(id); err != nil {
			return fmt.Errorf("stop planner: %w", err)
		}

		fmt.Printf("🚌 Planning agent %s stopped\n", id)
		return nil
	},
}

var planChatCmd = &cobra.Command{
	Use:   "chat <id>",
	Short: "Open interactive chat with a planning agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]

		client := MustConnect()

		// Run TUI in planner mode
		return tui.RunPlannerMode(client, id)
	},
}

func init() {
	planCmd.Flags().StringVarP(&planProject, "project", "p", "", "Run in project worktree")
	planCmd.AddCommand(planListCmd)
	planCmd.AddCommand(planStopCmd)
	planCmd.AddCommand(planChatCmd)

	planListCmd.Flags().StringVarP(&planProject, "project", "p", "", "Filter by project")

	rootCmd.AddCommand(planCmd)
}
