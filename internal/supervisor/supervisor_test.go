package supervisor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/registry"
)

// newTestGitRepo creates a temp directory initialized as a git repository.
func newTestGitRepo(t *testing.T) (string, func()) {
	t.Helper()

	dir, err := os.MkdirTemp("", "fab-project-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	// Initialize as git repository
	cmd := exec.Command("git", "init")
	cmd.Dir = dir
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatalf("failed to init git repo: %v", err)
	}

	// Create initial commit (required for worktrees)
	cmd = exec.Command("git", "commit", "--allow-empty", "-m", "Initial commit")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test",
		"GIT_AUTHOR_EMAIL=test@test.com",
		"GIT_COMMITTER_NAME=Test",
		"GIT_COMMITTER_EMAIL=test@test.com",
	)
	if err := cmd.Run(); err != nil {
		_ = os.RemoveAll(dir)
		t.Fatalf("failed to create initial commit: %v", err)
	}

	cleanup := func() {
		_ = os.RemoveAll(dir)
	}

	return dir, cleanup
}

// newTestSupervisor creates a supervisor with a temporary registry for testing.
func newTestSupervisor(t *testing.T) (*Supervisor, func()) {
	t.Helper()

	// Create temp directory for config
	tmpDir, err := os.MkdirTemp("", "fab-supervisor-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	configPath := filepath.Join(tmpDir, "config.toml")
	reg, err := registry.NewWithPath(configPath)
	if err != nil {
		_ = os.RemoveAll(tmpDir)
		t.Fatalf("failed to create registry: %v", err)
	}

	agents := agent.NewManager()
	sup := New(reg, agents)

	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	return sup, cleanup
}

func TestSupervisor_HandlePing(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	req := &daemon.Request{
		Type: daemon.MsgPing,
		ID:   "test-1",
	}

	resp := sup.Handle(context.Background(), req)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}
	if resp.Type != daemon.MsgPing {
		t.Errorf("expected type %s, got %s", daemon.MsgPing, resp.Type)
	}
	if resp.ID != "test-1" {
		t.Errorf("expected ID test-1, got %s", resp.ID)
	}

	// Verify payload
	payload, ok := resp.Payload.(daemon.PingResponse)
	if !ok {
		t.Fatalf("expected PingResponse payload, got %T", resp.Payload)
	}
	if payload.Version != Version {
		t.Errorf("expected version %s, got %s", Version, payload.Version)
	}
	if payload.StartedAt.IsZero() {
		t.Error("expected non-zero started_at")
	}
}

func TestSupervisor_HandleShutdown(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	req := &daemon.Request{
		Type: daemon.MsgShutdown,
		ID:   "test-1",
	}

	resp := sup.Handle(context.Background(), req)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	// Verify shutdown channel is closed
	select {
	case <-sup.ShutdownCh():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("expected shutdown channel to be closed")
	}

	// Second shutdown should also succeed (idempotent)
	resp2 := sup.Handle(context.Background(), req)
	if !resp2.Success {
		t.Errorf("expected second shutdown to succeed, got error: %s", resp2.Error)
	}
}

func TestSupervisor_HandleStatus(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	req := &daemon.Request{
		Type: daemon.MsgStatus,
		ID:   "test-1",
	}

	resp := sup.Handle(context.Background(), req)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	payload, ok := resp.Payload.(daemon.StatusResponse)
	if !ok {
		t.Fatalf("expected StatusResponse payload, got %T", resp.Payload)
	}

	if !payload.Daemon.Running {
		t.Error("expected daemon to be running")
	}
	if payload.Daemon.Version != Version {
		t.Errorf("expected version %s, got %s", Version, payload.Daemon.Version)
	}
	if payload.Daemon.PID <= 0 {
		t.Error("expected positive PID")
	}
}

func TestSupervisor_HandleProjectAdd(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	// Create a temp git repository to act as "remote"
	projDir, projCleanup := newTestGitRepo(t)
	defer projCleanup()

	// Use file:// protocol for local "remote"
	remoteURL := "file://" + projDir

	req := &daemon.Request{
		Type: daemon.MsgProjectAdd,
		ID:   "test-1",
		Payload: map[string]any{
			"remote_url": remoteURL,
			"name":       "test-project",
			"max_agents": 5,
		},
	}

	resp := sup.Handle(context.Background(), req)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	payload, ok := resp.Payload.(daemon.ProjectAddResponse)
	if !ok {
		t.Fatalf("expected ProjectAddResponse payload, got %T", resp.Payload)
	}

	if payload.Name != "test-project" {
		t.Errorf("expected name test-project, got %s", payload.Name)
	}
	if payload.MaxAgents != 5 {
		t.Errorf("expected max_agents 5, got %d", payload.MaxAgents)
	}
	if len(payload.Worktrees) != 5 {
		t.Errorf("expected 5 worktrees, got %d", len(payload.Worktrees))
	}
}

