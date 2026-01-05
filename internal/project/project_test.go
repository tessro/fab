package project

import (
	"testing"
)

func TestNewProject(t *testing.T) {
	p := NewProject("myapp", "git@github.com:user/myapp.git")

	if p.Name != "myapp" {
		t.Errorf("Name = %q, want %q", p.Name, "myapp")
	}
	if p.RemoteURL != "git@github.com:user/myapp.git" {
		t.Errorf("RemoteURL = %q, want %q", p.RemoteURL, "git@github.com:user/myapp.git")
	}
	if p.MaxAgents != DefaultMaxAgents {
		t.Errorf("MaxAgents = %d, want %d", p.MaxAgents, DefaultMaxAgents)
	}
	if p.Running {
		t.Error("Running = true, want false")
	}
	if len(p.Worktrees) != 0 {
		t.Errorf("Worktrees = %d, want 0", len(p.Worktrees))
	}
}

func TestGetAvailableWorktree_NoWorktrees(t *testing.T) {
	p := NewProject("test", "")

	_, err := p.GetAvailableWorktree("agent1")
	if err != ErrNoWorktreeAvailable {
		t.Errorf("err = %v, want ErrNoWorktreeAvailable", err)
	}
}

func TestGetAvailableWorktree_AllInUse(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
		{Path: "/tmp/wt2", InUse: true, AgentID: "agent2"},
	}

	_, err := p.GetAvailableWorktree("agent3")
	if err != ErrNoWorktreeAvailable {
		t.Errorf("err = %v, want ErrNoWorktreeAvailable", err)
	}
}

func TestGetAvailableWorktree_ReturnsFirstAvailable(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
		{Path: "/tmp/wt2", InUse: false, AgentID: ""},
		{Path: "/tmp/wt3", InUse: false, AgentID: ""},
	}

	wt, err := p.GetAvailableWorktree("agent2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if wt.Path != "/tmp/wt2" {
		t.Errorf("Path = %q, want %q", wt.Path, "/tmp/wt2")
	}
	if !wt.InUse {
		t.Error("InUse = false, want true")
	}
	if wt.AgentID != "agent2" {
		t.Errorf("AgentID = %q, want %q", wt.AgentID, "agent2")
	}
}

func TestGetAvailableWorktree_MarksInUse(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: false, AgentID: ""},
	}

	wt, err := p.GetAvailableWorktree("agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the worktree in the slice is marked
	if !p.Worktrees[0].InUse {
		t.Error("Worktree in pool should be marked InUse")
	}
	if p.Worktrees[0].AgentID != "agent1" {
		t.Errorf("Worktree AgentID = %q, want %q", p.Worktrees[0].AgentID, "agent1")
	}

	// Verify returned pointer reflects the same
	if !wt.InUse || wt.AgentID != "agent1" {
		t.Error("Returned worktree should be marked InUse with agent1")
	}
}

func TestReleaseWorktree_ByPath(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
		{Path: "/tmp/wt2", InUse: true, AgentID: "agent2"},
	}

	err := p.ReleaseWorktree("/tmp/wt1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Worktrees[0].InUse {
		t.Error("Worktree 0 InUse = true, want false")
	}
	if p.Worktrees[0].AgentID != "" {
		t.Errorf("Worktree 0 AgentID = %q, want empty", p.Worktrees[0].AgentID)
	}
	// Other worktree should be unaffected
	if !p.Worktrees[1].InUse {
		t.Error("Worktree 1 should still be in use")
	}
}

func TestReleaseWorktree_NotFound(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
	}

	err := p.ReleaseWorktree("/tmp/nonexistent")
	if err != ErrWorktreeNotFound {
		t.Errorf("err = %v, want ErrWorktreeNotFound", err)
	}
}

func TestReleaseWorktreeByAgent(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
		{Path: "/tmp/wt2", InUse: true, AgentID: "agent2"},
	}

	err := p.ReleaseWorktreeByAgent("agent2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Agent1's worktree should be unaffected
	if !p.Worktrees[0].InUse {
		t.Error("Worktree 0 should still be in use")
	}
	// Agent2's worktree should be released
	if p.Worktrees[1].InUse {
		t.Error("Worktree 1 InUse = true, want false")
	}
	if p.Worktrees[1].AgentID != "" {
		t.Errorf("Worktree 1 AgentID = %q, want empty", p.Worktrees[1].AgentID)
	}
}

func TestReleaseWorktreeByAgent_NotFound(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
	}

	err := p.ReleaseWorktreeByAgent("nonexistent")
	if err != ErrWorktreeNotFound {
		t.Errorf("err = %v, want ErrWorktreeNotFound", err)
	}
}

func TestReturnWorktreeToPool(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
		{Path: "/tmp/wt2", InUse: true, AgentID: "agent2"},
	}

	err := p.ReturnWorktreeToPool("agent1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Agent1's worktree should be released
	if p.Worktrees[0].InUse {
		t.Error("Worktree 0 InUse = true, want false")
	}
	if p.Worktrees[0].AgentID != "" {
		t.Errorf("Worktree 0 AgentID = %q, want empty", p.Worktrees[0].AgentID)
	}
	// Agent2's worktree should be unaffected
	if !p.Worktrees[1].InUse {
		t.Error("Worktree 1 should still be in use")
	}
}

func TestReturnWorktreeToPool_NotFound(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
	}

	err := p.ReturnWorktreeToPool("nonexistent")
	if err != ErrWorktreeNotFound {
		t.Errorf("err = %v, want ErrWorktreeNotFound", err)
	}
}

