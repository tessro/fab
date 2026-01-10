package daemon

import (
	"errors"
	"fmt"
)

// Sentinel errors for daemon client operations.
// These can be checked using errors.Is().
var (
	// ErrNotConnected is returned when an operation is attempted without a connection.
	ErrNotConnected = errors.New("daemon: not connected")

	// ErrConnectionFailed is returned when connecting to the daemon fails.
	ErrConnectionFailed = errors.New("daemon: connection failed")

	// ErrRequestTimeout is returned when a request times out.
	ErrRequestTimeout = errors.New("daemon: request timeout")
)

// ServerError represents an error returned by the daemon server.
// This wraps server-side errors with operation context.
type ServerError struct {
	Operation string
	Message   string
}

func (e *ServerError) Error() string {
	return fmt.Sprintf("%s failed: %s", e.Operation, e.Message)
}

// NewServerError creates a new ServerError for the given operation.
func NewServerError(operation, message string) *ServerError {
	return &ServerError{
		Operation: operation,
		Message:   message,
	}
}