func TestSupervisor_HandleProjectList(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	// Add a project first (using git repo as "remote")
	projDir, projCleanup := newTestGitRepo(t)
	defer projCleanup()

	addReq := &daemon.Request{
		Type: daemon.MsgProjectAdd,
		Payload: map[string]any{
			"remote_url": "file://" + projDir,
			"name":       "list-test",
		},
	}
	sup.Handle(context.Background(), addReq)

	// List projects
	req := &daemon.Request{
		Type: daemon.MsgProjectList,
		ID:   "test-1",
	}

	resp := sup.Handle(context.Background(), req)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	payload, ok := resp.Payload.(daemon.ProjectListResponse)
	if !ok {
		t.Fatalf("expected ProjectListResponse payload, got %T", resp.Payload)
	}

	if len(payload.Projects) != 1 {
		t.Errorf("expected 1 project, got %d", len(payload.Projects))
	}
	if payload.Projects[0].Name != "list-test" {
		t.Errorf("expected project name list-test, got %s", payload.Projects[0].Name)
	}
}

func TestSupervisor_HandleProjectRemove(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	// Add a project first (using git repo as "remote")
	projDir, projCleanup := newTestGitRepo(t)
	defer projCleanup()

	addReq := &daemon.Request{
		Type: daemon.MsgProjectAdd,
		Payload: map[string]any{
			"remote_url": "file://" + projDir,
			"name":       "remove-test",
		},
	}
	sup.Handle(context.Background(), addReq)

	// Remove the project
	req := &daemon.Request{
		Type: daemon.MsgProjectRemove,
		ID:   "test-1",
		Payload: map[string]any{
			"name": "remove-test",
		},
	}

	resp := sup.Handle(context.Background(), req)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	// Verify project is gone
	listReq := &daemon.Request{Type: daemon.MsgProjectList}
	listResp := sup.Handle(context.Background(), listReq)
	listPayload := listResp.Payload.(daemon.ProjectListResponse)

	if len(listPayload.Projects) != 0 {
		t.Errorf("expected 0 projects after removal, got %d", len(listPayload.Projects))
	}
}

func TestSupervisor_HandleAgentList(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	req := &daemon.Request{
		Type: daemon.MsgAgentList,
		ID:   "test-1",
	}

	resp := sup.Handle(context.Background(), req)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	payload, ok := resp.Payload.(daemon.AgentListResponse)
	if !ok {
		t.Fatalf("expected AgentListResponse payload, got %T", resp.Payload)
	}

	if len(payload.Agents) != 0 {
		t.Errorf("expected 0 agents, got %d", len(payload.Agents))
	}
}

func TestSupervisor_HandleUnknownType(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	req := &daemon.Request{
		Type: "unknown.type",
		ID:   "test-1",
	}

	resp := sup.Handle(context.Background(), req)

	if resp.Success {
		t.Error("expected error for unknown type")
	}
	if resp.Error == "" {
		t.Error("expected error message")
	}
}

func TestSupervisor_StartedAt(t *testing.T) {
	beforeCreate := time.Now()
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()
	afterCreate := time.Now()

	startedAt := sup.StartedAt()

	if startedAt.Before(beforeCreate) || startedAt.After(afterCreate) {
		t.Errorf("startedAt %v not in expected range [%v, %v]", startedAt, beforeCreate, afterCreate)
	}
}

