package tk

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/tessro/fab/internal/issue"
)

// Backend implements issue.Backend for tk file-based issues.
type Backend struct {
	repoDir    string // Path to the git repository
	ticketsDir string // Path to .tickets directory
	prefix     string // Issue ID prefix (e.g., "fa-")
}

// New creates a new tk backend for the given repository.
func New(repoDir string) (*Backend, error) {
	ticketsDir := filepath.Join(repoDir, ".tickets")

	// Detect prefix from existing issues
	prefix, err := detectPrefix(ticketsDir)
	if err != nil {
		return nil, err
	}

	return &Backend{
		repoDir:    repoDir,
		ticketsDir: ticketsDir,
		prefix:     prefix,
	}, nil
}

// Name returns the backend identifier.
func (b *Backend) Name() string {
	return "tk"
}

// Create creates a new issue.
func (b *Backend) Create(ctx context.Context, params issue.CreateParams) (*issue.Issue, error) {
	id := b.generateID()

	iss := &issue.Issue{
		ID:           id,
		Title:        params.Title,
		Description:  params.Description,
		Status:       issue.StatusOpen,
		Priority:     params.Priority,
		Type:         params.Type,
		Dependencies: params.Dependencies,
		Labels:       params.Labels,
		Created:      time.Now(),
	}

	// Default type to task
	if iss.Type == "" {
		iss.Type = "task"
	}

	// Write file
	if err := b.writeIssue(iss); err != nil {
		return nil, err
	}

	return iss, nil
}

// Get retrieves an issue by ID.
func (b *Backend) Get(ctx context.Context, id string) (*issue.Issue, error) {
	path := b.issuePath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("issue not found: %s", id)
		}
		return nil, err
	}

	return parseIssue(data)
}

// List returns issues matching the filter.
func (b *Backend) List(ctx context.Context, filter issue.ListFilter) ([]*issue.Issue, error) {
	entries, err := os.ReadDir(b.ticketsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No tickets directory yet
		}
		return nil, err
	}

	var issues []*issue.Issue
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		path := filepath.Join(b.ticketsDir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue // Skip unreadable files
		}

		iss, err := parseIssue(data)
		if err != nil {
			continue // Skip malformed files
		}

		// Apply status filter
		if len(filter.Status) > 0 {
			match := false
			for _, s := range filter.Status {
				if iss.Status == s {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		// Apply label filter (must have ALL labels)
		if len(filter.Labels) > 0 {
			hasAll := true
			for _, label := range filter.Labels {
				found := false
				for _, issLabel := range iss.Labels {
					if issLabel == label {
						found = true
						break
					}
				}
				if !found {
					hasAll = false
					break
				}
			}
			if !hasAll {
				continue
			}
		}

		issues = append(issues, iss)
	}

	return issues, nil
}

// Update modifies an existing issue.
func (b *Backend) Update(ctx context.Context, id string, params issue.UpdateParams) (*issue.Issue, error) {
	iss, err := b.Get(ctx, id)
	if err != nil {
		return nil, err
	}

	// Apply updates
	if params.Title != nil {
		iss.Title = *params.Title
	}
	if params.Description != nil {
		iss.Description = *params.Description
	}
	if params.Status != nil {
		iss.Status = *params.Status
	}
	if params.Priority != nil {
		iss.Priority = *params.Priority
	}
	if params.Type != nil {
		iss.Type = *params.Type
	}
	if params.Labels != nil {
		iss.Labels = params.Labels
	}
	if params.Dependencies != nil {
		iss.Dependencies = params.Dependencies
	}

	iss.Updated = time.Now()

	// Write file
	if err := b.writeIssue(iss); err != nil {
		return nil, err
	}

	return iss, nil
}

// Close marks an issue as closed.
func (b *Backend) Close(ctx context.Context, id string) error {
	status := issue.StatusClosed
	_, err := b.Update(ctx, id, issue.UpdateParams{Status: &status})
	return err
}

// Commit stages, commits, and pushes any pending issue changes.
func (b *Backend) Commit(ctx context.Context) error {
	return b.commitAndPush("issue: update tickets")
}

// Ready returns issues with no open dependencies.
func (b *Backend) Ready(ctx context.Context) ([]*issue.Issue, error) {
	// Get all open issues
	allIssues, err := b.List(ctx, issue.ListFilter{
		Status: []issue.Status{issue.StatusOpen},
	})
	if err != nil {
		return nil, err
	}

	// Build map of open issue IDs for dependency checking
	openIDs := make(map[string]bool)
	for _, iss := range allIssues {
		openIDs[iss.ID] = true
	}

	// Filter to issues with no open dependencies
	var ready []*issue.Issue
	for _, iss := range allIssues {
		hasOpenDep := false
		for _, depID := range iss.Dependencies {
			if openIDs[depID] {
				hasOpenDep = true
				break
			}
		}
		if !hasOpenDep {
			ready = append(ready, iss)
		}
	}

	return ready, nil
}

// issuePath returns the file path for an issue.
func (b *Backend) issuePath(id string) string {
	return filepath.Join(b.ticketsDir, id+".md")
}

// writeIssue writes an issue to disk.
func (b *Backend) writeIssue(iss *issue.Issue) error {
	// Ensure tickets directory exists
	if err := os.MkdirAll(b.ticketsDir, 0755); err != nil {
		return err
	}

	data, err := formatIssue(iss)
	if err != nil {
		return err
	}

	return os.WriteFile(b.issuePath(iss.ID), data, 0644)
}

// generateID creates a new unique issue ID.
func (b *Backend) generateID() string {
	// Generate 3 random bytes (6 hex chars)
	buf := make([]byte, 3)
	_, _ = rand.Read(buf)
	suffix := hex.EncodeToString(buf)[:3]
	return b.prefix + suffix
}

// detectPrefix detects the issue ID prefix from existing issues.
func detectPrefix(ticketsDir string) (string, error) {
	entries, err := os.ReadDir(ticketsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No tickets yet, use default prefix based on directory name
			return "issue-", nil
		}
		return "", err
	}

	// Look at first .md file to detect prefix
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		// Find the last dash followed by alphanumeric suffix
		for i := len(name) - 1; i >= 0; i-- {
			if name[i] == '-' {
				return name[:i+1], nil
			}
		}
	}

	return "issue-", nil
}
