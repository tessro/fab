package cli

import (
	"fmt"
	"os"
	"os/exec"
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
	_, _ = fmt.Fprintln(w, "ID\tPROJECT\tSTATE\tTASK\tDESCRIPTION\tAGE")

	for _, a := range resp.Agents {
		age := formatDuration(time.Since(a.StartedAt))
		task := a.Task
		if task == "" {
			task = "-"
		}
		desc := a.Description
		if desc == "" {
			desc = "-"
		}
		// Truncate long descriptions for display
		if len(desc) > 40 {
			desc = desc[:37] + "..."
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", a.ID, a.Project, a.State, task, desc, age)
	}

	_ = w.Flush()
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

	// Confirm unless --yes is specified
	if !abortNoConfirm {
		action := "gracefully abort"
		if abortForce {
			action = "force kill"
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

	if err := client.AgentAbort(agentID, abortForce); err != nil {
		return fmt.Errorf("abort agent: %w", err)
	}

	if abortForce {
		fmt.Printf("🚌 Force killed agent %s\n", agentID)
	} else {
		fmt.Printf("🚌 Sent quit to agent %s\n", agentID)
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

	fmt.Println("🚌 Rebase successful, merging to main...")

	client := MustConnect()
	defer client.Close()

	if err := client.AgentDone(agentID, doneTaskID, doneErrorMsg); err != nil {
		return fmt.Errorf("agent done: %w", err)
	}

	if doneErrorMsg != "" {
		fmt.Printf("🚌 Agent %s signaled error: %s\n", agentID, doneErrorMsg)
	} else {
		fmt.Printf("🚌 Agent %s completed and merged to main\n", agentID)
	}
	return nil
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

	rootCmd.AddCommand(agentCmd)
}
