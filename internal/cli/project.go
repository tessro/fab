package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

var projectCmd = &cobra.Command{
	Use:   "project",
	Short: "Manage projects",
	Long:  "Commands for managing projects registered with the fab daemon.",
}

var projectAddName string
var projectAddMaxAgents int
var projectAddAutostart bool

var projectAddCmd = &cobra.Command{
	Use:   "add <path>",
	Short: "Add a project to fab",
	Long:  "Register a project directory with the fab daemon for agent orchestration.",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectAdd,
}

var projectListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all projects",
	Long:  "List all projects registered with the fab daemon.",
	Args:  cobra.NoArgs,
	RunE:  runProjectList,
}

var projectStartAll bool

var projectStartCmd = &cobra.Command{
	Use:   "start [project]",
	Short: "Start orchestration for a project",
	Long:  "Start agent orchestration for a registered project. Agents will pick up tasks and work on them.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runProjectStart,
}

var projectStopAll bool

var projectStopCmd = &cobra.Command{
	Use:   "stop [project]",
	Short: "Stop orchestration for a project",
	Long:  "Stop agent orchestration for the specified project. Running agents will be gracefully stopped.",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runProjectStop,
}

var projectRemoveForce bool
var projectRemoveDeleteWorktrees bool

var projectRemoveCmd = &cobra.Command{
	Use:   "remove <name>",
	Short: "Remove a project from fab",
	Long:  "Unregister a project from the fab daemon. Optionally delete associated worktrees.",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectRemove,
}

var projectSetMaxAgents int
var projectSetAutostart bool

var projectSetCmd = &cobra.Command{
	Use:   "set <name>",
	Short: "Update project settings",
	Long:  "Update settings for a registered project, such as the maximum number of concurrent agents.",
	Args:  cobra.ExactArgs(1),
	RunE:  runProjectSet,
}

func runProjectAdd(cmd *cobra.Command, args []string) error {
	input := args[0]

	var remoteURL string

	// Check if input looks like a git URL
	if isGitURL(input) {
		remoteURL = input
	} else {
		// Treat as local path, extract remote URL
		absPath, err := filepath.Abs(input)
		if err != nil {
			return fmt.Errorf("resolve path: %w", err)
		}

		// Verify path exists and is a directory
		info, err := os.Stat(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("path does not exist: %s", absPath)
			}
			return fmt.Errorf("stat path: %w", err)
		}
		if !info.IsDir() {
			return fmt.Errorf("path is not a directory: %s", absPath)
		}

		// Extract remote URL from local repo
		remoteURL, err = getRemoteURL(absPath)
		if err != nil {
			return fmt.Errorf("get remote URL from %s: %w", absPath, err)
		}
	}

	client := MustConnect()
	defer client.Close()

	result, err := client.ProjectAdd(remoteURL, projectAddName, projectAddMaxAgents, projectAddAutostart)
	if err != nil {
		return fmt.Errorf("add project: %w", err)
	}

	fmt.Printf("🚌 Added project: %s\n", result.Name)
	fmt.Printf("   Remote: %s\n", result.RemoteURL)
	fmt.Printf("   Clone:  %s\n", result.RepoDir)
	fmt.Printf("   Max agents: %d\n", result.MaxAgents)
	if projectAddAutostart {
		fmt.Println("   Autostart: enabled")
	}

	return nil
}

// isGitURL returns true if the string looks like a git URL.
func isGitURL(s string) bool {
	return strings.Contains(s, "://") || strings.HasPrefix(s, "git@")
}

// getRemoteURL extracts the origin remote URL from a local git repository.
func getRemoteURL(path string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = path
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("no git remote: %w", err)
	}
	return strings.TrimSpace(string(output)), nil
}

