package cli

import (
	"errors"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/daemon"
)

var statusShowAgents bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show daemon and project status",
	Long:  "Display the status of the fab daemon, registered projects, and running agents.",
	Args:  cobra.NoArgs,
	RunE:  runStatus,
}

func runStatus(cmd *cobra.Command, args []string) error {
	// Check if daemon is running
	client, err := ConnectClient()
	if err != nil {
		if errors.Is(err, ErrDaemonNotRunning) {
			fmt.Println("🚌 fab daemon is not running")
			return nil
		}
		return fmt.Errorf("connect to daemon: %w", err)
	}
	defer client.Close()

	status, err := client.Status()
	if err != nil {
		return fmt.Errorf("get status: %w", err)
	}

	// Daemon info
	uptime := time.Since(status.Daemon.StartedAt).Truncate(time.Second)
	fmt.Printf("🚌 fab daemon running (pid %d, uptime %s)\n", status.Daemon.PID, uptime)

	// Supervisor summary
	fmt.Printf("   Projects: %d active, Agents: %d running / %d total\n",
		status.Supervisor.ActiveProjects,
		status.Supervisor.RunningAgents,
		status.Supervisor.TotalAgents)
	fmt.Println()

	// Projects table
	if len(status.Projects) == 0 {
		fmt.Println("No projects registered.")
		fmt.Println("Add a project with: fab project add <path>")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "PROJECT\tSTATUS\tAGENTS\tPATH")
	for _, p := range status.Projects {
		projectStatus := "stopped"
		if p.Running {
			projectStatus = "running"
		}
		agentInfo := fmt.Sprintf("%d/%d", p.ActiveAgents, p.MaxAgents)
		_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", p.Name, projectStatus, agentInfo, p.Path)
	}
	_ = w.Flush()

	// Show agents if requested
	if statusShowAgents {
		fmt.Println()
		printAgents(status.Projects)
	}

	return nil
}

func printAgents(projects []daemon.ProjectStatus) {
	var hasAgents bool
	for _, p := range projects {
		if len(p.Agents) > 0 {
			hasAgents = true
			break
		}
	}

	if !hasAgents {
		fmt.Println("No agents running.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "AGENT\tPROJECT\tSTATE\tUPTIME\tTASK")
	for _, p := range projects {
		for _, a := range p.Agents {
			uptime := time.Since(a.StartedAt).Truncate(time.Second)
			task := a.Task
			if task == "" {
				task = "-"
			}
			_, _ = fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", a.ID, a.Project, a.State, uptime, task)
		}
	}
	_ = w.Flush()
}

func init() {
	statusCmd.Flags().BoolVarP(&statusShowAgents, "agents", "a", false, "Show agent details")
	rootCmd.AddCommand(statusCmd)
}