func TestReturnWorktreeToPool_Reuse(t *testing.T) {
	// Test the acquire-return-reacquire cycle
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: false, AgentID: ""},
	}

	// Acquire worktree
	wt1, err := p.GetAvailableWorktree("agent1")
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if wt1.Path != "/tmp/wt1" {
		t.Errorf("wt1.Path = %q, want /tmp/wt1", wt1.Path)
	}

	// Return to pool
	if err := p.ReturnWorktreeToPool("agent1"); err != nil {
		t.Fatalf("return: %v", err)
	}

	// Reacquire - should get the same worktree
	wt2, err := p.GetAvailableWorktree("agent2")
	if err != nil {
		t.Fatalf("reacquire: %v", err)
	}
	if wt2.Path != "/tmp/wt1" {
		t.Errorf("wt2.Path = %q, want /tmp/wt1 (recycled)", wt2.Path)
	}
	if wt2.AgentID != "agent2" {
		t.Errorf("wt2.AgentID = %q, want agent2", wt2.AgentID)
	}
}

func TestWorktreePoolCycle(t *testing.T) {
	// Test the full acquire-release cycle
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: false, AgentID: ""},
		{Path: "/tmp/wt2", InUse: false, AgentID: ""},
	}

	// Acquire first worktree
	wt1, err := p.GetAvailableWorktree("agent1")
	if err != nil {
		t.Fatalf("acquire 1: %v", err)
	}
	if wt1.Path != "/tmp/wt1" {
		t.Errorf("wt1.Path = %q, want /tmp/wt1", wt1.Path)
	}

	// Acquire second worktree
	wt2, err := p.GetAvailableWorktree("agent2")
	if err != nil {
		t.Fatalf("acquire 2: %v", err)
	}
	if wt2.Path != "/tmp/wt2" {
		t.Errorf("wt2.Path = %q, want /tmp/wt2", wt2.Path)
	}

	// All worktrees in use
	_, err = p.GetAvailableWorktree("agent3")
	if err != ErrNoWorktreeAvailable {
		t.Errorf("acquire 3: err = %v, want ErrNoWorktreeAvailable", err)
	}

	// Release first worktree
	if err := p.ReleaseWorktreeByAgent("agent1"); err != nil {
		t.Fatalf("release agent1: %v", err)
	}

	// Now we can acquire again
	wt3, err := p.GetAvailableWorktree("agent3")
	if err != nil {
		t.Fatalf("acquire after release: %v", err)
	}
	if wt3.Path != "/tmp/wt1" {
		t.Errorf("wt3.Path = %q, want /tmp/wt1 (recycled)", wt3.Path)
	}
	if wt3.AgentID != "agent3" {
		t.Errorf("wt3.AgentID = %q, want agent3", wt3.AgentID)
	}
}

func TestAvailableWorktreeCount(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
		{Path: "/tmp/wt2", InUse: false, AgentID: ""},
		{Path: "/tmp/wt3", InUse: true, AgentID: "agent3"},
	}

	count := p.AvailableWorktreeCount()
	if count != 1 {
		t.Errorf("AvailableWorktreeCount() = %d, want 1", count)
	}
}

func TestActiveAgentCount(t *testing.T) {
	p := NewProject("test", "")
	p.Worktrees = []Worktree{
		{Path: "/tmp/wt1", InUse: true, AgentID: "agent1"},
		{Path: "/tmp/wt2", InUse: false, AgentID: ""},
		{Path: "/tmp/wt3", InUse: true, AgentID: "agent3"},
	}

	count := p.ActiveAgentCount()
	if count != 2 {
		t.Errorf("ActiveAgentCount() = %d, want 2", count)
	}
}

func TestSetRunning(t *testing.T) {
	p := NewProject("test", "")

	if p.IsRunning() {
		t.Error("IsRunning() = true, want false initially")
	}

	p.SetRunning(true)
	if !p.IsRunning() {
		t.Error("IsRunning() = false after SetRunning(true)")
	}

	p.SetRunning(false)
	if p.IsRunning() {
		t.Error("IsRunning() = true after SetRunning(false)")
	}
}

func TestAddWorktree(t *testing.T) {
	p := NewProject("test", "")

	p.AddWorktree(Worktree{Path: "/tmp/wt1", InUse: false})
	p.AddWorktree(Worktree{Path: "/tmp/wt2", InUse: true, AgentID: "agent1"})

	if len(p.Worktrees) != 2 {
		t.Errorf("len(Worktrees) = %d, want 2", len(p.Worktrees))
	}
	if p.Worktrees[0].Path != "/tmp/wt1" {
		t.Errorf("Worktrees[0].Path = %q, want /tmp/wt1", p.Worktrees[0].Path)
	}
	if p.Worktrees[1].AgentID != "agent1" {
		t.Errorf("Worktrees[1].AgentID = %q, want agent1", p.Worktrees[1].AgentID)
	}
}

func TestProjectDirs(t *testing.T) {
	p := NewProject("myapp", "")

	// These should return paths without error
	projectDir := p.ProjectDir()
	if projectDir == "" {
		t.Error("ProjectDir() returned empty string")
	}

	repoDir := p.RepoDir()
	if repoDir == "" {
		t.Error("RepoDir() returned empty string")
	}

	wtDir := p.WorktreesDir()
	if wtDir == "" {
		t.Error("WorktreesDir() returned empty string")
	}

	// Verify relationships
	if repoDir != projectDir+"/repo" {
		t.Errorf("RepoDir() = %q, want %q/repo", repoDir, projectDir)
	}
	if wtDir != projectDir+"/worktrees" {
		t.Errorf("WorktreesDir() = %q, want %q/worktrees", wtDir, projectDir)
	}
}
