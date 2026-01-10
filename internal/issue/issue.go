// Package issue provides a pluggable backend interface for issue tracking.
package issue

import (
	"io"
	"os"
	"time"
)

// Stderr is the writer for warning messages. Defaults to os.Stderr.
var Stderr io.Writer = os.Stderr

// Status represents the state of an issue.
type Status string

const (
	StatusOpen    Status = "open"
	StatusClosed  Status = "closed"
	StatusBlocked Status = "blocked"
)

// Issue represents a task/issue across backends.
type Issue struct {
	ID           string
	Title        string
	Description  string
	Status       Status
	Priority     int      // 0 = low, 1 = medium, 2 = high
	Type         string   // task, bug, feature, chore
	Dependencies []string // IDs of blocking issues
	Labels       []string
	Links        []string
	Created      time.Time
	Updated      time.Time
}

// CreateParams are options for creating an issue.
type CreateParams struct {
	Title        string
	Description  string
	Type         string
	Priority     int
	Labels       []string
	Dependencies []string
}

// UpdateParams are options for updating an issue.
// Nil pointers mean "no change".
type UpdateParams struct {
	Title        *string
	Description  *string
	Status       *Status
	Priority     *int
	Type         *string
	Labels       []string // nil = no change, empty = clear
	Dependencies []string // nil = no change, empty = clear
}

// ListFilter defines filtering for list operations.
type ListFilter struct {
	Status []Status // Match any of these statuses (empty = all)
	Labels []string // Match issues with ALL these labels
}

// NewBackendFunc creates an issue backend given a repo directory.
type NewBackendFunc func(repoDir string) (Backend, error)
