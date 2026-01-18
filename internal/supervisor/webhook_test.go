package supervisor

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/tessro/fab/internal/runtime"
)

func TestIssueEvent_DedupID(t *testing.T) {
	tests := []struct {
		name     string
		event    IssueEvent
		expected string
	}{
		{
			name: "comment event",
			event: IssueEvent{
				Type:      EventIssueComment,
				Source:    "github",
				IssueID:   "123",
				CommentID: "456",
			},
			expected: "github:123:456",
		},
		{
			name: "created event",
			event: IssueEvent{
				Type:      EventIssueCreated,
				Source:    "linear",
				IssueID:   "ABC-123",
				Timestamp: time.Unix(1234567890, 0),
			},
			expected: "linear:ABC-123:created:1234567890000000000",
		},
		{
			name: "updated event",
			event: IssueEvent{
				Type:      EventIssueUpdated,
				Source:    "github",
				IssueID:   "789",
				Timestamp: time.Unix(1234567891, 0),
			},
			expected: "github:789:updated:1234567891000000000",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.event.DedupID()
			if got != tt.expected {
				t.Errorf("DedupID() = %q, want %q", got, tt.expected)
			}
		})
	}
}

func TestWebhookServer_ParseGitHubIssueComment(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{}, dedup, events)

	payload := `{
		"action": "created",
		"issue": {"number": 123},
		"comment": {
			"id": 456,
			"body": "Test comment",
			"created_at": "2024-01-15T10:00:00Z",
			"user": {"login": "testuser"}
		}
	}`

	event, err := srv.parseGitHubEvent("issue_comment", []byte(payload), "test-project")
	if err != nil {
		t.Fatalf("parseGitHubEvent failed: %v", err)
	}

	if event.Type != EventIssueComment {
		t.Errorf("Type = %q, want %q", event.Type, EventIssueComment)
	}
	if event.Source != "github" {
		t.Errorf("Source = %q, want %q", event.Source, "github")
	}
	if event.Project != "test-project" {
		t.Errorf("Project = %q, want %q", event.Project, "test-project")
	}
	if event.IssueID != "123" {
		t.Errorf("IssueID = %q, want %q", event.IssueID, "123")
	}
	if event.CommentID != "456" {
		t.Errorf("CommentID = %q, want %q", event.CommentID, "456")
	}
	if event.Author != "testuser" {
		t.Errorf("Author = %q, want %q", event.Author, "testuser")
	}
	if event.Body != "Test comment" {
		t.Errorf("Body = %q, want %q", event.Body, "Test comment")
	}
}

func TestWebhookServer_ParseGitHubIssueComment_IgnoreEdited(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{}, dedup, events)

	payload := `{
		"action": "edited",
		"issue": {"number": 123},
		"comment": {
			"id": 456,
			"body": "Edited comment"
		}
	}`

	_, err := srv.parseGitHubEvent("issue_comment", []byte(payload), "test-project")
	if err != errIgnoredEvent {
		t.Errorf("expected errIgnoredEvent for edited action, got %v", err)
	}
}

func TestWebhookServer_ParseGitHubIssue(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{}, dedup, events)

	payload := `{
		"action": "opened",
		"issue": {
			"number": 789,
			"title": "Test Issue",
			"body": "Issue description",
			"created_at": "2024-01-15T10:00:00Z",
			"updated_at": "2024-01-15T10:00:00Z",
			"user": {"login": "testuser"}
		}
	}`

	event, err := srv.parseGitHubEvent("issues", []byte(payload), "test-project")
	if err != nil {
		t.Fatalf("parseGitHubEvent failed: %v", err)
	}

	if event.Type != EventIssueCreated {
		t.Errorf("Type = %q, want %q", event.Type, EventIssueCreated)
	}
	if event.Title != "Test Issue" {
		t.Errorf("Title = %q, want %q", event.Title, "Test Issue")
	}
	if event.Body != "Issue description" {
		t.Errorf("Body = %q, want %q", event.Body, "Issue description")
	}
}

func TestWebhookServer_ParseLinearComment(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{}, dedup, events)

	payload := `{
		"action": "create",
		"type": "Comment",
		"data": {
			"id": "comment-id-123",
			"body": "Linear comment",
			"createdAt": "2024-01-15T10:00:00Z",
			"user": {"name": "Test User"},
			"issue": {
				"id": "issue-id",
				"identifier": "PROJ-123",
				"title": "Issue Title"
			}
		}
	}`

	event, err := srv.parseLinearEvent([]byte(payload), "test-project")
	if err != nil {
		t.Fatalf("parseLinearEvent failed: %v", err)
	}

	if event.Type != EventIssueComment {
		t.Errorf("Type = %q, want %q", event.Type, EventIssueComment)
	}
	if event.Source != "linear" {
		t.Errorf("Source = %q, want %q", event.Source, "linear")
	}
	if event.IssueID != "PROJ-123" {
		t.Errorf("IssueID = %q, want %q", event.IssueID, "PROJ-123")
	}
	if event.CommentID != "comment-id-123" {
		t.Errorf("CommentID = %q, want %q", event.CommentID, "comment-id-123")
	}
	if event.Author != "Test User" {
		t.Errorf("Author = %q, want %q", event.Author, "Test User")
	}
	if event.Body != "Linear comment" {
		t.Errorf("Body = %q, want %q", event.Body, "Linear comment")
	}
}

