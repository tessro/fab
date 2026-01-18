package supervisor

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/tessro/fab/internal/logging"
	"github.com/tessro/fab/internal/runtime"
)

// IssueEventType represents the type of issue event.
type IssueEventType string

const (
	// EventIssueComment represents a comment added to an issue.
	EventIssueComment IssueEventType = "comment"
	// EventIssueCreated represents a new issue being created.
	EventIssueCreated IssueEventType = "created"
	// EventIssueUpdated represents an issue being updated.
	EventIssueUpdated IssueEventType = "updated"
)

// IssueEvent represents an event from an issue tracker.
type IssueEvent struct {
	// Type is the kind of event (comment, created, updated).
	Type IssueEventType `json:"type"`

	// Source identifies the issue tracker (github, linear, tk).
	Source string `json:"source"`

	// Project is the fab project name this event belongs to.
	Project string `json:"project"`

	// IssueID is the issue identifier in the source system.
	IssueID string `json:"issue_id"`

	// CommentID is the unique identifier for comments (used for dedup).
	// Empty for non-comment events.
	CommentID string `json:"comment_id,omitempty"`

	// Author is the username or identifier of the event author.
	Author string `json:"author,omitempty"`

	// Body is the content of the comment or issue.
	Body string `json:"body,omitempty"`

	// Title is the issue title (for created/updated events).
	Title string `json:"title,omitempty"`

	// Timestamp is when the event occurred.
	Timestamp time.Time `json:"timestamp"`
}

// DedupID returns the unique identifier used for deduplication.
// For comments, this is the comment ID; for other events, it combines
// the event type, issue ID, and timestamp.
func (e *IssueEvent) DedupID() string {
	if e.CommentID != "" {
		return fmt.Sprintf("%s:%s:%s", e.Source, e.IssueID, e.CommentID)
	}
	return fmt.Sprintf("%s:%s:%s:%d", e.Source, e.IssueID, e.Type, e.Timestamp.UnixNano())
}

// WebhookConfig configures the webhook HTTP server.
type WebhookConfig struct {
	// Enabled determines whether the webhook server is started.
	Enabled bool

	// BindAddr is the address to bind the HTTP server to.
	// Default: ":8080"
	BindAddr string

	// Secret is the webhook secret for validating signatures.
	// If empty, signature validation is skipped (not recommended for production).
	Secret string

	// PathPrefix is the URL path prefix for webhook endpoints.
	// Default: "/webhooks"
	PathPrefix string
}

// MaxWebhookBodySize is the maximum size of a webhook request body (10MB).
const MaxWebhookBodySize = 10 << 20

// DefaultWebhookConfig returns the default webhook configuration.
func DefaultWebhookConfig() WebhookConfig {
	return WebhookConfig{
		Enabled:    false,
		BindAddr:   ":8080",
		PathPrefix: "/webhooks",
	}
}

// WebhookServer handles incoming webhook events from issue trackers.
type WebhookServer struct {
	config   WebhookConfig
	dedup    *runtime.DedupStore
	eventsCh chan<- *IssueEvent
	srv      *http.Server
	mu       sync.Mutex
	// +checklocks:mu
	running bool
	doneCh  chan struct{}
}

// NewWebhookServer creates a new webhook server.
// Events are sent to the provided channel for processing.
func NewWebhookServer(cfg WebhookConfig, dedup *runtime.DedupStore, eventsCh chan<- *IssueEvent) *WebhookServer {
	return &WebhookServer{
		config:   cfg,
		dedup:    dedup,
		eventsCh: eventsCh,
	}
}

