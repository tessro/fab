package issue

import (
	"context"
	"errors"
	"time"
)

// ErrNotSupported is returned when a backend does not support an operation.
var ErrNotSupported = errors.New("operation not supported by this backend")

// IssueReader provides read-only access to issues.
type IssueReader interface {
	// Name returns the backend identifier (e.g., "tk", "github").
	Name() string

	// Get retrieves an issue by ID.
	Get(ctx context.Context, id string) (*Issue, error)

	// List returns issues matching the filter.
	List(ctx context.Context, filter ListFilter) ([]*Issue, error)

	// Ready returns issues with no open dependencies (ready to work on).
	Ready(ctx context.Context) ([]*Issue, error)
}

// IssueWriter provides write access to issues.
type IssueWriter interface {
	// Create creates a new issue and returns it with its assigned ID.
	Create(ctx context.Context, params CreateParams) (*Issue, error)

	// CreateSubIssue creates a child issue under a parent issue.
	// Returns the child issue with its assigned ID.
	CreateSubIssue(ctx context.Context, parentID string, params CreateParams) (*Issue, error)

	// Update modifies an existing issue.
	Update(ctx context.Context, id string, params UpdateParams) (*Issue, error)

	// Close marks an issue as closed.
	Close(ctx context.Context, id string) error

	// Commit stages, commits, and pushes any pending issue changes.
	Commit(ctx context.Context) error
}

// IssueCollaborator provides methods for issue collaboration features.
// Backends may return ErrNotSupported for operations they cannot perform.
type IssueCollaborator interface {
	// AddComment adds a comment to an issue.
	// Returns ErrNotSupported if the backend does not support comments.
	AddComment(ctx context.Context, id string, body string) error

	// ListComments returns comments for an issue, ordered by creation time (oldest first).
	// The since parameter filters to comments created after the given time.
	// Returns ErrNotSupported if the backend does not support listing comments.
	ListComments(ctx context.Context, id string, since time.Time) ([]*Comment, error)

	// UpsertPlanSection updates or creates a ## Plan section in the issue body.
	// The plan content should be bullet points describing the implementation plan.
	// This operation is idempotent - calling it multiple times with the same content
	// will not create duplicate sections.
	// Returns ErrNotSupported if the backend does not support plan sections.
	UpsertPlanSection(ctx context.Context, id string, planContent string) error

	// CreateSubIssue creates a child issue linked to a parent issue.
	// The child issue will have the parent as a dependency.
	// Returns ErrNotSupported if the backend does not support sub-issues.
	CreateSubIssue(ctx context.Context, parentID string, params CreateParams) (*Issue, error)
}

// Backend is the interface for issue tracking backends.
// It combines IssueReader and IssueWriter for full access.
type Backend interface {
	IssueReader
	IssueWriter
}

// CollaborativeBackend extends Backend with collaboration features.
// Backends that support comments, plans, and sub-issues should implement this.
type CollaborativeBackend interface {
	Backend
	IssueCollaborator
}
