// Package linear provides a Linear Issues backend using the Linear GraphQL API.
package linear

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/tessro/fab/internal/issue"
)

const (
	apiEndpoint = "https://api.linear.app/graphql"
)

// Backend implements issue.Backend for Linear Issues using the GraphQL API.
type Backend struct {
	projectID      string   // Linear project ID to scope issues to
	allowedAuthors []string // Linear usernames allowed to create issues (empty = all)
	apiKey         string   // Linear API key
	client         *http.Client
}

// New creates a new Linear issues backend.
// repoDir is used for context but Linear doesn't require it.
// configAPIKey is the API key from the global config (can be empty).
// The backend falls back to LINEAR_API_KEY environment variable if configAPIKey is empty.
// projectID should be set via project config (linear-project setting).
func New(repoDir string, projectID string, allowedAuthors []string, configAPIKey string) (*Backend, error) {
	apiKey := configAPIKey
	if apiKey == "" {
		apiKey = os.Getenv("LINEAR_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("LINEAR_API_KEY not set in config or environment")
	}

	if projectID == "" {
		return nil, fmt.Errorf("linear-project setting not configured for this project")
	}

	return &Backend{
		projectID:      projectID,
		allowedAuthors: allowedAuthors,
		apiKey:         apiKey,
		client:         &http.Client{Timeout: 30 * time.Second},
	}, nil
}

// Name returns the backend identifier.
func (b *Backend) Name() string {
	return "linear"
}

// graphqlRequest sends a GraphQL request to the Linear API.
func (b *Backend) graphqlRequest(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
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

	req, err := http.NewRequestWithContext(ctx, "POST", apiEndpoint, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", b.apiKey)

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

// linearIssue represents a Linear issue from the GraphQL API.
type linearIssue struct {
	ID          string    `json:"id"`
	Identifier  string    `json:"identifier"` // e.g., "FAB-123"
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Priority    int       `json:"priority"` // 0=none, 1=urgent, 2=high, 3=medium, 4=low
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	State       struct {
		ID   string `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"` // backlog, unstarted, started, completed, canceled
	} `json:"state"`
	Labels struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"labels"`
	Parent *struct {
		Identifier string `json:"identifier"`
	} `json:"parent"`
	Creator *struct {
		Name string `json:"name"`
	} `json:"creator"`
}

// Create creates a new issue on Linear.
func (b *Backend) Create(ctx context.Context, params issue.CreateParams) (*issue.Issue, error) {
	// Map priority: fab uses 0=low,1=medium,2=high; Linear uses 1=urgent,2=high,3=medium,4=low,0=none
	linearPriority := mapPriorityToLinear(params.Priority)

	// Build label IDs from names (we need to look them up first or create them)
	var labelIDs []string
	if params.Type != "" {
		// Add type as a label
		labelID, err := b.findOrCreateLabel(ctx, "type:"+params.Type)
		if err != nil {
			fmt.Fprintf(issue.Stderr, "warning: failed to create type label: %v\n", err)
		} else if labelID != "" {
			labelIDs = append(labelIDs, labelID)
		}
	}

	query := `
		mutation IssueCreate($input: IssueCreateInput!) {
			issueCreate(input: $input) {
				success
				issue {
					id
					identifier
					title
					description
					priority
					createdAt
					updatedAt
					state { id name type }
					labels { nodes { name } }
					parent { identifier }
					creator { name }
				}
			}
		}
	`

	input := map[string]any{
		"title":     params.Title,
		"projectId": b.projectID,
		"priority":  linearPriority,
	}
	if params.Description != "" {
		input["description"] = params.Description
	}
	if len(labelIDs) > 0 {
		input["labelIds"] = labelIDs
	}

	// If there are dependencies, set the parent (Linear uses parent-child for sub-issues)
	if len(params.Dependencies) > 0 {
		// Use first dependency as parent
		parentID, err := b.resolveIssueID(ctx, params.Dependencies[0])
		if err != nil {
			fmt.Fprintf(issue.Stderr, "warning: failed to resolve parent issue: %v\n", err)
		} else {
			input["parentId"] = parentID
		}
	}

	data, err := b.graphqlRequest(ctx, query, map[string]any{"input": input})
	if err != nil {
		return nil, fmt.Errorf("create issue: %w", err)
	}

	var result struct {
		IssueCreate struct {
			Success bool        `json:"success"`
			Issue   linearIssue `json:"issue"`
		} `json:"issueCreate"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse create response: %w", err)
	}

	if !result.IssueCreate.Success {
		return nil, fmt.Errorf("issue creation failed")
	}

	return b.toIssue(&result.IssueCreate.Issue), nil
}

// Get retrieves an issue by ID (identifier like "FAB-123" or UUID).
func (b *Backend) Get(ctx context.Context, id string) (*issue.Issue, error) {
	// Try to get by identifier first (more common use case)
	query := `
		query Issue($id: String!) {
			issue(id: $id) {
				id
				identifier
				title
				description
				priority
				createdAt
				updatedAt
				state { id name type }
				labels { nodes { name } }
				parent { identifier }
				creator { name }
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, map[string]any{"id": id})
	if err != nil {
		return nil, fmt.Errorf("get issue %s: %w", id, err)
	}

	var result struct {
		Issue linearIssue `json:"issue"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse issue: %w", err)
	}

	return b.toIssue(&result.Issue), nil
}

// List returns issues matching the filter.
func (b *Backend) List(ctx context.Context, filter issue.ListFilter) ([]*issue.Issue, error) {
	// Build filter for the project
	filterObj := map[string]any{
		"project": map[string]any{"id": map[string]any{"eq": b.projectID}},
	}

	// Apply status filter
	if len(filter.Status) > 0 {
		stateTypes := make([]string, 0)
		for _, s := range filter.Status {
			switch s {
			case issue.StatusOpen:
				stateTypes = append(stateTypes, "backlog", "unstarted", "started")
			case issue.StatusClosed:
				stateTypes = append(stateTypes, "completed", "canceled")
			case issue.StatusBlocked:
				// Linear doesn't have a direct "blocked" state, we use a label
				stateTypes = append(stateTypes, "backlog", "unstarted", "started")
			}
		}
		if len(stateTypes) > 0 {
			filterObj["state"] = map[string]any{"type": map[string]any{"in": stateTypes}}
		}
	}

	query := `
		query Issues($filter: IssueFilter) {
			issues(filter: $filter, first: 100) {
				nodes {
					id
					identifier
					title
					description
					priority
					createdAt
					updatedAt
					state { id name type }
					labels { nodes { name } }
					parent { identifier }
					creator { name }
				}
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, map[string]any{"filter": filterObj})
	if err != nil {
		return nil, fmt.Errorf("list issues: %w", err)
	}

	var result struct {
		Issues struct {
			Nodes []linearIssue `json:"nodes"`
		} `json:"issues"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse issues: %w", err)
	}

	issues := make([]*issue.Issue, 0, len(result.Issues.Nodes))
	for _, li := range result.Issues.Nodes {
		iss := b.toIssue(&li)

		// Apply label filter (Linear doesn't support label filtering in the API well)
		if len(filter.Labels) > 0 {
			hasAllLabels := true
			for _, requiredLabel := range filter.Labels {
				found := false
				for _, label := range iss.Labels {
					if label == requiredLabel {
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
	// Resolve the issue ID (convert identifier to UUID if needed)
	issueID, err := b.resolveIssueID(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("resolve issue ID: %w", err)
	}

	query := `
		mutation IssueUpdate($id: String!, $input: IssueUpdateInput!) {
			issueUpdate(id: $id, input: $input) {
				success
				issue {
					id
					identifier
					title
					description
					priority
					createdAt
					updatedAt
					state { id name type }
					labels { nodes { name } }
					parent { identifier }
					creator { name }
				}
			}
		}
	`

	input := make(map[string]any)

	if params.Title != nil {
		input["title"] = *params.Title
	}
	if params.Description != nil {
		input["description"] = *params.Description
	}
	if params.Priority != nil {
		input["priority"] = mapPriorityToLinear(*params.Priority)
	}
	if params.Status != nil {
		// Find appropriate state ID for the status
		stateID, err := b.findStateForStatus(ctx, *params.Status)
		if err != nil {
			return nil, fmt.Errorf("find state for status: %w", err)
		}
		input["stateId"] = stateID
	}

	// Handle labels
	if params.Labels != nil {
		labelIDs := make([]string, 0, len(params.Labels))
		for _, labelName := range params.Labels {
			labelID, err := b.findOrCreateLabel(ctx, labelName)
			if err != nil {
				fmt.Fprintf(issue.Stderr, "warning: failed to find/create label %s: %v\n", labelName, err)
				continue
			}
			if labelID != "" {
				labelIDs = append(labelIDs, labelID)
			}
		}
		input["labelIds"] = labelIDs
	}

	// Handle dependencies (parent)
	if params.Dependencies != nil {
		if len(params.Dependencies) > 0 {
			parentID, err := b.resolveIssueID(ctx, params.Dependencies[0])
			if err != nil {
				fmt.Fprintf(issue.Stderr, "warning: failed to resolve parent: %v\n", err)
			} else {
				input["parentId"] = parentID
			}
		} else {
			// Clear parent
			input["parentId"] = nil
		}
	}

	if len(input) == 0 {
		// No changes, just return current state
		return b.Get(ctx, id)
	}

	data, err := b.graphqlRequest(ctx, query, map[string]any{"id": issueID, "input": input})
	if err != nil {
		return nil, fmt.Errorf("update issue: %w", err)
	}

	var result struct {
		IssueUpdate struct {
			Success bool        `json:"success"`
			Issue   linearIssue `json:"issue"`
		} `json:"issueUpdate"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("parse update response: %w", err)
	}

	if !result.IssueUpdate.Success {
		return nil, fmt.Errorf("issue update failed")
	}

	return b.toIssue(&result.IssueUpdate.Issue), nil
}

// Close marks an issue as closed (completed).
func (b *Backend) Close(ctx context.Context, id string) error {
	status := issue.StatusClosed
	_, err := b.Update(ctx, id, issue.UpdateParams{Status: &status})
	return err
}

// Ready returns open issues that are not blocked and have no open dependencies.
// Note: Unlike the GitHub backend, author filtering is not implemented because
// Linear's user model uses different identifiers than simple usernames.
func (b *Backend) Ready(ctx context.Context) ([]*issue.Issue, error) {
	// Get all open issues for the project
	issues, err := b.List(ctx, issue.ListFilter{Status: []issue.Status{issue.StatusOpen}})
	if err != nil {
		return nil, err
	}

	// Build a set of open issue identifiers for dependency checking
	openIssues := make(map[string]bool, len(issues))
	for _, iss := range issues {
		openIssues[iss.ID] = true
	}

	// Filter: not blocked + no open dependencies
	ready := make([]*issue.Issue, 0)
	for _, iss := range issues {
		// Skip blocked issues
		if iss.Status == issue.StatusBlocked {
			continue
		}

		// Skip issues with open dependencies (parent)
		hasOpenDep := false
		for _, dep := range iss.Dependencies {
			if openIssues[dep] {
				hasOpenDep = true
				break
			}
		}
		if hasOpenDep {
			continue
		}

		ready = append(ready, iss)
	}

	return ready, nil
}

// Commit is a no-op for Linear issues since changes are immediate via API.
func (b *Backend) Commit(ctx context.Context) error {
	return nil
}

// toIssue converts a Linear issue to our Issue type.
func (b *Backend) toIssue(li *linearIssue) *issue.Issue {
	iss := &issue.Issue{
		ID:          li.Identifier, // Use the human-readable identifier (e.g., "FAB-123")
		Title:       li.Title,
		Description: li.Description,
		Created:     li.CreatedAt,
		Updated:     li.UpdatedAt,
		Priority:    mapPriorityFromLinear(li.Priority),
	}

	// Determine status from workflow state type
	switch li.State.Type {
	case "completed", "canceled":
		iss.Status = issue.StatusClosed
	default:
		iss.Status = issue.StatusOpen
	}

	// Extract labels and check for blocked status
	for _, label := range li.Labels.Nodes {
		name := label.Name
		switch {
		case strings.HasPrefix(name, "type:"):
			iss.Type = strings.TrimPrefix(name, "type:")
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

	// Set parent as dependency
	if li.Parent != nil {
		iss.Dependencies = []string{li.Parent.Identifier}
	}

	return iss
}

// mapPriorityToLinear converts fab priority (0=low,1=medium,2=high) to Linear priority (0=none,1=urgent,2=high,3=medium,4=low).
func mapPriorityToLinear(priority int) int {
	switch priority {
	case 0:
		return 4 // low
	case 1:
		return 3 // medium
	case 2:
		return 2 // high
	default:
		return 0 // none
	}
}

// mapPriorityFromLinear converts Linear priority to fab priority.
func mapPriorityFromLinear(priority int) int {
	switch priority {
	case 1, 2: // urgent, high
		return 2 // high
	case 3: // medium
		return 1 // medium
	case 4: // low
		return 0 // low
	default:
		return 1 // default to medium
	}
}

// resolveIssueID converts an identifier (e.g., "FAB-123") to a UUID if needed.
func (b *Backend) resolveIssueID(ctx context.Context, id string) (string, error) {
	// If it looks like a UUID, return as-is
	if len(id) == 36 && strings.Count(id, "-") == 4 {
		return id, nil
	}

	// Otherwise, fetch the issue to get its UUID
	query := `
		query Issue($id: String!) {
			issue(id: $id) {
				id
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, map[string]any{"id": id})
	if err != nil {
		return "", fmt.Errorf("resolve issue ID: %w", err)
	}

	var result struct {
		Issue struct {
			ID string `json:"id"`
		} `json:"issue"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse issue: %w", err)
	}

	return result.Issue.ID, nil
}

// findStateForStatus finds a workflow state ID that matches the desired status.
func (b *Backend) findStateForStatus(ctx context.Context, status issue.Status) (string, error) {
	// Query the team's workflow states
	// Since we're working with a project, we need to find states from the teams
	query := `
		query WorkflowStates {
			workflowStates(first: 50) {
				nodes {
					id
					name
					type
				}
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, nil)
	if err != nil {
		return "", fmt.Errorf("query workflow states: %w", err)
	}

	var result struct {
		WorkflowStates struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"nodes"`
		} `json:"workflowStates"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse workflow states: %w", err)
	}

	// Find appropriate state type based on status
	var targetTypes []string
	switch status {
	case issue.StatusOpen:
		targetTypes = []string{"unstarted", "started", "backlog"}
	case issue.StatusClosed:
		targetTypes = []string{"completed", "canceled"}
	case issue.StatusBlocked:
		// For blocked, we'll use an open state and add a "blocked" label
		targetTypes = []string{"unstarted", "backlog"}
	}

	for _, targetType := range targetTypes {
		for _, state := range result.WorkflowStates.Nodes {
			if state.Type == targetType {
				return state.ID, nil
			}
		}
	}

	return "", fmt.Errorf("no suitable workflow state found for status %s", status)
}

// findOrCreateLabel finds or creates a label with the given name.
func (b *Backend) findOrCreateLabel(ctx context.Context, name string) (string, error) {
	// First, try to find existing label
	query := `
		query Labels($filter: IssueLabelFilter) {
			issueLabels(filter: $filter, first: 1) {
				nodes {
					id
					name
				}
			}
		}
	`

	data, err := b.graphqlRequest(ctx, query, map[string]any{
		"filter": map[string]any{
			"name": map[string]any{"eq": name},
		},
	})
	if err != nil {
		return "", fmt.Errorf("query labels: %w", err)
	}

	var result struct {
		IssueLabels struct {
			Nodes []struct {
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"nodes"`
		} `json:"issueLabels"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("parse labels: %w", err)
	}

	if len(result.IssueLabels.Nodes) > 0 {
		return result.IssueLabels.Nodes[0].ID, nil
	}

	// Label not found - we can't create labels without a team ID,
	// and Linear's label system is team-scoped, so we'll skip creating
	// and just return empty (the issue will be created without this label)
	return "", nil
}