// Start begins listening for webhook events.
func (w *WebhookServer) Start() error {
	w.mu.Lock()
	if w.running {
		w.mu.Unlock()
		return errors.New("webhook server already running")
	}

	if !w.config.Enabled {
		w.mu.Unlock()
		return nil
	}

	w.doneCh = make(chan struct{})
	w.running = true
	w.mu.Unlock()

	mux := http.NewServeMux()
	prefix := w.config.PathPrefix
	if prefix == "" {
		prefix = "/webhooks"
	}

	// Register webhook endpoints
	mux.HandleFunc(prefix+"/github", w.handleGitHub)
	mux.HandleFunc(prefix+"/linear", w.handleLinear)
	mux.HandleFunc(prefix+"/generic", w.handleGeneric)

	// Health check endpoint
	mux.HandleFunc("/health", func(rw http.ResponseWriter, _ *http.Request) {
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte("ok"))
	})

	w.srv = &http.Server{
		Addr:              w.config.BindAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	// Start listener
	ln, err := net.Listen("tcp", w.config.BindAddr)
	if err != nil {
		w.mu.Lock()
		w.running = false
		w.mu.Unlock()
		return fmt.Errorf("listen on %s: %w", w.config.BindAddr, err)
	}

	slog.Info("webhook server started", "addr", w.config.BindAddr)

	go func() {
		defer logging.LogPanic("webhook-server", nil)
		defer close(w.doneCh)

		if err := w.srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("webhook server error", "error", err)
		}
	}()

	return nil
}

// Stop gracefully shuts down the webhook server.
func (w *WebhookServer) Stop() error {
	w.mu.Lock()
	if !w.running {
		w.mu.Unlock()
		return nil
	}
	w.running = false
	w.mu.Unlock()

	if w.srv == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := w.srv.Shutdown(ctx); err != nil {
		return fmt.Errorf("shutdown webhook server: %w", err)
	}

	// Wait for serve goroutine to exit
	<-w.doneCh

	slog.Info("webhook server stopped")
	return nil
}

// IsRunning returns true if the webhook server is running.
func (w *WebhookServer) IsRunning() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.running
}