func runProjectList(cmd *cobra.Command, args []string) error {
	client := MustConnect()
	defer client.Close()

	result, err := client.ProjectList()
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	if len(result.Projects) == 0 {
		fmt.Println("No projects registered.")
		fmt.Println("Add a project with: fab project add <url-or-path>")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "NAME\tREMOTE\tAGENTS\tSTATUS")
	for _, p := range result.Projects {
		status := "stopped"
		if p.Running {
			status = "running"
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", p.Name, p.RemoteURL, p.MaxAgents, status)
	}
	_ = w.Flush()

	return nil
}

func runProjectStart(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && !projectStartAll {
		return fmt.Errorf("specify a project name or use --all")
	}

	client := MustConnect()
	defer client.Close()

	var project string
	if len(args) > 0 {
		project = args[0]
	}

	if err := client.Start(project, projectStartAll); err != nil {
		return fmt.Errorf("start: %w", err)
	}

	if projectStartAll {
		fmt.Println("🚌 Started orchestration for all projects")
	} else {
		fmt.Printf("🚌 Started orchestration for project: %s\n", project)
	}
	return nil
}

func runProjectStop(cmd *cobra.Command, args []string) error {
	if len(args) == 0 && !projectStopAll {
		return fmt.Errorf("specify a project name or use --all")
	}

	client := MustConnect()
	defer client.Close()

	var project string
	if len(args) > 0 {
		project = args[0]
	}

	if err := client.Stop(project, projectStopAll); err != nil {
		return fmt.Errorf("stop: %w", err)
	}

	if projectStopAll {
		fmt.Println("🚌 Stopped orchestration for all projects")
	} else {
		fmt.Printf("🚌 Stopped orchestration for project: %s\n", project)
	}
	return nil
}

func runProjectRemove(cmd *cobra.Command, args []string) error {
	projectName := args[0]

	client := MustConnect()
	defer client.Close()

	// Check if project exists and get info
	result, err := client.ProjectList()
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	var found bool
	var project struct {
		Running   bool
		RemoteURL string
	}
	for _, p := range result.Projects {
		if p.Name == projectName {
			found = true
			project.Running = p.Running
			project.RemoteURL = p.RemoteURL
			break
		}
	}

	if !found {
		return fmt.Errorf("project not found: %s", projectName)
	}

	// Check for running agents
	if project.Running {
		return fmt.Errorf("project %s has running agents; stop it first with: fab project stop %s", projectName, projectName)
	}

	// Confirm with user unless --force
	if !projectRemoveForce {
		fmt.Printf("Remove project %s?\n", projectName)
		fmt.Printf("   Remote: %s\n", project.RemoteURL)
		if projectRemoveDeleteWorktrees {
			fmt.Println("   Worktrees will be deleted")
		}
		fmt.Print("Type 'yes' to confirm: ")

		var confirm string
		_, _ = fmt.Scanln(&confirm)
		if confirm != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	if err := client.ProjectRemove(projectName, projectRemoveDeleteWorktrees); err != nil {
		return fmt.Errorf("remove project: %w", err)
	}

	fmt.Printf("🚌 Removed project: %s\n", projectName)
	if projectRemoveDeleteWorktrees {
		fmt.Println("   Worktrees deleted")
	}
	return nil
}

func runProjectSet(cmd *cobra.Command, args []string) error {
	projectName := args[0]

	// Check if any flags were provided
	if !cmd.Flags().Changed("max-agents") && !cmd.Flags().Changed("autostart") {
		return fmt.Errorf("no settings to update; use --max-agents or --autostart")
	}

	client := MustConnect()
	defer client.Close()

	var maxAgents *int
	if cmd.Flags().Changed("max-agents") {
		maxAgents = &projectSetMaxAgents
	}

	var autostart *bool
	if cmd.Flags().Changed("autostart") {
		autostart = &projectSetAutostart
	}

	if err := client.ProjectSet(projectName, maxAgents, autostart); err != nil {
		return fmt.Errorf("set project: %w", err)
	}

	fmt.Printf("🚌 Updated project: %s\n", projectName)
	if maxAgents != nil {
		fmt.Printf("   Max agents: %d\n", *maxAgents)
	}
	if autostart != nil {
		if *autostart {
			fmt.Println("   Autostart: enabled")
		} else {
			fmt.Println("   Autostart: disabled")
		}
	}
	return nil
}

func init() {
	projectAddCmd.Flags().StringVarP(&projectAddName, "name", "n", "", "Project name (default: directory name)")
	projectAddCmd.Flags().IntVarP(&projectAddMaxAgents, "max-agents", "m", 3, "Maximum concurrent agents")
	projectAddCmd.Flags().BoolVar(&projectAddAutostart, "autostart", false, "Start orchestration when daemon starts")

	projectStartCmd.Flags().BoolVarP(&projectStartAll, "all", "a", false, "Start all projects")
	projectStopCmd.Flags().BoolVarP(&projectStopAll, "all", "a", false, "Stop all projects")

	projectRemoveCmd.Flags().BoolVarP(&projectRemoveForce, "force", "f", false, "Skip confirmation prompt")
	projectRemoveCmd.Flags().BoolVar(&projectRemoveDeleteWorktrees, "delete-worktrees", false, "Delete associated worktrees")

	projectSetCmd.Flags().IntVarP(&projectSetMaxAgents, "max-agents", "m", 0, "Maximum concurrent agents")
	projectSetCmd.Flags().BoolVar(&projectSetAutostart, "autostart", false, "Start orchestration when daemon starts")

	projectCmd.AddCommand(projectAddCmd)
	projectCmd.AddCommand(projectListCmd)
	projectCmd.AddCommand(projectStartCmd)
	projectCmd.AddCommand(projectStopCmd)
	projectCmd.AddCommand(projectRemoveCmd)
	projectCmd.AddCommand(projectSetCmd)
	rootCmd.AddCommand(projectCmd)
}
