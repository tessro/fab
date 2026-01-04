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

var (
	doneErrorMsg string
	doneTaskID   string
)

var agentDoneCmd = &cobra.Command{
	Use:   "done",
	Short: "Signal that the agent has completed its task",
	Long:  "Called by Claude Code to signal task completion. Uses FAB_AGENT_ID env var.",
	RunE:  runAgentDone,
}

func runAgentDone(cmd *cobra.Command, args []string) error {
	agentID := os.Getenv("FAB_AGENT_ID")
	if agentID == "" {
		return fmt.Errorf("FAB_AGENT_ID environment variable not set")
	}

	client := MustConnect()
	defer client.Close()

	if err := client.AgentDone(agentID, doneTaskID, doneErrorMsg); err != nil {
		return fmt.Errorf("agent done: %w", err)
	}

	if doneErrorMsg != "" {
		fmt.Printf("🚌 Agent %s signaled error: %s\n", agentID, doneErrorMsg)
	} else {
		fmt.Printf("🚌 Agent %s signaled completion\n", agentID)
	}
	return nil
}

func init() {
	agentListCmd.Flags().StringVarP(&agentListProject, "project", "p", "", "Filter by project name")
	agentCmd.AddCommand(agentListCmd)

	agentDoneCmd.Flags().StringVar(&doneErrorMsg, "error", "", "Error message if task failed")
	agentDoneCmd.Flags().StringVar(&doneTaskID, "task", "", "Task ID that was completed")
	agentCmd.AddCommand(agentDoneCmd)

	rootCmd.AddCommand(agentCmd)
}
