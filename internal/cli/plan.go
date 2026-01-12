package cli

import (
	"fmt"
	"log/slog"
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

Use 'fab tui' or 'fab plan chat <id>' to interact with the planning agent.

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

	slog.Debug("plan: connecting to daemon")
	client := MustConnect()
	slog.Debug("plan: connected to daemon")

	// Start the planning agent
	slog.Debug("plan: sending PlanStart request", "project", planProject, "prompt_len", len(prompt))
	resp, err := client.PlanStart(planProject, prompt)
	if err != nil {
		slog.Error("plan: PlanStart failed", "error", err)
		client.Close()
		return fmt.Errorf("start planner: %w", err)
	}
	slog.Debug("plan: PlanStart succeeded", "id", resp.ID, "project", resp.Project, "workdir", resp.WorkDir)

	fmt.Printf("🚌 Planning agent started (ID: %s)\n", resp.ID)
	if resp.Project != "" {
		fmt.Printf("   Project: %s\n", resp.Project)
	}
	fmt.Printf("   Working directory: %s\n", resp.WorkDir)
	fmt.Println()
	fmt.Printf("Use 'fab tui' or 'fab plan chat %s' to interact with the agent.\n", resp.ID)

	client.Close()
	return nil
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
		_, _ = fmt.Fprintln(w, "ID\tPROJECT\tSTATE\tBACKEND\tDESCRIPTION\tAGE\tPLAN FILE")

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
			desc := p.Description
			if desc == "" {
				desc = "-"
			}
			// Truncate long descriptions for display
			if len(desc) > 40 {
				desc = desc[:37] + "..."
			}
			backend := p.Backend
			if backend == "" {
				backend = "-"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n", p.ID, project, p.State, backend, desc, age, planFile)
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

		// Launch main TUI with the planner selected
		return tui.RunWithClient(client, &tui.TUIOptions{
			InitialAgentID: tui.PlannerAgentIDPrefix + id,
		})
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
