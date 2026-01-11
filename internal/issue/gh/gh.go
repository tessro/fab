// Package gh provides a GitHub Issues backend using the gh CLI.
package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tessro/fab/internal/issue"
)

// Backend implements issue.Backend for GitHub Issues using the gh CLI.
type Backend struct {
	repoDir        string   // Path to a git repository with a GitHub remote
	nwo            string   // GitHub owner/repo (e.g., "owner/repo")
	allowedAuthors []string // GitHub usernames allowed to create issues (empty = owner only)
}

// New creates a new GitHub issues backend.
// repoDir should be a git repository with a GitHub remote.
// allowedAuthors is a list of GitHub usernames allowed to create issues.
// If empty, defaults to the repository owner inferred from the remote URL.
func New(repoDir string, allowedAuthors []string) (*Backend, error) {
	// Extract owner/repo from the git remote
	nwo, err := detectNWO(repoDir)
	if err != nil {
		return nil, fmt.Errorf("detect github repo: %w", err)
	}

	// Default to repo owner if no allowed authors specified
	if len(allowedAuthors) == 0 {
		owner := ownerFromNWO(nwo)
		if owner != "" {
			allowedAuthors = []string{owner}
		}
	}

	return &Backend{
		repoDir:        repoDir,
		nwo:            nwo,
		allowedAuthors: allowedAuthors,
	}, nil
}

// ownerFromNWO extracts the owner from an owner/repo string.
func ownerFromNWO(nwo string) string {
	parts := strings.Split(nwo, "/")
	if len(parts) >= 1 {
		return parts[0]
	}
	return ""
}

// Name returns the backend identifier.
func (b *Backend) Name() string {
	return "github"
}

// ghIssue represents a GitHub issue from the gh CLI JSON output.
type ghIssue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"` // OPEN, CLOSED
	Labels    []ghLabel `json:"labels"`
	Author    ghAuthor  `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ghLabel struct {
	Name string `json:"name"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

// Create creates a new issue on GitHub.
func (b *Backend) Create(ctx context.Context, params issue.CreateParams) (*issue.Issue, error) {
	args := []string{"issue", "create", "--repo", b.nwo, "--title", params.Title, "--body", params.Description}

	// Add type label
	issueType := params.Type
	if issueType == "" {
		issueType = "task"
	}
	args = append(args, "--label", "type:"+issueType)

	// Add priority label
	args = append(args, "--label", fmt.Sprintf("priority:%d", params.Priority))

	// Run gh issue create and get the issue URL
	out, err := b.runGH(ctx, args...)
	if err != nil {
		return nil, err
	}

	// Parse the issue number from the output URL
	// Output looks like: https://github.com/owner/repo/issues/123
	num, err := parseIssueNumberFromURL(strings.TrimSpace(out))
	if err != nil {
		return nil, fmt.Errorf("parse created issue: %w", err)
	}

	// Set up dependencies using GitHub sub-issues
	// The new issue becomes a sub-issue of each dependency (parent)
	for _, depID := range params.Dependencies {
		if err := b.addSubIssue(ctx, depID, strconv.Itoa(num)); err != nil {
			// Log warning but don't fail the create
			fmt.Fprintf(issue.Stderr, "warning: failed to add dependency %s: %v\n", depID, err)
		}
	}

	return b.Get(ctx, strconv.Itoa(num))
}

// Get retrieves an issue by ID (issue number as string).
func (b *Backend) Get(ctx context.Context, id string) (*issue.Issue, error) {
	out, err := b.runGH(ctx, "issue", "view", id, "--repo", b.nwo, "--json",
		"number,title,body,state,labels,author,createdAt,updatedAt")
	if err != nil {
		return nil, fmt.Errorf("get issue %s: %w", id, err)
	}

	var gh ghIssue
	if err := json.Unmarshal([]byte(out), &gh); err != nil {
		return nil, fmt.Errorf("parse issue: %w", err)
	}

	iss := b.toIssue(&gh)

	// Fetch parent issue (dependency) if this issue is a sub-issue
	if parent, err := b.getParentIssue(ctx, id); err == nil && parent != "" {
		iss.Dependencies = []string{parent}
	}

	return iss, nil
}

