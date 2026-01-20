package supervisor

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/config"
	"github.com/tessro/fab/internal/issue"
	"github.com/tessro/fab/internal/logging"
	"github.com/tessro/fab/internal/orchestrator"
	"github.com/tessro/fab/internal/project"
	"github.com/tessro/fab/internal/runtime"
)

// DefaultCommentPollInterval is the default interval for polling comments.
// This is set to 10 seconds to balance responsiveness with API rate limits.
const DefaultCommentPollInterval = 10 * time.Second

// CommentPollerConfig configures the comment poller.
type CommentPollerConfig struct {
	// PollInterval is how often to poll for new comments.
	PollInterval time.Duration

	// GetOrchestrators returns the map of active orchestrators.
	GetOrchestrators func() map[string]*orchestrator.Orchestrator

	// GetAgent returns an agent by ID.
	GetAgent func(id string) (*agent.Agent, error)

	// GetProject returns a project by name.
	GetProject func(name string) (*project.Project, error)

	// GlobalConfig for creating issue backends.
	GlobalConfig *config.GlobalConfig
}

// CommentPoller polls issue trackers for new comments and delivers them to agents.
type CommentPoller struct {
	config CommentPollerConfig
	dedup  *runtime.DedupStore

	stopCh chan struct{}
	doneCh chan struct{}
	mu     sync.Mutex
	// +checklocks:mu
	running bool

	// Track when we started polling each claimed issue to filter old comments
	// Map of "project:issueID" -> time when the claim was first seen
	claimStartTimes map[string]time.Time
	claimMu         sync.Mutex
}

// NewCommentPoller creates a new comment poller.
func NewCommentPoller(cfg CommentPollerConfig, dedup *runtime.DedupStore) *CommentPoller {
	if cfg.PollInterval == 0 {
		cfg.PollInterval = DefaultCommentPollInterval
	}
	return &CommentPoller{
		config:          cfg,
		dedup:           dedup,
		claimStartTimes: make(map[string]time.Time),
	}
}

// Start begins the comment polling loop.
func (p *CommentPoller) Start() error {
	p.mu.Lock()
	if p.running {
		p.mu.Unlock()
		return errors.New("comment poller already running")
	}
	p.stopCh = make(chan struct{})
	p.doneCh = make(chan struct{})
	p.running = true
	p.mu.Unlock()

	go p.run()
	slog.Info("comment poller started", "interval", p.config.PollInterval)
	return nil
}

// Stop stops the comment polling loop.
func (p *CommentPoller) Stop() {
	p.mu.Lock()
	if !p.running {
		p.mu.Unlock()
		return
	}
	close(p.stopCh)
	p.running = false
	p.mu.Unlock()

	<-p.doneCh
	slog.Info("comment poller stopped")
}

// IsRunning returns whether the poller is currently running.
func (p *CommentPoller) IsRunning() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.running
}

// run is the main polling loop.
func (p *CommentPoller) run() {
	defer logging.LogPanic("comment-poller", nil)
	defer close(p.doneCh)

	ticker := time.NewTicker(p.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.pollAllProjects()
		}
	}
}

// pollAllProjects polls for comments across all active projects.
func (p *CommentPoller) pollAllProjects() {
	orchestrators := p.config.GetOrchestrators()
	if orchestrators == nil {
		return
	}

	// Collect all active claims to clean up stale entries
	activeClaims := make(map[string]bool)

	for projectName, orch := range orchestrators {
		if !orch.IsRunning() {
			continue
		}

		proj, err := p.config.GetProject(projectName)
		if err != nil {
			slog.Debug("failed to get project for comment polling",
				"project", projectName,
				"error", err,
			)
			continue
		}

		// Track active claims for cleanup
		if claims := orch.Claims(); claims != nil {
			for issueID := range claims.List() {
				activeClaims[fmt.Sprintf("%s:%s", projectName, issueID)] = true
			}
		}

		p.pollProjectComments(proj, orch)
	}

	// Clean up stale claim start times for released claims
	p.cleanupStaleClaimTimes(activeClaims)
}

