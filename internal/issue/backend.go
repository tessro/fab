package issue

import "context"

// IssueReader provides read-only access to issues.
type IssueReader interface {
	// Name returns the backend identifier (e.g., "tk", "linear").
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

	// Update modifies an existing issue.
	Update(ctx context.Context, id string, params UpdateParams) (*Issue, error)

	// Close marks an issue as closed.
	Close(ctx context.Context, id string) error

	// Commit stages, commits, and pushes any pending issue changes.
	Commit(ctx context.Context) error
}

// Backend is the interface for issue tracking backends.
// It combines IssueReader and IssueWriter for full access.
type Backend interface {
	IssueReader
	IssueWriter
}
