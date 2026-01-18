// Package gh provides a GitHub Issues backend using the GitHub GraphQL API.
package gh

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/tessro/fab/internal/issue"
)

const graphqlEndpoint = "https://api.github.com/graphql"

// Backend implements issue.Backend for GitHub Issues using the GraphQL API.
type Backend struct {
	repoDir        string   // Path to a git repository with a GitHub remote
	nwo            string   // GitHub owner/repo (e.g., "owner/repo")
	allowedAuthors []string // GitHub usernames allowed to create issues (empty = owner only)
	token          string   // GitHub personal access token
	client         *http.Client
}

// New creates a new GitHub issues backend.
// repoDir should be a git repository with a GitHub remote.
// allowedAuthors is a list of GitHub usernames allowed to create issues.
// If empty, defaults to the repository owner inferred from the remote URL.
// configAPIKey is an optional API key from the global config; if empty, falls back to
// GITHUB_TOKEN or GH_TOKEN environment variables.
func New(repoDir string, allowedAuthors []string, configAPIKey string) (*Backend, error) {
	// Extract owner/repo from the git remote
	nwo, err := detectNWO(repoDir)
	if err != nil {
		return nil, fmt.Errorf("detect github repo: %w", err)
	}

	// Get GitHub token from config or environment
	token := configAPIKey
	if token == "" {
		token = os.Getenv("GITHUB_TOKEN")
	}
	if token == "" {
		token = os.Getenv("GH_TOKEN")
	}
	if token == "" {
		return nil, fmt.Errorf("GITHUB_TOKEN or GH_TOKEN not set in config or environment")
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
		token:          token,
		client:         &http.Client{Timeout: 30 * time.Second},
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

// graphqlRequest sends a GraphQL request to the GitHub API.
func (b *Backend) graphqlRequest(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	return b.graphqlRequestWithFeatures(ctx, query, variables, nil)
}

// ghIssue represents a GitHub issue from the GraphQL API.
type ghIssue struct {
	ID        string    `json:"id"` // GraphQL node ID
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	State     string    `json:"state"` // OPEN, CLOSED
	Labels    ghLabels  `json:"labels"`
	Author    ghAuthor  `json:"author"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type ghLabels struct {
	Nodes []ghLabel `json:"nodes"`
}

type ghLabel struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type ghAuthor struct {
	Login string `json:"login"`
}

// ghBlockedBy represents the blockedBy connection on a GitHub issue.
type ghBlockedBy struct {
	Nodes []struct {
		Number int    `json:"number"`
		State  string `json:"state"`
	} `json:"nodes"`
}

// Create creates a new issue on GitHub.
func (b *Backend) Create(ctx context.Context, params issue.CreateParams) (*issue.Issue, error) {
	// First, get the repository ID
	repoID, err := b.getRepositoryID(ctx)
	if err != nil {
		return nil, fmt.Errorf("get repository ID: %w", err)
	}

	// Get or create labels for type and priority
	var labelIDs []string
	issueType := params.Type
	if issueType == "" {
		issueType = "task"
	}
	typeLabel, err := b.findOrCreateLabel(ctx, "type:"+issueType)
	if err == nil && typeLabel != "" {
		labelIDs = append(labelIDs, typeLabel)
	}

	priorityLabel, err := b.findOrCreateLabel(ctx, fmt.Sprintf("priority:%d", params.Priority))
	if err == nil && priorityLabel != "" {
		labelIDs = append(labelIDs, priorityLabel)
	}

	query := `
		mutation CreateIssue($input: CreateIssueInput!) {
			createIssue(input: $input) {
				issue {
					id
					number
					title
					body
					state
					createdAt
					updatedAt
					author { login }
					labels(first: 20) { nodes { id name } }
				}
			}
		}
	`

	input := map[string]any{
		"repositoryId": repoID,
		"title":        params.Title,
		"body":         params.Description,
	}
	if len(labelIDs) > 0 {
		input["labelIds"] = labelIDs
	}

	data, err := b.graphqlRequest(ctx, query, map[string]any{"input": input})
	if err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}

	var result struct {
		CreateIssue struct {
			Issue ghIssue `json:"issue"`
		} `json:"createIssue"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse create response: %w", err)
	}

	// Set up dependencies using GitHub's blockedBy API
	for _, depID := range params.Dependencies {
		if err := b.addBlockedBy(ctx, strconv.Itoa(result.CreateIssue.Issue.Number), depID); err != nil {
			fmt.Fprintf(issue.Stderr, "warning: failed to add dependency %s: %v\n", depID, err)
		}
	}

	return b.toIssue(&result.CreateIssue.Issue), nil
}

// CreateSubIssue creates a child issue under a parent issue.
// Uses GitHub's native sub-issues API (GraphQL with sub_issues feature header).
func (b *Backend) CreateSubIssue(ctx context.Context, parentID string, params issue.CreateParams) (*issue.Issue, error) {
	// First, create the issue normally
	childIssue, err := b.Create(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create child issue: %w", err)
	}

	// Then link it as a sub-issue to the parent using native sub-issues API
	if err := b.addSubIssue(ctx, parentID, childIssue.ID); err != nil {
		// Log warning but don't fail - the issue was created, just not linked
		fmt.Fprintf(issue.Stderr, "warning: failed to link sub-issue to parent %s: %v\n", parentID, err)
	}

	return childIssue, nil
}

// Get retrieves an issue by ID (issue number as string).
func (b *Backend) Get(ctx context.Context, id string) (*issue.Issue, error) {
	num, err := strconv.Atoi(id)
	if err != nil {
		return nil, fmt.Errorf("invalid issue number: %s", id)
	}

	parts := strings.Split(b.nwo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid nwo: %s", b.nwo)
	}
	owner, repo := parts[0], parts[1]

	query := `
		query GetIssue($owner: String!, $repo: String!, $number: Int!) {
			repository(owner: $owner, name: $repo) {
				issue(number: $number) {
					id
					number
					title
					body
					state
					createdAt
					updatedAt
					author { login }
					labels(first: 20) { nodes { id name } }
					blockedBy(first: 20) { nodes { number state } }
				}
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, map[string]any{
		"owner":  owner,
		"repo":   repo,
		"number": num,
	})
	if err != nil {
		return nil, fmt.Errorf("get issue %s: %w", id, err)
	}

	var result struct {
		Repository struct {
			Issue struct {
				ghIssue
				BlockedBy ghBlockedBy `json:"blockedBy"`
			} `json:"issue"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse issue: %w", err)
	}

	iss := b.toIssue(&result.Repository.Issue.ghIssue)

	// Set dependencies from blockedBy issues
	for _, blocker := range result.Repository.Issue.BlockedBy.Nodes {
		iss.Dependencies = append(iss.Dependencies, strconv.Itoa(blocker.Number))
	}

	return iss, nil
}