func TestSupervisor_HandleStart(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	// Add a project first (using git repo as "remote")
	projDir, projCleanup := newTestGitRepo(t)
	defer projCleanup()

	addReq := &daemon.Request{
		Type: daemon.MsgProjectAdd,
		Payload: map[string]any{
			"remote_url": "file://" + projDir,
			"name":       "start-test",
		},
	}
	sup.Handle(context.Background(), addReq)

	// Start the project
	req := &daemon.Request{
		Type: daemon.MsgStart,
		ID:   "test-1",
		Payload: map[string]any{
			"project": "start-test",
		},
	}

	resp := sup.Handle(context.Background(), req)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	// Verify project is running
	statusReq := &daemon.Request{Type: daemon.MsgStatus}
	statusResp := sup.Handle(context.Background(), statusReq)
	statusPayload := statusResp.Payload.(daemon.StatusResponse)

	found := false
	for _, p := range statusPayload.Projects {
		if p.Name == "start-test" {
			found = true
			if !p.Running {
				t.Error("expected project to be running")
			}
		}
	}
	if !found {
		t.Error("project not found in status")
	}
}

func TestSupervisor_HandleStop(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	// Add and start a project first (using git repo as "remote")
	projDir, projCleanup := newTestGitRepo(t)
	defer projCleanup()

	addReq := &daemon.Request{
		Type: daemon.MsgProjectAdd,
		Payload: map[string]any{
			"remote_url": "file://" + projDir,
			"name":       "stop-test",
		},
	}
	sup.Handle(context.Background(), addReq)

	startReq := &daemon.Request{
		Type: daemon.MsgStart,
		Payload: map[string]any{
			"project": "stop-test",
		},
	}
	sup.Handle(context.Background(), startReq)

	// Stop the project
	req := &daemon.Request{
		Type: daemon.MsgStop,
		ID:   "test-1",
		Payload: map[string]any{
			"project": "stop-test",
		},
	}

	resp := sup.Handle(context.Background(), req)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	// Verify project is not running
	statusReq := &daemon.Request{Type: daemon.MsgStatus}
	statusResp := sup.Handle(context.Background(), statusReq)
	statusPayload := statusResp.Payload.(daemon.StatusResponse)

	for _, p := range statusPayload.Projects {
		if p.Name == "stop-test" {
			if p.Running {
				t.Error("expected project to not be running")
			}
		}
	}
}

func TestSupervisor_HandleProjectSet(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	// Add a project first (using git repo as "remote")
	projDir, projCleanup := newTestGitRepo(t)
	defer projCleanup()

	addReq := &daemon.Request{
		Type: daemon.MsgProjectAdd,
		Payload: map[string]any{
			"remote_url": "file://" + projDir,
			"name":       "set-test",
			"max_agents": 3,
		},
	}
	sup.Handle(context.Background(), addReq)

	// Update max_agents
	req := &daemon.Request{
		Type: daemon.MsgProjectSet,
		ID:   "test-1",
		Payload: map[string]any{
			"name":       "set-test",
			"max_agents": 10,
		},
	}

	resp := sup.Handle(context.Background(), req)

	if !resp.Success {
		t.Errorf("expected success, got error: %s", resp.Error)
	}

	// Verify the update
	listReq := &daemon.Request{Type: daemon.MsgProjectList}
	listResp := sup.Handle(context.Background(), listReq)
	listPayload := listResp.Payload.(daemon.ProjectListResponse)

	for _, p := range listPayload.Projects {
		if p.Name == "set-test" {
			if p.MaxAgents != 10 {
				t.Errorf("expected max_agents 10, got %d", p.MaxAgents)
			}
		}
	}
}

func TestSupervisor_HandleAgentDeleteNotFound(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	req := &daemon.Request{
		Type: daemon.MsgAgentDelete,
		ID:   "test-1",
		Payload: map[string]any{
			"id": "nonexistent",
		},
	}

	resp := sup.Handle(context.Background(), req)

	if resp.Success {
		t.Error("expected error for nonexistent agent")
	}
}

func TestSupervisor_HandleAgentInputNotFound(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	req := &daemon.Request{
		Type: daemon.MsgAgentInput,
		ID:   "test-1",
		Payload: map[string]any{
			"id":    "nonexistent",
			"input": "hello",
		},
	}

	resp := sup.Handle(context.Background(), req)

	if resp.Success {
		t.Error("expected error for nonexistent agent")
	}
}

func TestSupervisor_HandleAgentCreateNoProject(t *testing.T) {
	sup, cleanup := newTestSupervisor(t)
	defer cleanup()

	req := &daemon.Request{
		Type: daemon.MsgAgentCreate,
		ID:   "test-1",
		Payload: map[string]any{
			"project": "nonexistent",
		},
	}

	resp := sup.Handle(context.Background(), req)

	if resp.Success {
		t.Error("expected error for nonexistent project")
	}
}
