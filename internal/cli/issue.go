package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/issue"
	"github.com/tessro/fab/internal/issue/tk"
	"github.com/tessro/fab/internal/registry"
)

var issueProject string

var issueCmd = &cobra.Command{
	Use:   "issue",
	Short: "Manage project issues",
	Long:  "Commands for managing issues using the configured backend (tk, linear, etc.).",
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		// Resolve project context before any subcommand
		resolved, err := issue.ResolveProject(issueProject)
		if err != nil {
			return fmt.Errorf("could not determine project: %w\nUse --project flag or run from a project directory", err)
		}
		issueProject = resolved
		return nil
	},
}

// issue list

var issueListStatus string

var issueListCmd = &cobra.Command{
	Use:   "list",
	Short: "List issues",
	Long:  "List issues with optional filters.",
	RunE:  runIssueList,
}

func runIssueList(cmd *cobra.Command, args []string) error {
	backend, err := getIssueBackend()
	if err != nil {
		return err
	}

	filter := issue.ListFilter{}
	if issueListStatus != "" {
		filter.Status = []issue.Status{issue.Status(issueListStatus)}
	}

	issues, err := backend.List(context.Background(), filter)
	if err != nil {
		return fmt.Errorf("list issues: %w", err)
	}

	if len(issues) == 0 {
		fmt.Println("No issues found")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tSTATUS\tPRI\tTITLE")

	for _, iss := range issues {
		title := iss.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}
		_, _ = fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", iss.ID, iss.Status, iss.Priority, title)
	}

	_ = w.Flush()
	return nil
}

// issue show

var issueShowCmd = &cobra.Command{
	Use:   "show <id>",
	Short: "Show issue details",
	Long:  "Show detailed information about an issue.",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueShow,
}

func runIssueShow(cmd *cobra.Command, args []string) error {
	backend, err := getIssueBackend()
	if err != nil {
		return err
	}

	iss, err := backend.Get(context.Background(), args[0])
	if err != nil {
		return fmt.Errorf("get issue: %w", err)
	}

	fmt.Printf("ID:       %s\n", iss.ID)
	fmt.Printf("Title:    %s\n", iss.Title)
	fmt.Printf("Status:   %s\n", iss.Status)
	fmt.Printf("Priority: %d\n", iss.Priority)
	fmt.Printf("Type:     %s\n", iss.Type)
	fmt.Printf("Created:  %s\n", iss.Created.Format("2006-01-02 15:04"))

	if len(iss.Dependencies) > 0 {
		fmt.Printf("Deps:     %s\n", strings.Join(iss.Dependencies, ", "))
	}
	if len(iss.Labels) > 0 {
		fmt.Printf("Labels:   %s\n", strings.Join(iss.Labels, ", "))
	}

	if iss.Description != "" {
		fmt.Println()
		fmt.Println(iss.Description)
	}

	return nil
}

// issue ready

var issueReadyCmd = &cobra.Command{
	Use:   "ready",
	Short: "List issues ready to work on",
	Long:  "List open issues with no open dependencies.",
	RunE:  runIssueReady,
}

func runIssueReady(cmd *cobra.Command, args []string) error {
	backend, err := getIssueBackend()
	if err != nil {
		return err
	}

	issues, err := backend.Ready(context.Background())
	if err != nil {
		return fmt.Errorf("list ready issues: %w", err)
	}

	if len(issues) == 0 {
		fmt.Println("No ready issues")
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	_, _ = fmt.Fprintln(w, "ID\tPRI\tTITLE")

	for _, iss := range issues {
		title := iss.Title
		if len(title) > 50 {
			title = title[:47] + "..."
		}
		_, _ = fmt.Fprintf(w, "%s\t%d\t%s\n", iss.ID, iss.Priority, title)
	}

	_ = w.Flush()
	return nil
}

// issue create

var (
	issueCreateTitle       string
	issueCreateDescription string
	issueCreateType        string
	issueCreatePriority    int
)

var issueCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a new issue",
	Long:  "Create a new issue. Commits and pushes immediately.",
	RunE:  runIssueCreate,
}

