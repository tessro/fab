package tk

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tessro/fab/internal/issue"
)

func TestBackend_AddComment(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()
	ticketsDir := filepath.Join(tmpDir, ".tickets")
	if err := os.MkdirAll(ticketsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a test issue file
	issueContent := `---
id: test-1
status: open
type: task
priority: 1
deps: []
links: []
created: 2024-01-15T10:00:00Z
---
# Test Issue

This is the issue description.
`
	if err := os.WriteFile(filepath.Join(ticketsDir, "test-1.md"), []byte(issueContent), 0644); err != nil {
		t.Fatal(err)
	}

	backend := &Backend{
		repoDir:    tmpDir,
		ticketsDir: ticketsDir,
		prefix:     "test-",
	}

	ctx := context.Background()

	// Add first comment
	err := backend.AddComment(ctx, "test-1", "This is the first comment")
	if err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	// Read the issue and verify comment was added
	iss, err := backend.Get(ctx, "test-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if !strings.Contains(iss.Description, "## Comments") {
		t.Error("Description should contain ## Comments section")
	}
	if !strings.Contains(iss.Description, "This is the first comment") {
		t.Error("Description should contain the comment text")
	}

	// Add second comment
	err = backend.AddComment(ctx, "test-1", "Second comment here")
	if err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	// Read again and verify both comments
	iss, err = backend.Get(ctx, "test-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if !strings.Contains(iss.Description, "This is the first comment") {
		t.Error("Description should still contain first comment")
	}
	if !strings.Contains(iss.Description, "Second comment here") {
		t.Error("Description should contain second comment")
	}

	// Verify original description is preserved
	if !strings.Contains(iss.Description, "This is the issue description.") {
		t.Error("Original description should be preserved")
	}
}

func TestBackend_AddComment_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	ticketsDir := filepath.Join(tmpDir, ".tickets")
	if err := os.MkdirAll(ticketsDir, 0755); err != nil {
		t.Fatal(err)
	}

	backend := &Backend{
		repoDir:    tmpDir,
		ticketsDir: ticketsDir,
		prefix:     "test-",
	}

	ctx := context.Background()

	// Try to add comment to non-existent issue
	err := backend.AddComment(ctx, "nonexistent", "A comment")
	if err == nil {
		t.Error("AddComment() should return error for non-existent issue")
	}
}

func TestBackend_UpsertPlanSection(t *testing.T) {
	tmpDir := t.TempDir()
	ticketsDir := filepath.Join(tmpDir, ".tickets")
	if err := os.MkdirAll(ticketsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a test issue file
	issueContent := `---
id: test-1
status: open
type: task
priority: 1
deps: []
links: []
created: 2024-01-15T10:00:00Z
---
# Test Issue

This is the issue description.
`
	if err := os.WriteFile(filepath.Join(ticketsDir, "test-1.md"), []byte(issueContent), 0644); err != nil {
		t.Fatal(err)
	}

	backend := &Backend{
		repoDir:    tmpDir,
		ticketsDir: ticketsDir,
		prefix:     "test-",
	}

	ctx := context.Background()

	// Add plan section
	planContent := "- [ ] Step 1: Research\n- [ ] Step 2: Implement\n- [ ] Step 3: Test"
	err := backend.UpsertPlanSection(ctx, "test-1", planContent)
	if err != nil {
		t.Fatalf("UpsertPlanSection() error = %v", err)
	}

	// Read the issue and verify plan was added
	iss, err := backend.Get(ctx, "test-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if !strings.Contains(iss.Description, "## Plan") {
		t.Error("Description should contain ## Plan section")
	}
	if !strings.Contains(iss.Description, "Step 1: Research") {
		t.Error("Description should contain plan content")
	}

	// Verify original description is preserved
	if !strings.Contains(iss.Description, "This is the issue description.") {
		t.Error("Original description should be preserved")
	}

	// Update plan section
	newPlanContent := "- [x] Step 1: Research (done)\n- [ ] Step 2: Implement"
	err = backend.UpsertPlanSection(ctx, "test-1", newPlanContent)
	if err != nil {
		t.Fatalf("UpsertPlanSection() error = %v", err)
	}

	// Verify plan was updated
	iss, err = backend.Get(ctx, "test-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if !strings.Contains(iss.Description, "Step 1: Research (done)") {
		t.Error("Description should contain updated plan")
	}
	if strings.Contains(iss.Description, "Step 3: Test") {
		t.Error("Old plan content should be replaced")
	}
}

func TestBackend_UpsertPlanSection_NonExistent(t *testing.T) {
	tmpDir := t.TempDir()
	ticketsDir := filepath.Join(tmpDir, ".tickets")
	if err := os.MkdirAll(ticketsDir, 0755); err != nil {
		t.Fatal(err)
	}

	backend := &Backend{
		repoDir:    tmpDir,
		ticketsDir: ticketsDir,
		prefix:     "test-",
	}

	ctx := context.Background()

	err := backend.UpsertPlanSection(ctx, "nonexistent", "- [ ] Step 1")
	if err == nil {
		t.Error("UpsertPlanSection() should return error for non-existent issue")
	}
}

func TestBackend_CommentsAndPlanTogether(t *testing.T) {
	tmpDir := t.TempDir()
	ticketsDir := filepath.Join(tmpDir, ".tickets")
	if err := os.MkdirAll(ticketsDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a test issue file
	issueContent := `---
id: test-1
status: open
type: task
priority: 1
deps: []
links: []
created: 2024-01-15T10:00:00Z
---
# Test Issue

This is the issue description.
`
	if err := os.WriteFile(filepath.Join(ticketsDir, "test-1.md"), []byte(issueContent), 0644); err != nil {
		t.Fatal(err)
	}

	backend := &Backend{
		repoDir:    tmpDir,
		ticketsDir: ticketsDir,
		prefix:     "test-",
	}

	ctx := context.Background()

	// Add plan section
	err := backend.UpsertPlanSection(ctx, "test-1", "- [ ] Step 1\n- [ ] Step 2")
	if err != nil {
		t.Fatalf("UpsertPlanSection() error = %v", err)
	}

	// Add comment
	err = backend.AddComment(ctx, "test-1", "Starting work on this issue")
	if err != nil {
		t.Fatalf("AddComment() error = %v", err)
	}

	// Verify both sections exist
	iss, err := backend.Get(ctx, "test-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if !strings.Contains(iss.Description, "## Plan") {
		t.Error("Description should contain ## Plan section")
	}
	if !strings.Contains(iss.Description, "## Comments") {
		t.Error("Description should contain ## Comments section")
	}
	if !strings.Contains(iss.Description, "Step 1") {
		t.Error("Plan content should be present")
	}
	if !strings.Contains(iss.Description, "Starting work on this issue") {
		t.Error("Comment should be present")
	}

	// Update plan and verify comments preserved
	err = backend.UpsertPlanSection(ctx, "test-1", "- [x] Step 1 (done)\n- [ ] Step 2")
	if err != nil {
		t.Fatalf("UpsertPlanSection() error = %v", err)
	}

	iss, err = backend.Get(ctx, "test-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	if !strings.Contains(iss.Description, "Starting work on this issue") {
		t.Error("Comments should be preserved after plan update")
	}
}

func TestBackend_CreateSubIssue_NotSupported(t *testing.T) {
	backend := &Backend{}
	ctx := context.Background()

	_, err := backend.CreateSubIssue(ctx, "parent", issue.CreateParams{Title: "Child"})
	if err != issue.ErrNotSupported {
		t.Errorf("CreateSubIssue() error = %v, want %v", err, issue.ErrNotSupported)
	}
}
