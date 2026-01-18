package cli

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/tui"
)

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "Manage fab agents",
	Long:  "Commands for managing Claude Code agents.",
}

var agentListProject string

var agentListCmd = &cobra.Command{
	Use:   "list",
	Short: "List running agents",
	Long:  "List all running agents, optionally filtered by project.",
	RunE:  runAgentList,
}

func runAgentList(cmd *cobra.Command, args []string) error {
	client := MustConnect()
	defer client.Close()

	resp, err := client.AgentList(agentListProject)
	if err != nil {
		return fmt.Errorf("list agents: %w", err)
	}

	if len(resp.Agents) == 0 {
		if agentListProject != "" {
			fmt.Printf("No agents for project %q\n", agentListProject)
		} else {
			fmt.Println("No agents running")
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, " \tID\tPROJECT\tBACKEND\tDESCRIPTION\tAGE")

	for _, a := range resp.Agents {
		age := formatDuration(time.Since(a.StartedAt))
		desc := a.Description
		if desc == "" {
			desc = "-"
		}
		// Truncate long descriptions for display
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
		backend := a.Backend
		if backend == "" {
			backend = "-"
		}
		icon := stateIcon(a.State)
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", icon, a.ID, a.Project, backend, desc, age)
	}

	_ = w.Flush()
	return nil
}

// stateIcon returns an icon for the agent state.
func stateIcon(state string) string {
	switch state {
	case "starting":
		return "○"
	case "running":
		return "●"
	case "idle":
		return "○"
	case "done":
		return "✓"
	case "error":
		return "✗"
	case "stopped":
		return "○"
	case "stopping":
		return "○"
	default:
		return "?"
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

var (
	doneErrorMsg   string
	doneTaskID     string
	abortForce     bool
	abortNoConfirm bool
)

var agentAbortCmd = &cobra.Command{
	Use:   "abort <agent-id>",
	Short: "Abort a running agent",
	Long:  "Abort a running agent. By default sends /quit for graceful shutdown. Use --force to kill immediately.",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentAbort,
}

func runAgentAbort(cmd *cobra.Command, args []string) error {
	agentID := args[0]

	// Check if this is a planner agent
	isPlanner := strings.HasPrefix(agentID, tui.PlannerAgentIDPrefix)

	// Confirm unless --yes is specified
	if !abortNoConfirm {
		action := "gracefully abort"
		if abortForce && !isPlanner {
			action = "force kill"
		} else if isPlanner {
			action = "stop"
		}
		fmt.Printf("Are you sure you want to %s agent %s? [y/N] ", action, agentID)
		var confirm string
		_, _ = fmt.Scanln(&confirm)
		if confirm != "y" && confirm != "Y" {
			return fmt.Errorf("aborted")
		}
	}

	client := MustConnect()
	defer client.Close()

	if isPlanner {
		// Planners use PlanStop (graceful only, force is ignored)
		plannerID := strings.TrimPrefix(agentID, tui.PlannerAgentIDPrefix)
		if err := client.PlanStop(plannerID); err != nil {
			return fmt.Errorf("stop planner: %w", err)
		}
		fmt.Printf("🚌 Stopped planner %s\n", agentID)
	} else {
		if err := client.AgentAbort(agentID, abortForce); err != nil {
			return fmt.Errorf("abort agent: %w", err)
		}
		if abortForce {
			fmt.Printf("🚌 Force killed agent %s\n", agentID)
		} else {
			fmt.Printf("🚌 Sent quit to agent %s\n", agentID)
		}
	}
	return nil
}

var agentClaimCmd = &cobra.Command{
	Use:   "claim <ticket-id>",
	Short: "Claim a ticket for this agent",
	Long:  "Claim a ticket to prevent other agents from working on it. Uses FAB_AGENT_ID env var.",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentClaim,
}

func runAgentClaim(cmd *cobra.Command, args []string) error {
	agentID := os.Getenv("FAB_AGENT_ID")
	if agentID == "" {
		return fmt.Errorf("FAB_AGENT_ID environment variable not set")
	}

	ticketID := args[0]

	client := MustConnect()
	defer client.Close()

	if err := client.AgentClaim(agentID, ticketID); err != nil {
		return fmt.Errorf("claim failed: %w", err)
	}

	fmt.Printf("🚌 Claimed %s\n", ticketID)
	return nil
}

var agentDoneCmd = &cobra.Command{
	Use:   "done",
	Short: "Signal that the agent has completed its task",
	Long:  "Called by Claude Code to signal task completion. Uses FAB_AGENT_ID env var.",
	RunE:  runAgentDone,
}

var agentDescribeCmd = &cobra.Command{
	Use:   "describe <description>",
	Short: "Set a description for this agent",
	Long:  "Set a human-readable description of what the agent is currently doing. Uses FAB_AGENT_ID env var.",
	Args:  cobra.ExactArgs(1),
	RunE:  runAgentDescribe,
}

func runAgentDescribe(cmd *cobra.Command, args []string) error {
	agentID := os.Getenv("FAB_AGENT_ID")
	if agentID == "" {
		return fmt.Errorf("FAB_AGENT_ID environment variable not set")
	}

	description := args[0]

	client := MustConnect()
	defer client.Close()

	if err := client.AgentDescribe(agentID, description); err != nil {
		return fmt.Errorf("describe failed: %w", err)
	}

	fmt.Printf("🚌 Description set: %s\n", description)
	return nil
}

func runAgentDone(cmd *cobra.Command, args []string) error {
	agentID := os.Getenv("FAB_AGENT_ID")
	if agentID == "" {
		return fmt.Errorf("FAB_AGENT_ID environment variable not set")
	}

	// Check if this is a planner agent (worktrees should NOT be merged)
	isPlanner := strings.HasPrefix(agentID, tui.PlannerAgentIDPrefix)

	if !isPlanner {
		// Pre-rebase: fetch and rebase onto origin/main to catch conflicts early
		// Agent runs in worktree, so use current directory
		fmt.Println("🚌 Rebasing onto origin/main...")

		fetchCmd := exec.Command("git", "fetch", "origin")
		if output, err := fetchCmd.CombinedOutput(); err != nil {
			return fmt.Errorf("fetch origin: %w\n%s", err, output)
		}

		rebaseCmd := exec.Command("git", "rebase", "origin/main")
		if output, err := rebaseCmd.CombinedOutput(); err != nil {
			// Rebase failed - abort and return error
			abortCmd := exec.Command("git", "rebase", "--abort")
			_ = abortCmd.Run()
			return fmt.Errorf("rebase conflict - please resolve conflicts and try again:\n%s", output)
		}

		fmt.Println("🚌 Rebase successful, completing...")
	}

	client := MustConnect()
	defer client.Close()

	resp, err := client.AgentDoneWithResponse(agentID, doneTaskID, doneErrorMsg)
	if err != nil {
		return fmt.Errorf("agent done: %w", err)
	}

	if doneErrorMsg != "" {
		fmt.Printf("🚌 Agent %s signaled error: %s\n", agentID, doneErrorMsg)
	} else if isPlanner {
		fmt.Printf("🚌 Plan agent %s completed\n", agentID)
	} else if resp.PRCreated {
		fmt.Printf("🚌 Agent %s completed and created PR: %s\n", agentID, resp.PRURL)
	} else if resp.Merged {
		fmt.Printf("🚌 Agent %s completed and merged to main\n", agentID)
	} else {
		fmt.Printf("🚌 Agent %s completed\n", agentID)
	}
	return nil
}

// Agent plan subcommand for managing planning agents
var agentPlanProject string

var agentPlanCmd = &cobra.Command{
	Use:   "plan [prompt]",
	Short: "Start a planning agent",
	Long: `Start a planning agent in plan mode to create implementation plans.

The planning agent will explore the codebase, design an implementation approach,
and write the plan via 'fab plan write' when complete.

Planning agents:
- Are visible in the TUI
- Can ask questions via AskUserQuestion
- Run in a worktree if --project is specified
- Are not subject to max-agents limit

Use 'fab tui' to interact with the planning agent.

Examples:
  fab agent plan "Add user authentication"
  fab agent plan --project myapp "Implement dark mode"
`,
	RunE: runAgentPlan,
}

func runAgentPlan(cmd *cobra.Command, args []string) error {
	// Get prompt from args
	var prompt string
	if len(args) > 0 {
		prompt = strings.Join(args, " ")
	} else {
		return fmt.Errorf("prompt is required: fab agent plan \"your planning task\"")
	}

	slog.Debug("plan: connecting to daemon")
	client := MustConnect()
	defer client.Close()
	slog.Debug("plan: connected to daemon")

	// Start the planning agent
	slog.Debug("plan: sending PlanStart request", "project", agentPlanProject, "prompt_len", len(prompt))
	resp, err := client.PlanStart(agentPlanProject, prompt)
	if err != nil {
		slog.Error("plan: PlanStart failed", "error", err)
		return fmt.Errorf("start planner: %w", err)
	}
	slog.Debug("plan: PlanStart succeeded", "id", resp.ID, "project", resp.Project, "workdir", resp.WorkDir)

	fmt.Printf("🚌 Planning agent started (ID: %s)\n", resp.ID)
	if resp.Project != "" {
		fmt.Printf("   Project: %s\n", resp.Project)
	}
	fmt.Printf("   Working directory: %s\n", resp.WorkDir)
	fmt.Println()
	fmt.Printf("Use 'fab tui' to interact with the agent.\n")

	return nil
}

var agentPlanListCmd = &cobra.Command{
	Use:   "list",
	Short: "List planning agents",
	RunE: func(cmd *cobra.Command, args []string) error {
		client := MustConnect()
		defer client.Close()

		resp, err := client.PlanList(agentPlanProject)
		if err != nil {
			return fmt.Errorf("list planners: %w", err)
		}

		if len(resp.Planners) == 0 {
			fmt.Println("No planning agents running")
			return nil
		}

		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		_, _ = fmt.Fprintln(w, " \tID\tPROJECT\tBACKEND\tDESCRIPTION\tAGE")

		for _, p := range resp.Planners {
			startedAt, _ := time.Parse(time.RFC3339, p.StartedAt)
			age := formatDuration(time.Since(startedAt))
			project := p.Project
			if project == "" {
				project = "-"
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
			icon := stateIcon(p.State)
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", icon, p.ID, project, backend, desc, age)
		}

		_ = w.Flush()
		return nil
	},
}

var agentPlanStopCmd = &cobra.Command{
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

func init() {
	agentListCmd.Flags().StringVarP(&agentListProject, "project", "p", "", "Filter by project name")
	agentCmd.AddCommand(agentListCmd)

	agentAbortCmd.Flags().BoolVarP(&abortForce, "force", "f", false, "Force kill immediately (SIGKILL)")
	agentAbortCmd.Flags().BoolVarP(&abortNoConfirm, "yes", "y", false, "Skip confirmation prompt")
	agentCmd.AddCommand(agentAbortCmd)

	agentCmd.AddCommand(agentClaimCmd)

	agentDoneCmd.Flags().StringVar(&doneErrorMsg, "error", "", "Error message if task failed")
	agentDoneCmd.Flags().StringVar(&doneTaskID, "task", "", "Task ID that was completed")
	agentCmd.AddCommand(agentDoneCmd)

	agentCmd.AddCommand(agentDescribeCmd)

	// Agent plan subcommands
	agentPlanCmd.Flags().StringVarP(&agentPlanProject, "project", "p", "", "Run in project worktree")
	agentPlanCmd.AddCommand(agentPlanListCmd)
	agentPlanCmd.AddCommand(agentPlanStopCmd)
	agentPlanListCmd.Flags().StringVarP(&agentPlanProject, "project", "p", "", "Filter by project")
	agentCmd.AddCommand(agentPlanCmd)

	rootCmd.AddCommand(agentCmd)
}