// List returns issues matching the filter.
func (b *Backend) List(ctx context.Context, filter issue.ListFilter) ([]*issue.Issue, error) {
	args := []string{"issue", "list", "--repo", b.nwo, "--json",
		"number,title,body,state,labels,author,createdAt,updatedAt", "--limit", "100"}

	// Apply status filter
	if len(filter.Status) > 0 {
		// gh CLI uses --state with values: open, closed, all
		states := make([]string, 0, len(filter.Status))
		for _, s := range filter.Status {
			switch s {
			case issue.StatusOpen, issue.StatusBlocked:
				states = append(states, "open")
			case issue.StatusClosed:
				states = append(states, "closed")
			}
		}
		if len(states) > 0 {
			// If both open and closed, use all
			hasOpen := false
			hasClosed := false
			for _, s := range states {
				if s == "open" {
					hasOpen = true
				}
				if s == "closed" {
					hasClosed = true
				}
			}
			if hasOpen && hasClosed {
				args = append(args, "--state", "all")
			} else if hasClosed {
				args = append(args, "--state", "closed")
			}
			// open is default, no need to specify
		}
	}

	// Apply label filter
	for _, label := range filter.Labels {
		args = append(args, "--label", label)
	}

	out, err := b.runGH(ctx, args...)
	if err != nil {
		return nil, err
	}

	var ghIssues []ghIssue
	if err := json.Unmarshal([]byte(out), &ghIssues); err != nil {
		return nil, fmt.Errorf("parse issues: %w", err)
	}

	issues := make([]*issue.Issue, 0, len(ghIssues))
	for _, gh := range ghIssues {
		issues = append(issues, b.toIssue(&gh))
	}

	return issues, nil
}

// Update modifies an existing issue.
func (b *Backend) Update(ctx context.Context, id string, params issue.UpdateParams) (*issue.Issue, error) {
	args := []string{"issue", "edit", id, "--repo", b.nwo}

	if params.Title != nil {
		args = append(args, "--title", *params.Title)
	}
	if params.Description != nil {
		args = append(args, "--body", *params.Description)
	}

	// Handle labels
	if params.Labels != nil {
		// Get current issue to manage label updates
		current, err := b.Get(ctx, id)
		if err != nil {
			return nil, err
		}

		// Remove old labels and add new ones
		for _, label := range current.Labels {
			args = append(args, "--remove-label", label)
		}
		for _, label := range params.Labels {
			args = append(args, "--add-label", label)
		}
	}

	// Update type via label if specified
	if params.Type != nil {
		// Get current to find old type label
		current, err := b.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, label := range current.Labels {
			if strings.HasPrefix(label, "type:") {
				args = append(args, "--remove-label", label)
				break
			}
		}
		args = append(args, "--add-label", "type:"+*params.Type)
	}

	// Update priority via label if specified
	if params.Priority != nil {
		// Get current to find old priority label
		current, err := b.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		for _, label := range current.Labels {
			if strings.HasPrefix(label, "priority:") {
				args = append(args, "--remove-label", label)
				break
			}
		}
		args = append(args, "--add-label", fmt.Sprintf("priority:%d", *params.Priority))
	}

	if len(args) > 4 { // Only run if there are actual edits
		if _, err := b.runGH(ctx, args...); err != nil {
			return nil, err
		}
	}

	// Handle status change separately (close/reopen)
	if params.Status != nil {
		switch *params.Status {
		case issue.StatusClosed:
			if _, err := b.runGH(ctx, "issue", "close", id, "--repo", b.nwo); err != nil {
				return nil, err
			}
		case issue.StatusOpen:
			if _, err := b.runGH(ctx, "issue", "reopen", id, "--repo", b.nwo); err != nil {
				return nil, err
			}
		case issue.StatusBlocked:
			// Mark as blocked using a label
			if _, err := b.runGH(ctx, "issue", "edit", id, "--repo", b.nwo, "--add-label", "blocked"); err != nil {
				return nil, err
			}
		}
	}

	return b.Get(ctx, id)
}

