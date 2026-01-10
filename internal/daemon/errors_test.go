package daemon

import (
	"errors"
	"testing"
)

func TestErrNotConnected(t *testing.T) {
	err := ErrNotConnected
	if err.Error() != "daemon: not connected" {
		t.Errorf("unexpected error message: %s", err.Error())
	}

	// Test errors.Is works
	if !errors.Is(err, ErrNotConnected) {
		t.Error("errors.Is should match ErrNotConnected")
	}
}

func TestErrConnectionFailed(t *testing.T) {
	err := ErrConnectionFailed
	if err.Error() != "daemon: connection failed" {
		t.Errorf("unexpected error message: %s", err.Error())
	}

	// Test errors.Is works
	if !errors.Is(err, ErrConnectionFailed) {
		t.Error("errors.Is should match ErrConnectionFailed")
	}
}

func TestErrRequestTimeout(t *testing.T) {
	err := ErrRequestTimeout
	if err.Error() != "daemon: request timeout" {
		t.Errorf("unexpected error message: %s", err.Error())
	}

	// Test errors.Is works
	if !errors.Is(err, ErrRequestTimeout) {
		t.Error("errors.Is should match ErrRequestTimeout")
	}
}

func TestServerError(t *testing.T) {
	t.Run("basic error", func(t *testing.T) {
		err := NewServerError("ping", "server unavailable")
		if err.Error() != "ping failed: server unavailable" {
			t.Errorf("unexpected error message: %s", err.Error())
		}
		if err.Operation != "ping" {
			t.Errorf("unexpected operation: %s", err.Operation)
		}
		if err.Message != "server unavailable" {
			t.Errorf("unexpected message: %s", err.Message)
		}
	})

	t.Run("errors.As works", func(t *testing.T) {
		err := NewServerError("agent create", "agent limit reached")

		var serverErr *ServerError
		if !errors.As(err, &serverErr) {
			t.Error("errors.As should match *ServerError")
		}

		if serverErr.Operation != "agent create" {
			t.Errorf("unexpected operation: %s", serverErr.Operation)
		}
	})

	t.Run("different operations", func(t *testing.T) {
		tests := []struct {
			operation string
			message   string
			expected  string
		}{
			{"ping", "timeout", "ping failed: timeout"},
			{"project add", "already exists", "project add failed: already exists"},
			{"agent create", "limit reached", "agent create failed: limit reached"},
		}

		for _, tc := range tests {
			err := NewServerError(tc.operation, tc.message)
			if err.Error() != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, err.Error())
			}
		}
	})
}

func TestSendNotConnectedError(t *testing.T) {
	c := NewClient("/tmp/test.sock")
	_, err := c.Send(&Request{Type: MsgPing})
	if err == nil {
		t.Fatal("expected error when not connected")
	}

	if !errors.Is(err, ErrNotConnected) {
		t.Errorf("expected ErrNotConnected, got: %v", err)
	}
}