// cleanupStaleClaimTimes removes claim start times for claims that are no longer active.
func (p *CommentPoller) cleanupStaleClaimTimes(activeClaims map[string]bool) {
	p.claimMu.Lock()
	defer p.claimMu.Unlock()

	for claimKey := range p.claimStartTimes {
		if !activeClaims[claimKey] {
			delete(p.claimStartTimes, claimKey)
		}
	}
}

// pollProjectComments polls for new comments on claimed issues in a project.
func (p *CommentPoller) pollProjectComments(proj *project.Project, orch *orchestrator.Orchestrator) {
	claims := orch.Claims()
	if claims == nil {
		return
	}

	// Get all active claims for this project
	activeClaims := claims.List()
	if len(activeClaims) == 0 {
		return
	}

	// Create issue backend for this project
	backend, err := p.createBackend(proj)
	if err != nil {
		slog.Debug("failed to create issue backend for comment polling",
			"project", proj.Name,
			"error", err,
		)
		return
	}

	// Check if backend supports listing comments
	collabBackend, ok := backend.(issue.CollaborativeBackend)
	if !ok {
		return // Backend doesn't support comments
	}

	// Poll comments for each claimed issue
	for issueID, agentID := range activeClaims {
		p.pollIssueComments(proj.Name, issueID, agentID, collabBackend)
	}
}

// pollIssueComments fetches and delivers new comments for a single issue.
func (p *CommentPoller) pollIssueComments(projectName, issueID, agentID string, backend issue.CollaborativeBackend) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Get or set the claim start time (first time we saw this claim)
	claimKey := fmt.Sprintf("%s:%s", projectName, issueID)
	p.claimMu.Lock()
	startTime, exists := p.claimStartTimes[claimKey]
	if !exists {
		startTime = time.Now()
		p.claimStartTimes[claimKey] = startTime
	}
	p.claimMu.Unlock()

	// Fetch comments since the claim started
	comments, err := backend.ListComments(ctx, issueID, startTime)
	if err != nil {
		if errors.Is(err, issue.ErrNotSupported) {
			return // Backend doesn't support listing comments
		}
		slog.Debug("failed to list comments",
			"project", projectName,
			"issue", issueID,
			"error", err,
		)
		return
	}

	// Process each comment
	for _, comment := range comments {
		p.deliverComment(projectName, issueID, agentID, comment)
	}
}

// deliverComment sends a comment to the appropriate agent.
func (p *CommentPoller) deliverComment(projectName, issueID, agentID string, comment *issue.Comment) {
	// Build dedup ID
	dedupID := fmt.Sprintf("comment:%s:%s:%s", projectName, issueID, comment.ID)

	// Check if we've already delivered this comment
	if !p.dedup.Mark(dedupID, projectName) {
		return // Already delivered
	}

	// Get the agent
	a, err := p.config.GetAgent(agentID)
	if err != nil {
		slog.Debug("failed to get agent for comment delivery",
			"agent", agentID,
			"error", err,
		)
		return
	}

	// Format and send the comment
	msg := fmt.Sprintf("New comment on issue #%s from %s:\n\n%s",
		issueID, comment.Author, comment.Body)

	if err := a.SendMessage(msg); err != nil {
		slog.Warn("failed to send comment to agent",
			"agent", agentID,
			"issue", issueID,
			"error", err,
		)
	} else {
		slog.Info("delivered comment to agent",
			"agent", agentID,
			"project", projectName,
			"issue", issueID,
			"author", comment.Author,
		)
	}
}

// createBackend creates an issue backend for the given project.
func (p *CommentPoller) createBackend(proj *project.Project) (issue.Backend, error) {
	factory := issueBackendFactoryForProject(proj, p.config.GlobalConfig)
	return factory(proj.RepoDir())
}

// ClearClaimTime removes the claim start time for an issue (called when claim is released).
func (p *CommentPoller) ClearClaimTime(projectName, issueID string) {
	claimKey := fmt.Sprintf("%s:%s", projectName, issueID)
	p.claimMu.Lock()
	delete(p.claimStartTimes, claimKey)
	p.claimMu.Unlock()
}
