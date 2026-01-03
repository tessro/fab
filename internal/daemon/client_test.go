package daemon

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

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
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	// Start a server
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		return &Response{Success: true}
	})
	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer srv.Stop()

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
	tmpDir := t.TempDir()
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
	defer srv.Stop()

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
	tmpDir := t.TempDir()
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
	defer srv.Stop()

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
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	var lastStart, lastStop string
	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		switch req.Type {
		case MsgStart:
			var payload StartRequest
			data, _ := json.Marshal(req.Payload)
			json.Unmarshal(data, &payload)
			lastStart = payload.Project
			return &Response{Success: true}
		case MsgStop:
			var payload StopRequest
			data, _ := json.Marshal(req.Payload)
			json.Unmarshal(data, &payload)
			lastStop = payload.Project
			return &Response{Success: true}
		}
		return &Response{Success: false, Error: "unknown"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer srv.Stop()

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
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		switch req.Type {
		case MsgProjectAdd:
			return &Response{
				Success: true,
				Payload: ProjectAddResponse{
					Name:      "test-proj",
					Path:      "/path/to/test",
					MaxAgents: 3,
					Worktrees: []string{"/path/to/test/.fab-worktrees/1"},
				},
			}
		case MsgProjectList:
			return &Response{
				Success: true,
				Payload: ProjectListResponse{
					Projects: []ProjectInfo{
						{Name: "proj1", Path: "/p1", MaxAgents: 3, Running: true},
						{Name: "proj2", Path: "/p2", MaxAgents: 2, Running: false},
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
	defer srv.Stop()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	t.Run("add", func(t *testing.T) {
		result, err := c.ProjectAdd("/path/to/test", "test-proj", 3)
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
		if err := c.ProjectSet("test-proj", &maxAgents); err != nil {
			t.Fatalf("project set: %v", err)
		}
	})
}

func TestClient_AgentOperations(t *testing.T) {
	tmpDir := t.TempDir()
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
	defer srv.Stop()

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
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		srv := ServerFromContext(ctx)
		conn := ConnFromContext(ctx)

		switch req.Type {
		case MsgAttach:
			var payload AttachRequest
			if req.Payload != nil {
				data, _ := json.Marshal(req.Payload)
				json.Unmarshal(data, &payload)
			}
			srv.Attach(conn, payload.Projects)
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
	defer srv.Stop()

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
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		srv := ServerFromContext(ctx)
		conn := ConnFromContext(ctx)

		if req.Type == MsgAttach {
			srv.Attach(conn, nil)
			return &Response{Success: true}
		}
		return &Response{Success: false, Error: "unknown"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer srv.Stop()

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
	tmpDir := t.TempDir()
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
	defer srv.Stop()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	if err := c.Shutdown(); err != nil {
		t.Fatalf("shutdown: %v", err)
	}

	if !shutdownCalled {
		t.Error("shutdown handler was not called")
	}
}

func TestClient_ErrorResponses(t *testing.T) {
	tmpDir := t.TempDir()
	sockPath := filepath.Join(tmpDir, "test.sock")

	handler := HandlerFunc(func(ctx context.Context, req *Request) *Response {
		return &Response{Success: false, Error: "something went wrong"}
	})

	srv := NewServer(sockPath, handler)
	if err := srv.Start(); err != nil {
		t.Fatalf("server start: %v", err)
	}
	defer srv.Stop()

	c := NewClient(sockPath)
	if err := c.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer c.Close()

	// All operations should return errors
	if _, err := c.Ping(); err == nil {
		t.Error("expected error from Ping")
	}
	if err := c.Shutdown(); err == nil {
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