func runIssueCreate(cmd *cobra.Command, args []string) error {
	if issueCreateTitle == "" {
		return fmt.Errorf("--title is required")
	}

	backend, err := getIssueBackend()
	if err != nil {
		return err
	}

	params := issue.CreateParams{
		Title:       issueCreateTitle,
		Description: issueCreateDescription,
		Type:        issueCreateType,
		Priority:    issueCreatePriority,
	}

	iss, err := backend.Create(context.Background(), params)
	if err != nil {
		return fmt.Errorf("create issue: %w", err)
	}

	fmt.Printf("🚌 Created issue: %s\n", iss.ID)
	fmt.Printf("   Title: %s\n", iss.Title)
	return nil
}

// issue update

var (
	issueUpdateStatus   string
	issueUpdatePriority int
	issueUpdateTitle    string
)

var issueUpdateCmd = &cobra.Command{
	Use:   "update <id>",
	Short: "Update an issue",
	Long:  "Update an issue's status, priority, or other fields. Commits and pushes immediately.",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueUpdate,
}

func runIssueUpdate(cmd *cobra.Command, args []string) error {
	backend, err := getIssueBackend()
	if err != nil {
		return err
	}

	params := issue.UpdateParams{}

	if cmd.Flags().Changed("status") {
		s := issue.Status(issueUpdateStatus)
		params.Status = &s
	}
	if cmd.Flags().Changed("priority") {
		params.Priority = &issueUpdatePriority
	}
	if cmd.Flags().Changed("title") {
		params.Title = &issueUpdateTitle
	}

	iss, err := backend.Update(context.Background(), args[0], params)
	if err != nil {
		return fmt.Errorf("update issue: %w", err)
	}

	fmt.Printf("🚌 Updated issue: %s\n", iss.ID)
	return nil
}

// issue close

var issueCloseCmd = &cobra.Command{
	Use:   "close <id>",
	Short: "Close an issue",
	Long:  "Mark an issue as closed. Commits and pushes immediately.",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueClose,
}

func runIssueClose(cmd *cobra.Command, args []string) error {
	backend, err := getIssueBackend()
	if err != nil {
		return err
	}

	if err := backend.Close(context.Background(), args[0]); err != nil {
		return fmt.Errorf("close issue: %w", err)
	}

	fmt.Printf("🚌 Closed issue: %s\n", args[0])
	return nil
}

// getIssueBackend returns the issue backend for the resolved project.
func getIssueBackend() (issue.Backend, error) {
	reg, err := registry.New()
	if err != nil {
		return nil, fmt.Errorf("load registry: %w", err)
	}

	project, err := reg.Get(issueProject)
	if err != nil {
		return nil, fmt.Errorf("get project: %w", err)
	}

	// Default to tk backend
	backendType := project.IssueBackend
	if backendType == "" {
		backendType = "tk"
	}

	switch backendType {
	case "tk":
		return tk.New(project.RepoDir())
	default:
		return nil, fmt.Errorf("unknown issue backend: %s", backendType)
	}
}

func init() {
	// Parent command flag
	issueCmd.PersistentFlags().StringVarP(&issueProject, "project", "p", "", "Project name (default: detect from cwd)")

	// list flags
	issueListCmd.Flags().StringVarP(&issueListStatus, "status", "s", "", "Filter by status (open, closed, blocked)")

	// create flags
	issueCreateCmd.Flags().StringVarP(&issueCreateTitle, "title", "t", "", "Issue title (required)")
	issueCreateCmd.Flags().StringVarP(&issueCreateDescription, "description", "d", "", "Issue description")
	issueCreateCmd.Flags().StringVar(&issueCreateType, "type", "task", "Issue type (task, bug, feature, chore)")
	issueCreateCmd.Flags().IntVar(&issueCreatePriority, "priority", 1, "Issue priority (0=low, 1=medium, 2=high)")

	// update flags
	issueUpdateCmd.Flags().StringVarP(&issueUpdateStatus, "status", "s", "", "New status (open, closed, blocked)")
	issueUpdateCmd.Flags().IntVar(&issueUpdatePriority, "priority", 0, "New priority (0=low, 1=medium, 2=high)")
	issueUpdateCmd.Flags().StringVarP(&issueUpdateTitle, "title", "t", "", "New title")

	// Add subcommands
	issueCmd.AddCommand(issueListCmd)
	issueCmd.AddCommand(issueShowCmd)
	issueCmd.AddCommand(issueReadyCmd)
	issueCmd.AddCommand(issueCreateCmd)
	issueCmd.AddCommand(issueUpdateCmd)
	issueCmd.AddCommand(issueCloseCmd)

	rootCmd.AddCommand(issueCmd)
}
