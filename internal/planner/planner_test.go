package planner_test

import (
	"os/exec"
	"testing"

	"github.com/tessro/fab/internal/backend"
	"github.com/tessro/fab/internal/planner"
)

// mockBackend implements backend.Backend for testing.
type mockBackend struct {
	buildCommandCalled bool
	hookSettingsCalled bool
	lastConfig         backend.CommandConfig
	lastFabPath        string
}

func (m *mockBackend) Name() string { return "mock" }

func (m *mockBackend) BuildCommand(cfg backend.CommandConfig) (*exec.Cmd, error) {
	m.buildCommandCalled = true
	m.lastConfig = cfg
	// Return a simple command that will exist
	return exec.Command("echo", "test"), nil
}

func (m *mockBackend) ParseStreamMessage(line []byte) (*backend.StreamMessage, error) {
	if len(line) == 0 {
		return nil, nil
	}
	return &backend.StreamMessage{Type: "test"}, nil
}

func (m *mockBackend) FormatInputMessage(content string, sessionID string) ([]byte, error) {
	return []byte(content), nil
}

func (m *mockBackend) HookSettings(fabPath string) map[string]any {
	m.hookSettingsCalled = true
	m.lastFabPath = fabPath
	return map[string]any{"test": true}
}

// Verify mockBackend implements backend.Backend.
var _ backend.Backend = (*mockBackend)(nil)

func TestPlanner_New_AcceptsBackend(t *testing.T) {
	b := &mockBackend{}

	p := planner.New("test-id", "test-project", "/tmp", "test prompt", b)
	if p == nil {
		t.Fatal("New() returned nil")
	}

	// Verify planner was created with expected values
	if p.ID() != "test-id" {
		t.Errorf("ID() = %q, want %q", p.ID(), "test-id")
	}
	if p.Project() != "test-project" {
		t.Errorf("Project() = %q, want %q", p.Project(), "test-project")
	}
}

func TestManager_Create_AcceptsBackend(t *testing.T) {
	m := planner.NewManager()
	b := &mockBackend{}

	p, err := m.Create("test-project", "/tmp/workdir", "test prompt", b)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if p == nil {
		t.Fatal("Create() returned nil planner")
	}

	// Verify planner was registered
	retrieved, err := m.Get(p.ID())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if retrieved != p {
		t.Error("Get() returned different planner instance")
	}
}

func TestManager_CreateWithID_AcceptsBackend(t *testing.T) {
	m := planner.NewManager()
	b := &mockBackend{}

	p, err := m.CreateWithID("custom-id", "test-project", "/tmp/workdir", "test prompt", b)
	if err != nil {
		t.Fatalf("CreateWithID() error = %v", err)
	}
	if p == nil {
		t.Fatal("CreateWithID() returned nil planner")
	}

	// Verify the custom ID was used
	if p.ID() != "custom-id" {
		t.Errorf("ID() = %q, want %q", p.ID(), "custom-id")
	}

	// Verify planner was registered with the custom ID
	retrieved, err := m.Get("custom-id")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if retrieved != p {
		t.Error("Get() returned different planner instance")
	}
}

func TestManager_Count(t *testing.T) {
	m := planner.NewManager()
	b := &mockBackend{}

	if m.Count() != 0 {
		t.Errorf("Count() = %d, want 0", m.Count())
	}

	_, _ = m.Create("project1", "/tmp/1", "prompt1", b)
	if m.Count() != 1 {
		t.Errorf("Count() = %d, want 1", m.Count())
	}

	_, _ = m.Create("project2", "/tmp/2", "prompt2", b)
	if m.Count() != 2 {
		t.Errorf("Count() = %d, want 2", m.Count())
	}
}

func TestManager_ListByProject(t *testing.T) {
	m := planner.NewManager()
	b := &mockBackend{}

	// Create planners for different projects
	_, _ = m.Create("project-a", "/tmp/a1", "prompt1", b)
	_, _ = m.Create("project-a", "/tmp/a2", "prompt2", b)
	_, _ = m.Create("project-b", "/tmp/b1", "prompt3", b)

	// List planners for project-a
	projectAPlanners := m.ListByProject("project-a")
	if len(projectAPlanners) != 2 {
		t.Errorf("ListByProject(project-a) returned %d planners, want 2", len(projectAPlanners))
	}

	// List planners for project-b
	projectBPlanners := m.ListByProject("project-b")
	if len(projectBPlanners) != 1 {
		t.Errorf("ListByProject(project-b) returned %d planners, want 1", len(projectBPlanners))
	}

	// List planners for non-existent project
	noProjectPlanners := m.ListByProject("project-c")
	if len(noProjectPlanners) != 0 {
		t.Errorf("ListByProject(project-c) returned %d planners, want 0", len(noProjectPlanners))
	}
}
