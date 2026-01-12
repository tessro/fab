package daemon

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDecodePayload(t *testing.T) {
	t.Run("nil payload returns zero value", func(t *testing.T) {
		result, err := decodePayload[PingResponse](nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result == nil {
			t.Fatal("expected non-nil result")
		}
		if result.Version != "" || result.Uptime != "" {
			t.Error("expected zero value struct")
		}
	})

	t.Run("valid payload decodes correctly", func(t *testing.T) {
		payload := map[string]any{
			"version": "1.0.0",
			"uptime":  "1h30m",
		}
		result, err := decodePayload[PingResponse](payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Version != "1.0.0" {
			t.Errorf("expected version 1.0.0, got %s", result.Version)
		}
		if result.Uptime != "1h30m" {
			t.Errorf("expected uptime 1h30m, got %s", result.Uptime)
		}
	})

	t.Run("complex struct decodes correctly", func(t *testing.T) {
		payload := map[string]any{
			"projects": []map[string]any{
				{"name": "proj1", "remote_url": "git@github.com:user/p1.git", "max_agents": 3, "running": true},
				{"name": "proj2", "remote_url": "git@github.com:user/p2.git", "max_agents": 2, "running": false},
			},
		}
		result, err := decodePayload[ProjectListResponse](payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(result.Projects) != 2 {
			t.Fatalf("expected 2 projects, got %d", len(result.Projects))
		}
		if result.Projects[0].Name != "proj1" {
			t.Errorf("expected proj1, got %s", result.Projects[0].Name)
		}
		if result.Projects[1].MaxAgents != 2 {
			t.Errorf("expected max agents 2, got %d", result.Projects[1].MaxAgents)
		}
	})

	t.Run("type mismatch returns error", func(t *testing.T) {
		// Create a payload that can't be unmarshaled to the target type
		// JSON unmarshal is lenient, so we need a truly incompatible type
		type BadTarget struct {
			Count int `json:"count"`
		}
		payload := map[string]any{
			"count": "not a number", // string instead of int
		}
		_, err := decodePayload[BadTarget](payload)
		if err == nil {
			t.Error("expected error for type mismatch")
		}
		if !strings.Contains(err.Error(), "unmarshal payload") {
			t.Errorf("expected unmarshal error, got: %v", err)
		}
	})

	t.Run("already typed payload decodes correctly", func(t *testing.T) {
		// When server returns an already-typed response that gets re-encoded
		payload := PingResponse{
			Version: "2.0.0",
			Uptime:  "2h",
		}
		result, err := decodePayload[PingResponse](payload)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Version != "2.0.0" {
			t.Errorf("expected version 2.0.0, got %s", result.Version)
		}
	})
}

// shortTempDir creates a temp directory with a short path for socket tests.
// Unix sockets have a path limit (~104 chars on macOS), and t.TempDir()
// includes the full test name which can exceed this limit.
func shortClientTempDir(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "fab-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	return dir, func() { os.RemoveAll(dir) }
}

func TestNewClient(t *testing.T) {
	t.Run("with explicit path", func(t *testing.T) {
		c := NewClient("/tmp/test.sock")
		if c.SocketPath() != "/tmp/test.sock" {
			t.Errorf("expected /tmp/test.sock, got %s", c.SocketPath())
		}
	})

	t.Run("with empty path uses default", func(t *testing.T) {
		c := NewClient("")
		if c.SocketPath() != DefaultSocketPath() {
			t.Errorf("expected default path, got %s", c.SocketPath())
		}
	})
}

func TestClient_ConnectClose(t *testing.T) {
	tmpDir, cleanup := shortClientTempDir(t)
	defer cleanup()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Start a server
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		return &Response{Success: true}
	})
	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	// Create client
	c := NewClient(sockPath)

	if c.IsConnected() {
		t.Error("should not be connected initially")
	}

	// Connect
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}

	if !c.IsConnected() {
		t.Error("should be connected after Connect()")
	}

	// Double connect should be idempotent
	if err := c.Connect(); err != nil {
		t.Errorf("double connect should not error: %v", err)
	}

	// Close
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	if c.IsConnected() {
		t.Error("should not be connected after Close()")
	}

	// Double close should be idempotent
	if err := c.Close(); err != nil {
		t.Errorf("double close should not error: %v", err)
	}
}

func TestClient_ConnectFails(t *testing.T) {
	c := NewClient("/nonexistent/path/test.sock")
	if err := c.Connect(); err == nil {
		t.Error("expected error connecting to nonexistent socket")
	}
}

func TestClient_SendWithoutConnect(t *testing.T) {
	c := NewClient("/tmp/test.sock")
	_, err := c.Send(&Request{Type: MsgPing})
	if err == nil {
		t.Error("expected error sending without connect")
	}
}

