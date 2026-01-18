package linear

import (
	"testing"

	"github.com/tessro/fab/internal/issue"
)

func TestName(t *testing.T) {
	// Create a mock backend without making API calls
	b := &Backend{
		teamID:    "test-team",
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

// TestCreateSubIssue_ParentInResponse tests that the CreateSubIssue response
// correctly maps the parent to the Dependencies field. This validates that when
// Linear returns a parent in the response, it's properly converted to our Issue model.
// Note: The actual API call is not tested here (requires mocking), but the
// conversion logic that toIssue uses for parent handling is covered by TestToIssue_WithParent.
func TestCreateSubIssue_ParentInResponse(t *testing.T) {
	b := &Backend{}

	// Simulate response from Linear API with parent set
	li := &linearIssue{
		ID:         "uuid-child",
		Identifier: "FAB-200",
		Title:      "Child Issue",
		Priority:   3,
	}
	li.State.Type = "backlog"
	li.Parent = &struct {
		Identifier string `json:"identifier"`
	}{
		Identifier: "FAB-100", // Parent identifier
	}

	iss := b.toIssue(li)

	// Verify the child issue has the parent in Dependencies
	if iss.ID != "FAB-200" {
		t.Errorf("ID = %q, want %q", iss.ID, "FAB-200")
	}
	if len(iss.Dependencies) != 1 {
		t.Errorf("Dependencies length = %d, want 1", len(iss.Dependencies))
	}
	if len(iss.Dependencies) > 0 && iss.Dependencies[0] != "FAB-100" {
		t.Errorf("Dependencies[0] = %q, want %q", iss.Dependencies[0], "FAB-100")
	}
}

func TestNew_MissingAPIKey(t *testing.T) {
	// Temporarily clear the env var if set
	t.Setenv("LINEAR_API_KEY", "")

	_, err := New("/tmp", "team-id", "project-id", nil, "")
	if err == nil {
		t.Error("New() with missing API key should return error")
	}
}

func TestNew_MissingTeamID(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "test-key")

	_, err := New("/tmp", "", "", nil, "")
	if err == nil {
		t.Error("New() with missing team ID should return error")
	}
}

func TestNew_OptionalProjectID(t *testing.T) {
	t.Setenv("LINEAR_API_KEY", "test-key")

	// Project ID is optional, so this should succeed
	b, err := New("/tmp", "team-id", "", nil, "")
	if err != nil {
		t.Errorf("New() with missing project ID should succeed: %v", err)
	}
	if b.teamID != "team-id" {
		t.Errorf("teamID = %q, want %q", b.teamID, "team-id")
	}
	if b.projectID != "" {
		t.Errorf("projectID = %q, want empty", b.projectID)
	}
}

func TestNew_APIKeyFromConfig(t *testing.T) {
	// Clear env var to ensure config key is used
	t.Setenv("LINEAR_API_KEY", "")

	b, err := New("/tmp", "team-id", "project-id", nil, "config-api-key")
	if err != nil {
		t.Errorf("New() with config API key should not error: %v", err)
	}
	if b.apiKey != "config-api-key" {
		t.Errorf("apiKey = %q, want %q", b.apiKey, "config-api-key")
	}
}

func TestNew_APIKeyFromEnvFallback(t *testing.T) {
	// Set env var and leave config key empty
	t.Setenv("LINEAR_API_KEY", "env-api-key")

	b, err := New("/tmp", "team-id", "project-id", nil, "")
	if err != nil {
		t.Errorf("New() with env API key should not error: %v", err)
	}
	if b.apiKey != "env-api-key" {
		t.Errorf("apiKey = %q, want %q", b.apiKey, "env-api-key")
	}
}

func TestNew_APIKeyConfigTakesPrecedence(t *testing.T) {
	// Set both config and env - config should take precedence
	t.Setenv("LINEAR_API_KEY", "env-api-key")

	b, err := New("/tmp", "team-id", "project-id", nil, "config-api-key")
	if err != nil {
		t.Errorf("New() should not error: %v", err)
	}
	if b.apiKey != "config-api-key" {
		t.Errorf("apiKey = %q, want %q (config should take precedence)", b.apiKey, "config-api-key")
	}
}

// TestBackendImplementsCollaborativeBackend verifies that the Backend type
// correctly implements the CollaborativeBackend interface.
func TestBackendImplementsCollaborativeBackend(t *testing.T) {
	// This test ensures the Backend type satisfies the CollaborativeBackend interface.
	// If Backend doesn't implement the interface, this will fail to compile.
	var _ issue.CollaborativeBackend = (*Backend)(nil)
}