// Close marks an issue as closed.
func (b *Backend) Close(ctx context.Context, id string) error {
	_, err := b.runGH(ctx, "issue", "close", id, "--repo", b.nwo)
	return err
}

// Ready returns open issues that are not blocked, have no open dependencies, and are authored by allowed users.
func (b *Backend) Ready(ctx context.Context) ([]*issue.Issue, error) {
	// Fetch raw issues to access author info for filtering
	args := []string{"issue", "list", "--repo", b.nwo, "--json",
		"number,title,body,state,labels,author,createdAt,updatedAt", "--limit", "100", "--state", "open"}

	out, err := b.runGH(ctx, args...)
	if err != nil {
		return nil, err
	}

	var ghIssues []ghIssue
	if err := json.Unmarshal([]byte(out), &ghIssues); err != nil {
		return nil, fmt.Errorf("parse issues: %w", err)
	}

	// Build allowed authors set for quick lookup
	allowedSet := make(map[string]bool, len(b.allowedAuthors))
	for _, author := range b.allowedAuthors {
		allowedSet[strings.ToLower(author)] = true
	}

	// Build a set of open issue numbers for dependency checking
	openIssues := make(map[string]bool, len(ghIssues))
	for _, gh := range ghIssues {
		openIssues[strconv.Itoa(gh.Number)] = true
	}

	// Filter: not blocked + authored by allowed user + no open dependencies
	ready := make([]*issue.Issue, 0, len(ghIssues))
	for _, gh := range ghIssues {
		iss := b.toIssue(&gh)

		// Skip blocked issues
		if iss.Status == issue.StatusBlocked {
			continue
		}

		// Skip issues not authored by allowed users
		if len(allowedSet) > 0 && !allowedSet[strings.ToLower(gh.Author.Login)] {
			continue
		}

		// Check if this issue has an open parent (dependency)
		// If it's a sub-issue of an open issue, it's not ready
		parent, err := b.getParentIssue(ctx, strconv.Itoa(gh.Number))
		if err == nil && parent != "" && openIssues[parent] {
			continue // Has open dependency, not ready
		}

		ready = append(ready, iss)
	}

	return ready, nil
}

// Commit is a no-op for GitHub issues since changes are immediate.
func (b *Backend) Commit(ctx context.Context) error {
	// GitHub issues are updated immediately via API, no commit needed
	return nil
}

// toIssue converts a GitHub issue to our Issue type.
func (b *Backend) toIssue(gh *ghIssue) *issue.Issue {
	iss := &issue.Issue{
		ID:      strconv.Itoa(gh.Number),
		Title:   gh.Title,
		Created: gh.CreatedAt,
		Updated: gh.UpdatedAt,
	}

	// Extract description (first part of body before any metadata)
	iss.Description = gh.Body

	// Determine status
	if gh.State == "CLOSED" {
		iss.Status = issue.StatusClosed
	} else {
		iss.Status = issue.StatusOpen
	}

	// Parse labels for type, priority, and blocked status
	for _, label := range gh.Labels {
		name := label.Name
		switch {
		case strings.HasPrefix(name, "type:"):
			iss.Type = strings.TrimPrefix(name, "type:")
		case strings.HasPrefix(name, "priority:"):
			p, _ := strconv.Atoi(strings.TrimPrefix(name, "priority:"))
			iss.Priority = p
		case name == "blocked":
			iss.Status = issue.StatusBlocked
		default:
			iss.Labels = append(iss.Labels, name)
		}
	}

	// Default type if not set
	if iss.Type == "" {
		iss.Type = "task"
	}

	return iss
}