func TestClient_Ping(t *testing.T) {
	tmpDir, cleanup := shortClientTempDir(t)
	defer cleanup()
	sockPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if req.Type == MsgPing {
			return &Response{
				Type:    MsgPing,
				Success: true,
				Payload: PingResponse{
					Version:   "1.0.0",
					Uptime:    "1h30m",
					StartedAt: time.Now(),
				},
			}
		}
		return &Response{Success: false, Error: "unknown"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	ping, err := c.Ping()
	if err != nil {
		t.Fatalf("ping: %v", err)
	}

	if ping.Version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %s", ping.Version)
	}
	if ping.Uptime != "1h30m" {
		t.Errorf("expected uptime 1h30m, got %s", ping.Uptime)
	}
}

func TestClient_Status(t *testing.T) {
	tmpDir, cleanup := shortClientTempDir(t)
	defer cleanup()
	sockPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if req.Type == MsgStatus {
			return &Response{
				Type:    MsgStatus,
				Success: true,
				Payload: StatusResponse{
					Daemon: DaemonStatus{
						Running: true,
						PID:     12345,
						Version: "1.0.0",
					},
					Supervisor: SupervisorStatus{
						ActiveProjects: 2,
						TotalAgents:    5,
						RunningAgents:  3,
					},
				},
			}
		}
		return &Response{Success: false, Error: "unknown"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	status, err := c.Status()
	if err != nil {
		t.Fatalf("status: %v", err)
	}

	if !status.Daemon.Running {
		t.Error("expected daemon running")
	}
	if status.Supervisor.ActiveProjects != 2 {
		t.Errorf("expected 2 active projects, got %d", status.Supervisor.ActiveProjects)
	}
}

func TestClient_StartStop(t *testing.T) {
	tmpDir, cleanup := shortClientTempDir(t)
	defer cleanup()
	sockPath := filepath.Join(tmpDir, "test.sock")

	var lastStart, lastStop string
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		switch req.Type {
		case MsgStart:
			payload, err := decodePayload[StartRequest](req.Payload)
			if err != nil {
				return &Response{Success: false, Error: err.Error()}
			}
			lastStart = payload.Project
			return &Response{Success: true}
		case MsgStop:
			payload, err := decodePayload[StopRequest](req.Payload)
			if err != nil {
				return &Response{Success: false, Error: err.Error()}
			}
			lastStop = payload.Project
			return &Response{Success: true}
		}
		return &Response{Success: false, Error: "unknown"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	if err := c.Start("my-project", false); err != nil {
		t.Fatalf("start: %v", err)
	}
	if lastStart != "my-project" {
		t.Errorf("expected project my-project, got %s", lastStart)
	}

	if err := c.Stop("my-project", false); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if lastStop != "my-project" {
		t.Errorf("expected project my-project, got %s", lastStop)
	}
}

func TestClient_ProjectOperations(t *testing.T) {
	tmpDir, cleanup := shortClientTempDir(t)
	defer cleanup()
	sockPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		switch req.Type {
		case MsgProjectAdd:
			return &Response{
				Success: true,
				Payload: ProjectAddResponse{
					Name:      "test-proj",
					RemoteURL: "git@github.com:user/test.git",
					RepoDir:   "/path/to/test/repo",
					MaxAgents: 3,
				},
			}
		case MsgProjectList:
			return &Response{
				Success: true,
				Payload: ProjectListResponse{
					Projects: []ProjectInfo{
						{Name: "proj1", RemoteURL: "git@github.com:user/p1.git", MaxAgents: 3, Running: true},
						{Name: "proj2", RemoteURL: "git@github.com:user/p2.git", MaxAgents: 2, Running: false},
					},
				},
			}
		case MsgProjectRemove:
			return &Response{Success: true}
		case MsgProjectSet:
			return &Response{Success: true}
		}
		return &Response{Success: false, Error: "unknown"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	t.Run("add", func(t *testing.T) {
		result, err := c.ProjectAdd("/path/to/test", "test-proj", 3, false, "")
		if err != nil {
			t.Fatalf("project add: %v", err)
		}
		if result.Name != "test-proj" {
			t.Errorf("expected name test-proj, got %s", result.Name)
		}
	})

	t.Run("list", func(t *testing.T) {
		result, err := c.ProjectList()
		if err != nil {
			t.Fatalf("project list: %v", err)
		}
		if len(result.Projects) != 2 {
			t.Errorf("expected 2 projects, got %d", len(result.Projects))
		}
	})

	t.Run("remove", func(t *testing.T) {
		if err := c.ProjectRemove("test-proj", true); err != nil {
			t.Fatalf("project remove: %v", err)
		}
	})

	t.Run("set", func(t *testing.T) {
		maxAgents := 5
		if err := c.ProjectSet("test-proj", &maxAgents, nil); err != nil {
			t.Fatalf("project set: %v", err)
		}
	})
}

func TestClient_AgentOperations(t *testing.T) {
	tmpDir, cleanup := shortClientTempDir(t)
	defer cleanup()
	sockPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		switch req.Type {
		case MsgAgentList:
			return &Response{
				Success: true,
				Payload: AgentListResponse{
					Agents: []AgentStatus{
						{ID: "agent-1", Project: "proj1", State: "running"},
						{ID: "agent-2", Project: "proj1", State: "idle"},
					},
				},
			}
		case MsgAgentCreate:
			return &Response{
				Success: true,
				Payload: AgentCreateResponse{
					ID:       "agent-3",
					Project:  "proj1",
					Worktree: "/path/.fab-worktrees/3",
				},
			}
		case MsgAgentDelete:
			return &Response{Success: true}
		case MsgAgentInput:
			return &Response{Success: true}
		}
		return &Response{Success: false, Error: "unknown"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	t.Run("list", func(t *testing.T) {
		result, err := c.AgentList("proj1")
		if err != nil {
			t.Fatalf("agent list: %v", err)
		}
		if len(result.Agents) != 2 {
			t.Errorf("expected 2 agents, got %d", len(result.Agents))
		}
	})

	t.Run("create", func(t *testing.T) {
		result, err := c.AgentCreate("proj1", "task-123")
		if err != nil {
			t.Fatalf("agent create: %v", err)
		}
		if result.ID != "agent-3" {
			t.Errorf("expected ID agent-3, got %s", result.ID)
		}
	})

	t.Run("delete", func(t *testing.T) {
		if err := c.AgentDelete("agent-1", false); err != nil {
			t.Fatalf("agent delete: %v", err)
		}
	})

	t.Run("input", func(t *testing.T) {
		if err := c.AgentInput("agent-1", "hello\n"); err != nil {
			t.Fatalf("agent input: %v", err)
		}
	})
}

func TestClient_AttachDetach(t *testing.T) {
	tmpDir, cleanup := shortClientTempDir(t)
	defer cleanup()
	sockPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		srv := ServerFromContext(ctx)
		conn := ConnFromContext(ctx)
		encoder := EncoderFromContext(ctx)
		writeMu := WriteMuFromContext(ctx)

		switch req.Type {
		case MsgAttach:
			payload, err := decodePayload[AttachRequest](req.Payload)
			if err != nil {
				return &Response{Success: false, Error: err.Error()}
			}
			srv.Attach(conn, payload.Projects, encoder, writeMu)
			return &Response{Success: true}
		case MsgDetach:
			srv.Detach(conn)
			return &Response{Success: true}
		}
		return &Response{Success: false, Error: "unknown"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	if c.IsAttached() {
		t.Error("should not be attached initially")
	}

	if err := c.Attach(nil); err != nil {
		t.Fatalf("attach: %v", err)
	}

	if !c.IsAttached() {
		t.Error("should be attached after Attach()")
	}

	if srv.AttachedCount() != 1 {
		t.Errorf("server should have 1 attached client, got %d", srv.AttachedCount())
	}

	if err := c.Detach(); err != nil {
		t.Fatalf("detach: %v", err)
	}

	if c.IsAttached() {
		t.Error("should not be attached after Detach()")
	}
}

func TestClient_RecvEvent(t *testing.T) {
	tmpDir, cleanup := shortClientTempDir(t)
	defer cleanup()
	sockPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		srv := ServerFromContext(ctx)
		conn := ConnFromContext(ctx)
		encoder := EncoderFromContext(ctx)
		writeMu := WriteMuFromContext(ctx)

		if req.Type == MsgAttach {
			srv.Attach(conn, nil, encoder, writeMu)
			return &Response{Success: true}
		}
		return &Response{Success: false, Error: "unknown"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	if err := c.Attach(nil); err != nil {
		t.Fatalf("attach: %v", err)
	}

	// Broadcast an event from server
	go func() {
		time.Sleep(50 * time.Millisecond)
		srv.Broadcast(&StreamEvent{
			Type:    "output",
			AgentID: "agent-1",
			Project: "test-project",
			Data:    "hello from agent",
		})
	}()

	event, err := c.RecvEvent()
	if err != nil {
		t.Fatalf("recv event: %v", err)
	}

	if event.Type != "output" {
		t.Errorf("expected type output, got %s", event.Type)
	}
	if event.Data != "hello from agent" {
		t.Errorf("expected data 'hello from agent', got %s", event.Data)
	}
}

func TestClient_Shutdown(t *testing.T) {
	tmpDir, cleanup := shortClientTempDir(t)
	defer cleanup()
	sockPath := filepath.Join(tmpDir, "test.sock")

	shutdownCalled := false
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		if req.Type == MsgShutdown {
			shutdownCalled = true
			return &Response{Success: true}
		}
		return &Response{Success: false, Error: "unknown"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	if err := c.Shutdown(false); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if !shutdownCalled {
		t.Error("shutdown handler was not called")
	}
}

func TestClient_ErrorResponses(t *testing.T) {
	tmpDir, cleanup := shortClientTempDir(t)
	defer cleanup()
	sockPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		return &Response{Success: false, Error: "something went wrong"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer func() { _ = srv.Stop() }()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// All operations should return errors
	if _, err := c.Ping(); err == nil {
		t.Error("expected error from Ping")
	}
	if err := c.Shutdown(false); err == nil {
		t.Error("expected error from Shutdown")
	}
	if _, err := c.Status(); err == nil {
		t.Error("expected error from Status")
	}
	if err := c.Start("p", false); err == nil {
		t.Error("expected error from Start")
	}
	if err := c.Stop("p", false); err == nil {
		t.Error("expected error from Stop")
	}
}
