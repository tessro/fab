package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"github.com/tessro/fab/internal/config"
	"github.com/tessro/fab/internal/issue"
	"github.com/tessro/fab/internal/issue/gh"
	"github.com/tessro/fab/internal/issue/linear"
	"github.com/tessro/fab/internal/issue/tk"
	"github.com/tessro/fab/internal/registry"
)

var issueProject string

var issueCmd = &cobra.Command{
	Use:   "issue",
	Short: "Manage project issues",
	Long:  "Commands for managing issues using the configured backend (tk, github, etc.).",
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
	issueCreateDescription string
	issueCreateType        string
	issueCreatePriority    int
	issueCreateCommit      bool
	issueCreateDependsOn   []string
	issueCreateParent      string
)

var issueCreateCmd = &cobra.Command{
	Use:   "create <title>",
	Short: "Create a new issue",
	Long:  "Create a new issue. Use 'fab issue commit' to push changes.",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueCreate,
}

func runIssueCreate(cmd *cobra.Command, args []string) error {
	title := args[0]

	backend, err := getIssueBackend()
	if err != nil {
		return err
	}

	params := issue.CreateParams{
		Title:        title,
		Description:  issueCreateDescription,
		Type:         issueCreateType,
		Priority:     issueCreatePriority,
		Dependencies: issueCreateDependsOn,
	}

	var iss *issue.Issue

	if issueCreateParent != "" {
		// Create as sub-issue
		iss, err = backend.CreateSubIssue(context.Background(), issueCreateParent, params)
		if err != nil {
			if errors.Is(err, issue.ErrNotSupported) {
				return fmt.Errorf("issue backend %q does not support sub-issues", backend.Name())
			}
			return fmt.Errorf("create sub-issue: %w", err)
		}
		fmt.Printf("🚌 Created sub-issue: %s (parent: %s)\n", iss.ID, issueCreateParent)
	} else {
		// Create regular issue
		iss, err = backend.Create(context.Background(), params)
		if err != nil {
			return fmt.Errorf("create issue: %w", err)
		}
		fmt.Printf("🚌 Created issue: %s\n", iss.ID)
	}

	fmt.Printf("   Title: %s\n", iss.Title)

	if issueCreateCommit {
		if err := backend.Commit(context.Background()); err != nil {
			return fmt.Errorf("commit issues: %w", err)
		}
		fmt.Println("🚌 Issue changes committed and pushed")
	}

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
	Long:  "Update an issue's status, priority, or other fields. Use 'fab issue commit' to push changes.",
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
	Long:  "Mark an issue as closed. Use 'fab issue commit' to push changes.",
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

// issue commit

var issueCommitCmd = &cobra.Command{
	Use:   "commit",
	Short: "Commit and push issue changes",
	Long:  "Stage, commit, and push any pending issue changes to the remote repository.",
	RunE:  runIssueCommit,
}

func runIssueCommit(cmd *cobra.Command, args []string) error {
	backend, err := getIssueBackend()
	if err != nil {
		return err
	}

	if err := backend.Commit(context.Background()); err != nil {
		return fmt.Errorf("commit issues: %w", err)
	}

	fmt.Println("🚌 Issue changes committed and pushed")
	return nil
}

// issue comment

var issueCommentBody string

var issueCommentCmd = &cobra.Command{
	Use:   "comment <id>",
	Short: "Add a comment to an issue",
	Long:  "Add a comment to an issue. The comment body can be provided via --body flag.",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssueComment,
}

func runIssueComment(cmd *cobra.Command, args []string) error {
	backend, err := getIssueBackend()
	if err != nil {
		return err
	}

	// Check if backend supports collaboration features
	collab, ok := backend.(issue.IssueCollaborator)
	if !ok {
		return fmt.Errorf("issue backend %q does not support comments", backend.Name())
	}

	if err := collab.AddComment(context.Background(), args[0], issueCommentBody); err != nil {
		if errors.Is(err, issue.ErrNotSupported) {
			return fmt.Errorf("issue backend %q does not support comments", backend.Name())
		}
		return fmt.Errorf("add comment: %w", err)
	}

	fmt.Printf("🚌 Comment added to issue %s\n", args[0])
	return nil
}

// issue plan

var (
	issuePlanBody string
	issuePlanFile string
)

var issuePlanCmd = &cobra.Command{
	Use:   "plan <id>",
	Short: "Upsert a plan section in an issue",
	Long:  "Update or create a ## Plan section in the issue body. The plan content can be provided via --body or --file flag.",
	Args:  cobra.ExactArgs(1),
	RunE:  runIssuePlan,
}

func runIssuePlan(cmd *cobra.Command, args []string) error {
	var planContent string

	if issuePlanFile != "" && issuePlanBody != "" {
		return fmt.Errorf("cannot specify both --body and --file")
	}

	if issuePlanFile != "" {
		content, err := os.ReadFile(issuePlanFile)
		if err != nil {
			return fmt.Errorf("read plan file: %w", err)
		}
		planContent = string(content)
	} else if issuePlanBody != "" {
		planContent = issuePlanBody
	} else {
		return fmt.Errorf("plan content is required (use --body or --file)")
	}

	backend, err := getIssueBackend()
	if err != nil {
		return err
	}

	// Check if backend supports collaboration features
	collab, ok := backend.(issue.IssueCollaborator)
	if !ok {
		return fmt.Errorf("issue backend %q does not support plans", backend.Name())
	}

	if err := collab.UpsertPlanSection(context.Background(), args[0], planContent); err != nil {
		if errors.Is(err, issue.ErrNotSupported) {
			return fmt.Errorf("issue backend %q does not support plans", backend.Name())
		}
		return fmt.Errorf("upsert plan: %w", err)
	}

	fmt.Printf("🚌 Plan updated for issue %s\n", args[0])
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
	case "github", "gh":
		// Load global config to get GitHub API key
		globalCfg, err := config.LoadGlobalConfig()
		if err != nil {
			return nil, fmt.Errorf("load global config: %w", err)
		}
		apiKey := ""
		if globalCfg != nil {
			apiKey = globalCfg.GetAPIKey("github")
		}
		return gh.New(project.RepoDir(), project.AllowedAuthors, apiKey)
	case "linear":
		// Load global config to get Linear API key
		globalCfg, err := config.LoadGlobalConfig()
		if err != nil {
			return nil, fmt.Errorf("load global config: %w", err)
		}
		apiKey := ""
		if globalCfg != nil {
			apiKey = globalCfg.GetAPIKey("linear")
		}
		return linear.New(project.RepoDir(), project.LinearTeam, project.LinearProject, project.AllowedAuthors, apiKey)
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
	issueCreateCmd.Flags().StringVarP(&issueCreateDescription, "description", "d", "", "Issue description")
	issueCreateCmd.Flags().StringVar(&issueCreateType, "type", "task", "Issue type (task, bug, feature, chore)")
	issueCreateCmd.Flags().IntVar(&issueCreatePriority, "priority", 1, "Issue priority (0=low, 1=medium, 2=high)")
	issueCreateCmd.Flags().BoolVar(&issueCreateCommit, "commit", false, "Commit and push changes immediately")
	issueCreateCmd.Flags().StringSliceVar(&issueCreateDependsOn, "depends-on", nil, "Issue IDs this issue depends on (comma-separated)")
	issueCreateCmd.Flags().StringVar(&issueCreateParent, "parent", "", "Parent issue ID (creates a sub-issue)")

	// update flags
	issueUpdateCmd.Flags().StringVarP(&issueUpdateStatus, "status", "s", "", "New status (open, closed, blocked)")
	issueUpdateCmd.Flags().IntVar(&issueUpdatePriority, "priority", 0, "New priority (0=low, 1=medium, 2=high)")
	issueUpdateCmd.Flags().StringVarP(&issueUpdateTitle, "title", "t", "", "New title")

	// comment flags
	issueCommentCmd.Flags().StringVarP(&issueCommentBody, "body", "b", "", "Comment body")
	_ = issueCommentCmd.MarkFlagRequired("body")

	// plan flags
	issuePlanCmd.Flags().StringVarP(&issuePlanBody, "body", "b", "", "Plan content")
	issuePlanCmd.Flags().StringVarP(&issuePlanFile, "file", "f", "", "Read plan content from file")

	// Add subcommands
	issueCmd.AddCommand(issueListCmd)
	issueCmd.AddCommand(issueShowCmd)
	issueCmd.AddCommand(issueReadyCmd)
	issueCmd.AddCommand(issueCreateCmd)
	issueCmd.AddCommand(issueUpdateCmd)
	issueCmd.AddCommand(issueCloseCmd)
	issueCmd.AddCommand(issueCommitCmd)
	issueCmd.AddCommand(issueCommentCmd)
	issueCmd.AddCommand(issuePlanCmd)

	rootCmd.AddCommand(issueCmd)
}
