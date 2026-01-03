package cli

import (
	"fmt"
	"os"
	"path/filepath"
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

func runProjectAdd(cmd *cobra.Command, args []string) error {
	path := args[0]

	// Resolve to absolute path
	absPath, err := filepath.Abs(path)
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

	client := MustConnect()
	defer client.Close()

	result, err := client.ProjectAdd(absPath, projectAddName, projectAddMaxAgents)
	if err != nil {
		return fmt.Errorf("add project: %w", err)
	}

	fmt.Printf("🚌 Added project: %s\n", result.Name)
	fmt.Printf("   Path: %s\n", result.Path)
	fmt.Printf("   Max agents: %d\n", result.MaxAgents)

	return nil
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
		fmt.Println("Add a project with: fab project add <path>")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tPATH\tAGENTS\tSTATUS")
	for _, p := range result.Projects {
		status := "stopped"
		if p.Running {
			status = "running"
		}
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", p.Name, p.Path, p.MaxAgents, status)
	}
	w.Flush()

	return nil
}

func init() {
	projectAddCmd.Flags().StringVarP(&projectAddName, "name", "n", "", "Project name (default: directory name)")
	projectAddCmd.Flags().IntVarP(&projectAddMaxAgents, "max-agents", "m", 3, "Maximum concurrent agents")

	projectCmd.AddCommand(projectAddCmd)
	projectCmd.AddCommand(projectListCmd)
	rootCmd.AddCommand(projectCmd)
}
