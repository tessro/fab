package cli

import (
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
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
	fmt.Fprintln(w, "ID\tPROJECT\tSTATE\tTASK\tAGE")

	for _, a := range resp.Agents {
		age := formatDuration(time.Since(a.StartedAt))
		task := a.Task
		if task == "" {
			task = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", a.ID, a.Project, a.State, task, age)
	}

	w.Flush()
	return nil
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

var doneReason string

var agentDoneCmd = &cobra.Command{
	Use:   "done",
	Short: "Signal task completion to the orchestrator",
	Long: `Signal to the orchestrator that this agent has completed its task.

This command is typically called by agents after finishing their work:
  1. Run quality gates (tests, linting, etc.)
  2. Push changes
  3. Close the task with 'bd close <id>'
  4. Run 'fab agent done'

The orchestrator will clean up the agent and spawn a new one if capacity is available.`,
	RunE: runAgentDone,
}

func runAgentDone(cmd *cobra.Command, args []string) error {
	client := MustConnect()
	defer client.Close()

	if err := client.AgentDone(doneReason); err != nil {
		return fmt.Errorf("agent done: %w", err)
	}

	fmt.Println("🚌 Agent done signaled")
	return nil
}

func init() {
	agentListCmd.Flags().StringVarP(&agentListProject, "project", "p", "", "Filter by project name")
	agentCmd.AddCommand(agentListCmd)

	agentDoneCmd.Flags().StringVar(&doneReason, "reason", "", "Optional completion reason")
	agentCmd.AddCommand(agentDoneCmd)

	rootCmd.AddCommand(agentCmd)
}
