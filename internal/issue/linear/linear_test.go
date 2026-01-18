package linear

import (
	"testing"

	"github.com/tessro/fab/internal/issue"
)

func TestName(t *testing.T) {
	// Create a mock backend without making API calls
	b := &Backend{
		projectID: "test-project",
		apiKey:    "test-key",
	}

	if got := b.Name(); got != "linear" {
		t.Errorf("Name() = %q, want %q", got, "linear")
	}
}

func TestMapPriorityToLinear(t *testing.T) {
	tests := []struct {
		fab    int
		linear int
	}{
		{0, 4}, // fab low -> linear low
		{1, 3}, // fab medium -> linear medium
		{2, 2}, // fab high -> linear high
		{3, 0}, // fab unknown -> linear none
	}

	for _, tc := range tests {
		got := mapPriorityToLinear(tc.fab)
		if got != tc.linear {
			t.Errorf("mapPriorityToLinear(%d) = %d, want %d", tc.fab, got, tc.linear)
		}
	}
}

func TestMapPriorityFromLinear(t *testing.T) {
	tests := []struct {
		linear int
		fab    int
	}{
		{1, 2}, // linear urgent -> fab high
		{2, 2}, // linear high -> fab high
		{3, 1}, // linear medium -> fab medium
		{4, 0}, // linear low -> fab low
		{0, 1}, // linear none -> fab medium (default)
	}

	for _, tc := range tests {
		got := mapPriorityFromLinear(tc.linear)
		if got != tc.fab {
			t.Errorf("mapPriorityFromLinear(%d) = %d, want %d", tc.linear, got, tc.fab)
		}
	}
}

func TestToIssue(t *testing.T) {
	b := &Backend{}

	li := &linearIssue{
		ID:         "uuid-123",
		Identifier: "FAB-123",
		Title:      "Test Issue",
		Priority:   3, // medium
	}
	li.State.Type = "started"
	li.State.Name = "In Progress"

	iss := b.toIssue(li)

	if iss.ID != "FAB-123" {
		t.Errorf("ID = %q, want %q", iss.ID, "FAB-123")
	}
	if iss.Title != "Test Issue" {
		t.Errorf("Title = %q, want %q", iss.Title, "Test Issue")
	}
	if iss.Status != issue.StatusOpen {
		t.Errorf("Status = %q, want %q", iss.Status, issue.StatusOpen)
	}
	if iss.Priority != 1 { // medium
		t.Errorf("Priority = %d, want %d", iss.Priority, 1)
	}
	if iss.Type != "task" { // default type
		t.Errorf("Type = %q, want %q", iss.Type, "task")
	}
}

func TestToIssue_Closed(t *testing.T) {
	b := &Backend{}

	li := &linearIssue{
		ID:         "uuid-456",
		Identifier: "FAB-456",
		Title:      "Closed Issue",
		Priority:   4, // low
	}
	li.State.Type = "completed"
	li.State.Name = "Done"

	iss := b.toIssue(li)

	if iss.Status != issue.StatusClosed {
		t.Errorf("Status = %q, want %q", iss.Status, issue.StatusClosed)
	}
}

func TestToIssue_Blocked(t *testing.T) {
	b := &Backend{}

	li := &linearIssue{
		ID:         "uuid-789",
		Identifier: "FAB-789",
		Title:      "Blocked Issue",
		Priority:   2, // high
	}
	li.State.Type = "started"
	li.Labels.Nodes = []struct {
		Name string `json:"name"`
	}{
		{Name: "blocked"},
		{Name: "bug"},
	}

	iss := b.toIssue(li)

	if iss.Status != issue.StatusBlocked {
		t.Errorf("Status = %q, want %q", iss.Status, issue.StatusBlocked)
	}
	if len(iss.Labels) != 1 || iss.Labels[0] != "bug" {
		t.Errorf("Labels = %v, want [bug]", iss.Labels)
	}
}

func TestToIssue_WithTypeLabel(t *testing.T) {
	b := &Backend{}

	li := &linearIssue{
		ID:         "uuid-111",
		Identifier: "FAB-111",
		Title:      "Feature Issue",
		Priority:   2,
	}
	li.State.Type = "started"
	li.Labels.Nodes = []struct {
		Name string `json:"name"`
	}{
		{Name: "type:feature"},
		{Name: "frontend"},
	}

	iss := b.toIssue(li)

	if iss.Type != "feature" {
		t.Errorf("Type = %q, want %q", iss.Type, "feature")
	}
	if len(iss.Labels) != 1 || iss.Labels[0] != "frontend" {
		t.Errorf("Labels = %v, want [frontend]", iss.Labels)
	}
}

func TestToIssue_WithParent(t *testing.T) {
	b := &Backend{}

	li := &linearIssue{
		ID:         "uuid-222",
		Identifier: "FAB-222",
		Title:      "Sub Issue",
		Priority:   3,
	}
	li.State.Type = "backlog"
	li.Parent = &struct {
		Identifier string `json:"identifier"`
	}{
		Identifier: "FAB-100",
	}

	iss := b.toIssue(li)

	if len(iss.Dependencies) != 1 || iss.Dependencies[0] != "FAB-100" {
		t.Errorf("Dependencies = %v, want [FAB-100]", iss.Dependencies)
	}
}

func TestNew_MissingAPIKey(t *testing.T) {
	// Temporarily clear the env var if set
	origKey := ""
	t.Setenv("LINEAR_API_KEY", origKey)

	_, err := New("/tmp", "project-id", nil)
	if err == nil {
		t.Error("New() with missing API key should return error")
	}
}

func TestNew_MissingProjectID(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "test-key")

	_, err := New("/tmp", "", nil)
	if err == nil {
		t.Error("New() with missing project ID should return error")
	}
}