// handleGitHub processes GitHub webhook events.
func (w *WebhookServer) handleGitHub(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body for signature validation
	body, err := io.ReadAll(io.LimitReader(r.Body, MaxWebhookBodySize)) // 10MB limit
	if err != nil {
		http.Error(rw, "failed to read body", http.StatusBadRequest)
		return
	}

	// Validate signature if secret is configured
	if w.config.Secret != "" {
		sig := r.Header.Get("X-Hub-Signature-256")
		if !w.validateGitHubSignature(body, sig) {
			slog.Warn("invalid GitHub webhook signature")
			http.Error(rw, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Parse event type
	eventType := r.Header.Get("X-GitHub-Event")
	if eventType == "" {
		http.Error(rw, "missing X-GitHub-Event header", http.StatusBadRequest)
		return
	}

	// Get project from query param or header
	project := r.URL.Query().Get("project")
	if project == "" {
		project = r.Header.Get("X-Fab-Project")
	}
	if project == "" {
		http.Error(rw, "missing project parameter", http.StatusBadRequest)
		return
	}

	event, err := w.parseGitHubEvent(eventType, body, project)
	if err != nil {
		if errors.Is(err, errIgnoredEvent) {
			// Event type we don't care about
			rw.WriteHeader(http.StatusOK)
			return
		}
		slog.Warn("failed to parse GitHub event", "error", err)
		http.Error(rw, "failed to parse event", http.StatusBadRequest)
		return
	}

	w.processEvent(event)
	rw.WriteHeader(http.StatusOK)
}

// errIgnoredEvent is returned when an event should be silently ignored.
var errIgnoredEvent = errors.New("ignored event")

// parseGitHubEvent parses a GitHub webhook payload into an IssueEvent.
func (w *WebhookServer) parseGitHubEvent(eventType string, body []byte, project string) (*IssueEvent, error) {
	switch eventType {
	case "issue_comment":
		return w.parseGitHubIssueComment(body, project)
	case "issues":
		return w.parseGitHubIssue(body, project)
	default:
		return nil, errIgnoredEvent
	}
}

// parseGitHubIssueComment parses a GitHub issue_comment event.
func (w *WebhookServer) parseGitHubIssueComment(body []byte, project string) (*IssueEvent, error) {
	var payload struct {
		Action  string `json:"action"`
		Issue   struct {
			Number int `json:"number"`
		} `json:"issue"`
		Comment struct {
			ID        int64  `json:"id"`
			Body      string `json:"body"`
			CreatedAt string `json:"created_at"`
			User      struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"comment"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse issue_comment payload: %w", err)
	}

	// Only process created comments
	if payload.Action != "created" {
		return nil, errIgnoredEvent
	}

	timestamp, _ := time.Parse(time.RFC3339, payload.Comment.CreatedAt)
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	return &IssueEvent{
		Type:      EventIssueComment,
		Source:    "github",
		Project:   project,
		IssueID:   fmt.Sprintf("%d", payload.Issue.Number),
		CommentID: fmt.Sprintf("%d", payload.Comment.ID),
		Author:    payload.Comment.User.Login,
		Body:      payload.Comment.Body,
		Timestamp: timestamp,
	}, nil
}

// parseGitHubIssue parses a GitHub issues event.
func (w *WebhookServer) parseGitHubIssue(body []byte, project string) (*IssueEvent, error) {
	var payload struct {
		Action string `json:"action"`
		Issue  struct {
			Number    int    `json:"number"`
			Title     string `json:"title"`
			Body      string `json:"body"`
			CreatedAt string `json:"created_at"`
			UpdatedAt string `json:"updated_at"`
			User      struct {
				Login string `json:"login"`
			} `json:"user"`
		} `json:"issue"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse issues payload: %w", err)
	}

	var eventType IssueEventType
	switch payload.Action {
	case "opened":
		eventType = EventIssueCreated
	case "edited":
		eventType = EventIssueUpdated
	default:
		return nil, errIgnoredEvent
	}

	timestampStr := payload.Issue.UpdatedAt
	if eventType == EventIssueCreated {
		timestampStr = payload.Issue.CreatedAt
	}
	timestamp, _ := time.Parse(time.RFC3339, timestampStr)
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	return &IssueEvent{
		Type:      eventType,
		Source:    "github",
		Project:   project,
		IssueID:   fmt.Sprintf("%d", payload.Issue.Number),
		Author:    payload.Issue.User.Login,
		Title:     payload.Issue.Title,
		Body:      payload.Issue.Body,
		Timestamp: timestamp,
	}, nil
}

// validateGitHubSignature validates the X-Hub-Signature-256 header.
func (w *WebhookServer) validateGitHubSignature(body []byte, sig string) bool {
	if !strings.HasPrefix(sig, "sha256=") {
		return false
	}
	sig = strings.TrimPrefix(sig, "sha256=")

	mac := hmac.New(sha256.New, []byte(w.config.Secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}

// handleLinear processes Linear webhook events.
func (w *WebhookServer) handleLinear(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, MaxWebhookBodySize))
	if err != nil {
		http.Error(rw, "failed to read body", http.StatusBadRequest)
		return
	}

	// Validate Linear webhook signature
	if w.config.Secret != "" {
		sig := r.Header.Get("Linear-Signature")
		if !w.validateLinearSignature(body, sig) {
			slog.Warn("invalid Linear webhook signature")
			http.Error(rw, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Get project from query param or header
	project := r.URL.Query().Get("project")
	if project == "" {
		project = r.Header.Get("X-Fab-Project")
	}
	if project == "" {
		http.Error(rw, "missing project parameter", http.StatusBadRequest)
		return
	}

	event, err := w.parseLinearEvent(body, project)
	if err != nil {
		if errors.Is(err, errIgnoredEvent) {
			rw.WriteHeader(http.StatusOK)
			return
		}
		slog.Warn("failed to parse Linear event", "error", err)
		http.Error(rw, "failed to parse event", http.StatusBadRequest)
		return
	}

	w.processEvent(event)
	rw.WriteHeader(http.StatusOK)
}

// parseLinearEvent parses a Linear webhook payload.
func (w *WebhookServer) parseLinearEvent(body []byte, project string) (*IssueEvent, error) {
	var payload struct {
		Action string `json:"action"`
		Type   string `json:"type"`
		Data   struct {
			ID        string `json:"id"`
			Body      string `json:"body"`
			CreatedAt string `json:"createdAt"`
			UpdatedAt string `json:"updatedAt"`
			User      struct {
				Name string `json:"name"`
			} `json:"user"`
			Issue struct {
				ID         string `json:"id"`
				Identifier string `json:"identifier"`
				Title      string `json:"title"`
			} `json:"issue"`
			// For issue events
			Identifier string `json:"identifier"`
			Title      string `json:"title"`
		} `json:"data"`
	}

	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("parse Linear payload: %w", err)
	}

	switch payload.Type {
	case "Comment":
		if payload.Action != "create" {
			return nil, errIgnoredEvent
		}
		timestamp, _ := time.Parse(time.RFC3339, payload.Data.CreatedAt)
		if timestamp.IsZero() {
			timestamp = time.Now()
		}
		return &IssueEvent{
			Type:      EventIssueComment,
			Source:    "linear",
			Project:   project,
			IssueID:   payload.Data.Issue.Identifier,
			CommentID: payload.Data.ID,
			Author:    payload.Data.User.Name,
			Body:      payload.Data.Body,
			Timestamp: timestamp,
		}, nil

	case "Issue":
		var eventType IssueEventType
		switch payload.Action {
		case "create":
			eventType = EventIssueCreated
		case "update":
			eventType = EventIssueUpdated
		default:
			return nil, errIgnoredEvent
		}
		timestampStr := payload.Data.UpdatedAt
		if eventType == EventIssueCreated {
			timestampStr = payload.Data.CreatedAt
		}
		timestamp, _ := time.Parse(time.RFC3339, timestampStr)
		if timestamp.IsZero() {
			timestamp = time.Now()
		}
		return &IssueEvent{
			Type:      eventType,
			Source:    "linear",
			Project:   project,
			IssueID:   payload.Data.Identifier,
			Author:    payload.Data.User.Name,
			Title:     payload.Data.Title,
			Body:      payload.Data.Body,
			Timestamp: timestamp,
		}, nil

	default:
		return nil, errIgnoredEvent
	}
}

// validateLinearSignature validates the Linear-Signature header.
func (w *WebhookServer) validateLinearSignature(body []byte, sig string) bool {
	if sig == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(w.config.Secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}

// handleGeneric processes generic webhook events.
// This endpoint accepts a simple JSON payload for custom integrations.
func (w *WebhookServer) handleGeneric(rw http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(rw, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, MaxWebhookBodySize))
	if err != nil {
		http.Error(rw, "failed to read body", http.StatusBadRequest)
		return
	}

	// Validate signature if configured
	if w.config.Secret != "" {
		sig := r.Header.Get("X-Webhook-Signature")
		if !w.validateGenericSignature(body, sig) {
			slog.Warn("invalid generic webhook signature")
			http.Error(rw, "invalid signature", http.StatusUnauthorized)
			return
		}
	}

	var event IssueEvent
	if err := json.Unmarshal(body, &event); err != nil {
		http.Error(rw, "invalid JSON payload", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if event.Project == "" {
		http.Error(rw, "missing project field", http.StatusBadRequest)
		return
	}
	if event.IssueID == "" {
		http.Error(rw, "missing issue_id field", http.StatusBadRequest)
		return
	}
	if event.Type == "" {
		event.Type = EventIssueComment
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}
	if event.Source == "" {
		event.Source = "generic"
	}

	w.processEvent(&event)
	rw.WriteHeader(http.StatusOK)
}

// validateGenericSignature validates the X-Webhook-Signature header.
func (w *WebhookServer) validateGenericSignature(body []byte, sig string) bool {
	if sig == "" {
		return false
	}

	mac := hmac.New(sha256.New, []byte(w.config.Secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))

	return hmac.Equal([]byte(sig), []byte(expected))
}

// processEvent handles deduplication and routing of events.
func (w *WebhookServer) processEvent(event *IssueEvent) {
	dedupID := event.DedupID()

	// Check if we've already processed this event
	if !w.dedup.Mark(dedupID, event.Project) {
		slog.Debug("duplicate event ignored", "dedup_id", dedupID)
		return
	}

	slog.Info("webhook event received",
		"type", event.Type,
		"source", event.Source,
		"project", event.Project,
		"issue", event.IssueID,
		"author", event.Author,
	)

	// Send to event channel for processing by supervisor
	select {
	case w.eventsCh <- event:
	default:
		slog.Warn("event channel full, dropping event", "dedup_id", dedupID)
	}
}