// runGH executes a gh command and returns the output.
func (b *Backend) runGH(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "gh", args...)
	cmd.Dir = b.repoDir

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("gh %s: %w: %s", strings.Join(args[:2], " "), err, stderr.String())
	}

	return stdout.String(), nil
}

// detectNWO extracts the owner/repo from a git repository.
func detectNWO(repoDir string) (string, error) {
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = repoDir

	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("get remote URL: %w", err)
	}

	return parseNWO(strings.TrimSpace(string(out)))
}

// parseNWO extracts owner/repo from a GitHub URL.
// Supports SSH (git@github.com:owner/repo.git) and HTTPS (https://github.com/owner/repo.git).
func parseNWO(url string) (string, error) {
	// SSH format: git@github.com:owner/repo.git
	if strings.HasPrefix(url, "git@github.com:") {
		nwo := strings.TrimPrefix(url, "git@github.com:")
		nwo = strings.TrimSuffix(nwo, ".git")
		return nwo, nil
	}

	// HTTPS format: https://github.com/owner/repo.git
	re := regexp.MustCompile(`https://github\.com/([^/]+/[^/]+?)(?:\.git)?$`)
	matches := re.FindStringSubmatch(url)
	if len(matches) == 2 {
		return matches[1], nil
	}

	return "", fmt.Errorf("not a GitHub URL: %s", url)
}

// parseIssueNumberFromURL extracts the issue number from a GitHub issue URL.
func parseIssueNumberFromURL(url string) (int, error) {
	// Format: https://github.com/owner/repo/issues/123
	re := regexp.MustCompile(`/issues/(\d+)$`)
	matches := re.FindStringSubmatch(url)
	if len(matches) != 2 {
		return 0, fmt.Errorf("invalid issue URL: %s", url)
	}
	return strconv.Atoi(matches[1])
}

// addSubIssue adds childNum as a sub-issue of parentNum using GitHub's REST API.
// This creates a dependency: child depends on parent (parent must complete first).
func (b *Backend) addSubIssue(ctx context.Context, parentNum, childNum string) error {
	// First, get the internal ID of the child issue (not the node_id)
	childID, err := b.getIssueID(ctx, childNum)
	if err != nil {
		return fmt.Errorf("get child issue ID: %w", err)
	}

	// Use the REST API to add the sub-issue
	// POST /repos/{owner}/{repo}/issues/{issue_number}/sub_issues
	endpoint := fmt.Sprintf("repos/%s/issues/%s/sub_issues", b.nwo, parentNum)
	cmd := exec.CommandContext(ctx, "gh", "api", endpoint, "-X", "POST", "-f", fmt.Sprintf("sub_issue_id=%d", childID))
	cmd.Dir = b.repoDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("add sub-issue: %w: %s", err, stderr.String())
	}
	return nil
}

// getIssueID retrieves the internal numeric ID for an issue (not the node_id).
func (b *Backend) getIssueID(ctx context.Context, issueNum string) (int64, error) {
	endpoint := fmt.Sprintf("repos/%s/issues/%s", b.nwo, issueNum)
	out, err := b.runGH(ctx, "api", endpoint, "--jq", ".id")
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(strings.TrimSpace(out), 10, 64)
}

// getParentIssue retrieves the parent issue number if this issue is a sub-issue.
func (b *Backend) getParentIssue(ctx context.Context, issueNum string) (string, error) {
	// Query the issue's parent using the REST API
	endpoint := fmt.Sprintf("repos/%s/issues/%s", b.nwo, issueNum)
	out, err := b.runGH(ctx, "api", endpoint, "--jq", ".parent.number // empty")
	if err != nil {
		return "", nil // No parent or API error, treat as no dependency
	}
	return strings.TrimSpace(out), nil
}