// List returns issues matching the filter.
func (b *Backend) List(ctx context.Context, filter issue.ListFilter) ([]*issue.Issue, error) {
	parts := strings.Split(b.nwo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid nwo: %s", b.nwo)
	}
	owner, repo := parts[0], parts[1]

	// Build filter states
	var states []string
	if len(filter.Status) > 0 {
		for _, s := range filter.Status {
			switch s {
			case issue.StatusOpen, issue.StatusBlocked:
				states = append(states, "OPEN")
			case issue.StatusClosed:
				states = append(states, "CLOSED")
			}
		}
		// Deduplicate
		stateSet := make(map[string]bool)
		for _, s := range states {
			stateSet[s] = true
		}
		states = nil
		for s := range stateSet {
			states = append(states, s)
		}
	}

	// Note: Label filtering is done client-side after fetching issues
	query := `
		query ListIssues($owner: String!, $repo: String!, $states: [IssueState!], $first: Int!) {
			repository(owner: $owner, name: $repo) {
				issues(states: $states, first: $first, orderBy: {field: UPDATED_AT, direction: DESC}) {
					nodes {
						id
						number
						title
						body
						state
						createdAt
						updatedAt
						author { login }
						labels(first: 20) { nodes { id name } }
						blockedBy(first: 20) { nodes { number state } }
					}
				}
			}
		}
	`

	variables := map[string]any{
		"owner": owner,
		"repo":  repo,
		"first": 100,
	}
	if len(states) > 0 {
		variables["states"] = states
	}

	data, err := b.graphqlRequest(ctx, query, variables)
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}

	var result struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					ghIssue
					BlockedBy ghBlockedBy `json:"blockedBy"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse issues: %w", err)
	}

	issues := make([]*issue.Issue, 0, len(result.Repository.Issues.Nodes))
	for _, gh := range result.Repository.Issues.Nodes {
		iss := b.toIssue(&gh.ghIssue)

		// Set dependencies from blockedBy issues
		for _, blocker := range gh.BlockedBy.Nodes {
			iss.Dependencies = append(iss.Dependencies, strconv.Itoa(blocker.Number))
		}

		// Apply label filter client-side
		if len(filter.Labels) > 0 {
			hasAllLabels := true
			for _, requiredLabel := range filter.Labels {
				found := false
				for _, label := range gh.Labels.Nodes {
					if label.Name == requiredLabel {
						found = true
						break
					}
				}
				if !found {
					hasAllLabels = false
					break
				}
			}
			if !hasAllLabels {
				continue
			}
		}

		issues = append(issues, iss)
	}

	return issues, nil
}

// Update modifies an existing issue.
func (b *Backend) Update(ctx context.Context, id string, params issue.UpdateParams) (*issue.Issue, error) {
	// Get current issue to find its node ID
	current, err := b.getIssueNodeID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("get issue node ID: %w", err)
	}

	// Build update input
	input := map[string]any{
		"id": current.ID,
	}

	if params.Title != nil {
		input["title"] = *params.Title
	}
	if params.Description != nil {
		input["body"] = *params.Description
	}

	// Handle state changes
	if params.Status != nil {
		switch *params.Status {
		case issue.StatusClosed:
			input["state"] = "CLOSED"
		case issue.StatusOpen, issue.StatusBlocked:
			input["state"] = "OPEN"
		}
	}

	// Handle labels
	if params.Labels != nil || params.Type != nil || params.Priority != nil || params.Status != nil {
		// Get current issue to manage labels
		currentIssue, err := b.Get(ctx, id)
		if err != nil {
			return nil, err
		}

		// Start with existing labels or new labels
		var newLabels []string
		if params.Labels != nil {
			newLabels = params.Labels
		} else {
			newLabels = currentIssue.Labels
		}

		// Handle type label
		if params.Type != nil {
			// Remove old type label and add new one
			filtered := make([]string, 0, len(newLabels))
			for _, l := range newLabels {
				if !strings.HasPrefix(l, "type:") {
					filtered = append(filtered, l)
				}
			}
			newLabels = append(filtered, "type:"+*params.Type)
		} else if currentIssue.Type != "" && params.Labels == nil {
			// Preserve existing type label if not explicitly clearing labels
			newLabels = append(newLabels, "type:"+currentIssue.Type)
		}

		// Handle priority label
		if params.Priority != nil {
			// Remove old priority label and add new one
			filtered := make([]string, 0, len(newLabels))
			for _, l := range newLabels {
				if !strings.HasPrefix(l, "priority:") {
					filtered = append(filtered, l)
				}
			}
			newLabels = append(filtered, fmt.Sprintf("priority:%d", *params.Priority))
		} else if params.Labels == nil {
			// Preserve existing priority label if not explicitly clearing labels
			newLabels = append(newLabels, fmt.Sprintf("priority:%d", currentIssue.Priority))
		}

		// Handle blocked status via label
		if params.Status != nil && *params.Status == issue.StatusBlocked {
			// Add blocked label
			hasBlocked := false
			for _, l := range newLabels {
				if l == "blocked" {
					hasBlocked = true
					break
				}
			}
			if !hasBlocked {
				newLabels = append(newLabels, "blocked")
			}
		} else if params.Status != nil && *params.Status != issue.StatusBlocked {
			// Remove blocked label
			filtered := make([]string, 0, len(newLabels))
			for _, l := range newLabels {
				if l != "blocked" {
					filtered = append(filtered, l)
				}
			}
			newLabels = filtered
		}

		// Get or create label IDs
		var labelIDs []string
		for _, labelName := range newLabels {
			labelID, err := b.findOrCreateLabel(ctx, labelName)
			if err != nil {
				fmt.Fprintf(issue.Stderr, "warning: failed to find/create label %s: %v\n", labelName, err)
				continue
			}
			if labelID != "" {
				labelIDs = append(labelIDs, labelID)
			}
		}
		if len(labelIDs) > 0 {
			input["labelIds"] = labelIDs
		}
	}

	query := `
		mutation UpdateIssue($input: UpdateIssueInput!) {
			updateIssue(input: $input) {
				issue {
					id
					number
					title
					body
					state
					createdAt
					updatedAt
					author { login }
					labels(first: 20) { nodes { id name } }
					blockedBy(first: 20) { nodes { number state } }
				}
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, map[string]any{"input": input})
	if err != nil {
		return nil, fmt.Errorf("update issue: %w", err)
	}

	var result struct {
		UpdateIssue struct {
			Issue struct {
				ghIssue
				BlockedBy ghBlockedBy `json:"blockedBy"`
			} `json:"issue"`
		} `json:"updateIssue"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse update response: %w", err)
	}

	iss := b.toIssue(&result.UpdateIssue.Issue.ghIssue)
	for _, blocker := range result.UpdateIssue.Issue.BlockedBy.Nodes {
		iss.Dependencies = append(iss.Dependencies, strconv.Itoa(blocker.Number))
	}

	return iss, nil
}

// Close marks an issue as closed.
func (b *Backend) Close(ctx context.Context, id string) error {
	status := issue.StatusClosed
	_, err := b.Update(ctx, id, issue.UpdateParams{Status: &status})
	return err
}

// Ready returns open issues that are not blocked, have no open dependencies, and are authored by allowed users.
func (b *Backend) Ready(ctx context.Context) ([]*issue.Issue, error) {
	parts := strings.Split(b.nwo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid nwo: %s", b.nwo)
	}
	owner, repo := parts[0], parts[1]

	// Build allowed authors set for quick lookup
	allowedSet := make(map[string]bool, len(b.allowedAuthors))
	for _, author := range b.allowedAuthors {
		allowedSet[strings.ToLower(author)] = true
	}

	// Fetch open issues with blockedBy and author info
	query := `
		query ListIssuesForReady($owner: String!, $repo: String!) {
			repository(owner: $owner, name: $repo) {
				issues(states: [OPEN], first: 100, orderBy: {field: UPDATED_AT, direction: DESC}) {
					nodes {
						id
						number
						title
						body
						state
						createdAt
						updatedAt
						author { login }
						labels(first: 20) { nodes { id name } }
						blockedBy(first: 20) { nodes { number state } }
					}
				}
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, map[string]any{
		"owner": owner,
		"repo":  repo,
	})
	if err != nil {
		return nil, fmt.Errorf("list issues for ready: %w", err)
	}

	var result struct {
		Repository struct {
			Issues struct {
				Nodes []struct {
					ghIssue
					BlockedBy ghBlockedBy `json:"blockedBy"`
				} `json:"nodes"`
			} `json:"issues"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse issues: %w", err)
	}

	// Filter: not blocked + authored by allowed user + no open blocking issues
	ready := make([]*issue.Issue, 0)
	for _, gh := range result.Repository.Issues.Nodes {
		iss := b.toIssue(&gh.ghIssue)

		// Skip blocked issues (via label)
		if iss.Status == issue.StatusBlocked {
			continue
		}

		// Skip issues not authored by allowed users
		if len(allowedSet) > 0 && !allowedSet[strings.ToLower(gh.Author.Login)] {
			continue
		}

		// Skip issues with open blocking issues
		hasOpenBlocker := false
		for _, blocker := range gh.BlockedBy.Nodes {
			if blocker.State == "OPEN" {
				hasOpenBlocker = true
				break
			}
		}
		if hasOpenBlocker {
			continue
		}

		ready = append(ready, iss)
	}

	return ready, nil
}

// Commit is a no-op for GitHub issues since changes are immediate.
func (b *Backend) Commit(ctx context.Context) error {
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

	// Extract description
	iss.Description = gh.Body

	// Determine status
	if gh.State == "CLOSED" {
		iss.Status = issue.StatusClosed
	} else {
		iss.Status = issue.StatusOpen
	}

	// Parse labels for type, priority, and blocked status
	for _, label := range gh.Labels.Nodes {
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

// getRepositoryID retrieves the GraphQL node ID for the repository.
func (b *Backend) getRepositoryID(ctx context.Context) (string, error) {
	parts := strings.Split(b.nwo, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid nwo: %s", b.nwo)
	}
	owner, repo := parts[0], parts[1]

	query := `
		query GetRepositoryID($owner: String!, $repo: String!) {
			repository(owner: $owner, name: $repo) {
				id
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, map[string]any{
		"owner": owner,
		"repo":  repo,
	})
	if err != nil {
		return "", err
	}

	var result struct {
		Repository struct {
			ID string `json:"id"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}

	return result.Repository.ID, nil
}

// getIssueNodeID retrieves the GraphQL node ID for an issue.
func (b *Backend) getIssueNodeID(ctx context.Context, id string) (*ghIssue, error) {
	num, err := strconv.Atoi(id)
	if err != nil {
		return nil, fmt.Errorf("invalid issue number: %s", id)
	}

	parts := strings.Split(b.nwo, "/")
	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid nwo: %s", b.nwo)
	}
	owner, repo := parts[0], parts[1]

	query := `
		query GetIssueNodeID($owner: String!, $repo: String!, $number: Int!) {
			repository(owner: $owner, name: $repo) {
				issue(number: $number) {
					id
					number
				}
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, map[string]any{
		"owner":  owner,
		"repo":   repo,
		"number": num,
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Repository struct {
			Issue ghIssue `json:"issue"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}

	return &result.Repository.Issue, nil
}

// findOrCreateLabel finds or creates a label with the given name.
func (b *Backend) findOrCreateLabel(ctx context.Context, name string) (string, error) {
	parts := strings.Split(b.nwo, "/")
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid nwo: %s", b.nwo)
	}
	owner, repo := parts[0], parts[1]

	// First, try to find the existing label
	query := `
		query GetLabel($owner: String!, $repo: String!, $name: String!) {
			repository(owner: $owner, name: $repo) {
				label(name: $name) {
					id
				}
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, map[string]any{
		"owner": owner,
		"repo":  repo,
		"name":  name,
	})
	if err != nil {
		// Label might not exist, try to create it
		return b.createLabel(ctx, name)
	}

	var result struct {
		Repository struct {
			Label *struct {
				ID string `json:"id"`
			} `json:"label"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}

	if result.Repository.Label != nil {
		return result.Repository.Label.ID, nil
	}

	// Label not found, create it
	return b.createLabel(ctx, name)
}

// createLabel creates a new label in the repository.
func (b *Backend) createLabel(ctx context.Context, name string) (string, error) {
	repoID, err := b.getRepositoryID(ctx)
	if err != nil {
		return "", err
	}

	// Choose color based on label type
	color := "ededed" // default gray
	switch {
	case strings.HasPrefix(name, "type:"):
		color = "0366d6" // blue
	case strings.HasPrefix(name, "priority:"):
		color = "fbca04" // yellow
	case name == "blocked":
		color = "d73a4a" // red
	}

	query := `
		mutation CreateLabel($input: CreateLabelInput!) {
			createLabel(input: $input) {
				label {
					id
				}
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, map[string]any{
		"input": map[string]any{
			"repositoryId": repoID,
			"name":         name,
			"color":        color,
		},
	})
	if err != nil {
		// If label already exists (race condition), try to fetch it again
		if strings.Contains(err.Error(), "already exists") {
			return b.findOrCreateLabel(ctx, name)
		}
		return "", err
	}

	var result struct {
		CreateLabel struct {
			Label struct {
				ID string `json:"id"`
			} `json:"label"`
		} `json:"createLabel"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}

	return result.CreateLabel.Label.ID, nil
}

// addBlockedBy adds a blockedBy relationship: blockedIssueNum is blocked by blockingIssueNum.
func (b *Backend) addBlockedBy(ctx context.Context, blockedIssueNum, blockingIssueNum string) error {
	// Get the GraphQL node IDs for both issues
	blockedIssue, err := b.getIssueNodeID(ctx, blockedIssueNum)
	if err != nil {
		return fmt.Errorf("get blocked issue node ID: %w", err)
	}

	blockingIssue, err := b.getIssueNodeID(ctx, blockingIssueNum)
	if err != nil {
		return fmt.Errorf("get blocking issue node ID: %w", err)
	}

	query := `
		mutation AddBlockedBy($input: AddBlockedByInput!) {
			addBlockedBy(input: $input) {
				issue { number }
				blockingIssue { number }
			}
		}
	`

	_, err = b.graphqlRequest(ctx, query, map[string]any{
		"input": map[string]any{
			"issueId":         blockedIssue.ID,
			"blockingIssueId": blockingIssue.ID,
		},
	})
	if err != nil {
		return fmt.Errorf("add blockedBy relationship: %w", err)
	}

	return nil
}

// addSubIssue links an existing issue as a sub-issue of a parent issue.
// Uses GitHub's sub-issues API with the GraphQL-Features: sub_issues header.
func (b *Backend) addSubIssue(ctx context.Context, parentIssueNum, childIssueNum string) error {
	// Get the GraphQL node IDs for both issues
	parentIssue, err := b.getIssueNodeID(ctx, parentIssueNum)
	if err != nil {
		return fmt.Errorf("get parent issue node ID: %w", err)
	}

	childIssue, err := b.getIssueNodeID(ctx, childIssueNum)
	if err != nil {
		return fmt.Errorf("get child issue node ID: %w", err)
	}

	query := `
		mutation AddSubIssue($input: AddSubIssueInput!) {
			addSubIssue(input: $input) {
				issue { number }
				subIssue { number }
			}
		}
	`

	_, err = b.graphqlRequestWithFeatures(ctx, query, map[string]any{
		"input": map[string]any{
			"issueId":    parentIssue.ID,
			"subIssueId": childIssue.ID,
		},
	}, []string{"sub_issues"})
	if err != nil {
		return fmt.Errorf("add sub-issue relationship: %w", err)
	}

	return nil
}

// graphqlRequestWithFeatures sends a GraphQL request with additional feature headers.
func (b *Backend) graphqlRequestWithFeatures(ctx context.Context, query string, variables map[string]any, features []string) (json.RawMessage, error) {
	body := map[string]any{
		"query": query,
	}
	if variables != nil {
		body["variables"] = variables
	}

	jsonBody, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", graphqlEndpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+b.token)
	if len(features) > 0 {
		req.Header.Set("GraphQL-Features", strings.Join(features, ","))
	}

	resp, err := b.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	var result struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}

	if len(result.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL error: %s", result.Errors[0].Message)
	}

	return result.Data, nil
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

// AddComment adds a comment to an issue.
func (b *Backend) AddComment(ctx context.Context, id string, body string) error {
	return issue.ErrNotSupported
}

// UpsertPlanSection updates or creates a ## Plan section in the issue body.
func (b *Backend) UpsertPlanSection(ctx context.Context, id string, planContent string) error {
	return issue.ErrNotSupported
}