func TestWebhookServer_ValidateGitHubSignature(t *testing.T) {
	secret := "test-secret"
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{Secret: secret}, dedup, events)

	body := []byte(`{"test": "payload"}`)

	// Compute valid signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validSig := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !srv.validateGitHubSignature(body, validSig) {
		t.Error("valid signature should pass validation")
	}

	if srv.validateGitHubSignature(body, "sha256=invalid") {
		t.Error("invalid signature should fail validation")
	}

	if srv.validateGitHubSignature(body, "invalid-format") {
		t.Error("missing sha256= prefix should fail validation")
	}
}

func TestWebhookServer_ValidateLinearSignature(t *testing.T) {
	secret := "test-secret"
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{Secret: secret}, dedup, events)

	body := []byte(`{"test": "payload"}`)

	// Compute valid signature
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	validSig := hex.EncodeToString(mac.Sum(nil))

	if !srv.validateLinearSignature(body, validSig) {
		t.Error("valid signature should pass validation")
	}

	if srv.validateLinearSignature(body, "invalid") {
		t.Error("invalid signature should fail validation")
	}

	if srv.validateLinearSignature(body, "") {
		t.Error("empty signature should fail validation")
	}
}

func TestWebhookServer_HandleGitHub(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{PathPrefix: "/webhooks"}, dedup, events)

	payload := map[string]interface{}{
		"action": "created",
		"issue":  map[string]interface{}{"number": 123},
		"comment": map[string]interface{}{
			"id":         456,
			"body":       "Test comment",
			"created_at": "2024-01-15T10:00:00Z",
			"user":       map[string]interface{}{"login": "testuser"},
		},
	}
	body, _ := json.Marshal(payload)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github?project=test-project", bytes.NewReader(body))
	req.Header.Set("X-GitHub-Event", "issue_comment")

	w := httptest.NewRecorder()
	srv.handleGitHub(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Check event was sent to channel
	select {
	case event := <-events:
		if event.Type != EventIssueComment {
			t.Errorf("expected comment event, got %v", event.Type)
		}
		if event.Project != "test-project" {
			t.Errorf("expected project test-project, got %s", event.Project)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event to be sent to channel")
	}
}

func TestWebhookServer_HandleGitHub_MissingProject(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{}, dedup, events)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", bytes.NewReader([]byte(`{}`)))
	req.Header.Set("X-GitHub-Event", "issue_comment")

	w := httptest.NewRecorder()
	srv.handleGitHub(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", w.Code)
	}
}

func TestWebhookServer_HandleGeneric(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{}, dedup, events)

	event := IssueEvent{
		Type:      EventIssueComment,
		Source:    "custom",
		Project:   "my-project",
		IssueID:   "ISSUE-1",
		CommentID: "comment-1",
		Author:    "user",
		Body:      "Comment body",
	}
	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/generic", bytes.NewReader(body))

	w := httptest.NewRecorder()
	srv.handleGeneric(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}

	// Check event was sent to channel
	select {
	case received := <-events:
		if received.Type != EventIssueComment {
			t.Errorf("expected comment event, got %v", received.Type)
		}
		if received.Project != "my-project" {
			t.Errorf("expected project my-project, got %s", received.Project)
		}
		if received.IssueID != "ISSUE-1" {
			t.Errorf("expected issue ISSUE-1, got %s", received.IssueID)
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("expected event to be sent to channel")
	}
}

func TestWebhookServer_HandleGeneric_MissingProject(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{}, dedup, events)

	event := IssueEvent{
		IssueID: "ISSUE-1",
	}
	body, _ := json.Marshal(event)

	req := httptest.NewRequest(http.MethodPost, "/webhooks/generic", bytes.NewReader(body))

	w := httptest.NewRecorder()
	srv.handleGeneric(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected status 400 for missing project, got %d", w.Code)
	}
}

func TestWebhookServer_ProcessEvent_Dedup(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{}, dedup, events)

	event := &IssueEvent{
		Type:      EventIssueComment,
		Source:    "github",
		Project:   "test",
		IssueID:   "123",
		CommentID: "456",
	}

	// First processing should succeed
	srv.processEvent(event)

	select {
	case <-events:
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Error("first event should be sent to channel")
	}

	// Second processing of same event should be deduplicated
	srv.processEvent(event)

	select {
	case <-events:
		t.Error("duplicate event should not be sent to channel")
	case <-time.After(50 * time.Millisecond):
		// Expected - no event should be sent
	}
}

func TestWebhookServer_MethodNotAllowed(t *testing.T) {
	dedup := runtime.NewDedupStore("")
	events := make(chan *IssueEvent, 10)
	srv := NewWebhookServer(WebhookConfig{}, dedup, events)

	handlers := []struct {
		name    string
		handler http.HandlerFunc
	}{
		{"github", srv.handleGitHub},
		{"linear", srv.handleLinear},
		{"generic", srv.handleGeneric},
	}

	for _, h := range handlers {
		t.Run(h.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			w := httptest.NewRecorder()
			h.handler(w, req)

			if w.Code != http.StatusMethodNotAllowed {
				t.Errorf("expected status 405, got %d", w.Code)
			}
		})
	}
}
