package daemon

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestServer_StartStop(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		return &Response{Success: true}
	})

	srv := NewServer(socketPath, handler)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Verify socket exists
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		t.Fatal("socket file not created")
	}

	// Verify we can connect
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	conn.Close()

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Verify socket removed
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatal("socket file not removed after Stop()")
	}
}

func TestServer_RequestResponse(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if req.Type == MsgPing {
			return &Response{
				Type:    MsgPing,
				ID:      req.ID,
				Success: true,
				Payload: &PingResponse{
					Version:   "1.0.0",
					Uptime:    "1h",
					StartedAt: time.Now(),
				},
			}
		}
		return &Response{Success: false, Error: "unknown message type"}
	})

	srv := NewServer(socketPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	// Connect and send request
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	req := &Request{
		Type: MsgPing,
		ID:   "test-1",
	}
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if !resp.Success {
		t.Errorf("expected Success=true, got false with error: %s", resp.Error)
	}
	if resp.Type != MsgPing {
		t.Errorf("expected Type=%s, got %s", MsgPing, resp.Type)
	}
	if resp.ID != "test-1" {
		t.Errorf("expected ID=test-1, got %s", resp.ID)
	}
}

func TestServer_DoubleStart(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		return &Response{Success: true}
	})

	srv := NewServer(socketPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	// Second start should fail
	if err := srv.Start(); err == nil {
		t.Error("expected error on double Start(), got nil")
	}
}

func TestServer_ContextContainsConnAndServer(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	var gotConn net.Conn
	var gotServer *Server

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		gotConn = ConnFromContext(ctx)
		gotServer = ServerFromContext(ctx)
		return &Response{Success: true}
	})

	srv := NewServer(socketPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(&Request{Type: MsgPing}); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if gotConn == nil {
		t.Error("ConnFromContext returned nil")
	}
	if gotServer != srv {
		t.Error("ServerFromContext did not return the server")
	}
}

func TestServer_AttachBroadcast(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if req.Type == MsgAttach {
			conn := ConnFromContext(ctx)
			srv := ServerFromContext(ctx)
			encoder := EncoderFromContext(ctx)
			writeMu := WriteMuFromContext(ctx)
			srv.Attach(conn, nil, encoder, writeMu) // Subscribe to all projects
		}
		return &Response{Success: true}
	})

	srv := NewServer(socketPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	// Connect and attach
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Send attach request
	if err := encoder.Encode(&Request{Type: MsgAttach}); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	// Read response
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if srv.AttachedCount() != 1 {
		t.Errorf("expected 1 attached client, got %d", srv.AttachedCount())
	}

	// Broadcast event
	event := &StreamEvent{
		Type:    "output",
		AgentID: "agent-1",
		Project: "test-project",
		Data:    "hello world",
	}
	srv.Broadcast(event)

	// Read broadcast event
	var receivedEvent StreamEvent
	if err := decoder.Decode(&receivedEvent); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if receivedEvent.Data != "hello world" {
		t.Errorf("expected Data='hello world', got %s", receivedEvent.Data)
	}
	if receivedEvent.AgentID != "agent-1" {
		t.Errorf("expected AgentID='agent-1', got %s", receivedEvent.AgentID)
	}
}

func TestServer_AttachWithProjectFilter(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if req.Type == MsgAttach {
			conn := ConnFromContext(ctx)
			srv := ServerFromContext(ctx)
			encoder := EncoderFromContext(ctx)
			writeMu := WriteMuFromContext(ctx)
			// Only subscribe to "project-a"
			srv.Attach(conn, []string{"project-a"}, encoder, writeMu)
		}
		return &Response{Success: true}
	})

	srv := NewServer(socketPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	if err := encoder.Encode(&Request{Type: MsgAttach}); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	// Broadcast to project-b (should not be received)
	srv.Broadcast(&StreamEvent{
		Type:    "output",
		AgentID: "agent-b",
		Project: "project-b",
		Data:    "should not receive",
	})

	// Broadcast to project-a (should be received)
	srv.Broadcast(&StreamEvent{
		Type:    "output",
		AgentID: "agent-a",
		Project: "project-a",
		Data:    "should receive",
	})

	// Set read deadline to avoid hanging if filtering works
	_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))

	var event StreamEvent
	if err := decoder.Decode(&event); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if event.Project != "project-a" {
		t.Errorf("expected Project='project-a', got %s", event.Project)
	}
	if event.Data != "should receive" {
		t.Errorf("expected Data='should receive', got %s", event.Data)
	}
}

func TestDefaultSocketPath(t *testing.T) {
	path := DefaultSocketPath()
	if path == "" {
		t.Error("DefaultSocketPath() returned empty string")
	}

	home, err := os.UserHomeDir()
	if err == nil {
		expected := filepath.Join(home, ".fab", "fab.sock")
		if path != expected {
			t.Errorf("DefaultSocketPath() = %s, want %s", path, expected)
		}
	}
}
