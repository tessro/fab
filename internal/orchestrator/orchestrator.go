// Package orchestrator manages the automatic agent lifecycle for projects.
package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"log/slog"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/issue"
	"github.com/tessro/fab/internal/logging"
	"github.com/tessro/fab/internal/project"
)

// Default polling interval for checking ready issues.
const DefaultPollInterval = 10 * time.Second

// Config configures orchestrator behavior.
type Config struct {
	// DefaultAgentMode is propagated to new agents.
	// ModeManual (default) stages actions for user confirmation.
	// ModeAuto executes actions immediately.
	DefaultAgentMode agent.Mode

	// KickstartPrompt is sent to agents when they start.
	KickstartPrompt string

	// InterventionSilence is the duration of user silence before resuming kickstart.
	// When a user sends a message to an agent, kickstart is paused until this duration
	// of silence passes. Zero disables intervention detection.
	InterventionSilence time.Duration

	// OnAgentStarted is called after an agent's process is started.
	// Use this to set up output reading/broadcasting.
	OnAgentStarted func(*agent.Agent)

	// IssueBackendFactory creates an issue backend for checking ready issues.
	// If nil, auto-spawning of agents is disabled.
	IssueBackendFactory issue.NewBackendFunc

	// PollInterval is how often to check for ready issues.
	// Defaults to DefaultPollInterval.
	PollInterval time.Duration
}

// DefaultConfig returns the default orchestrator configuration.
func DefaultConfig() Config {
	return Config{
		DefaultAgentMode:    agent.DefaultMode,
		InterventionSilence: agent.DefaultInterventionSilence,
		KickstartPrompt: `The 'fab' command is available on PATH - use 'fab', not './fab'.

Run 'fab issue ready' to find available tasks.
Pick one and run 'fab agent claim <id>' to claim it.
If already claimed, pick another from the list.
If all tasks are claimed, run 'fab agent done' to finish your session.
After claiming a task, run 'fab agent describe "<brief description>"' to set your status (e.g., "Implementing user auth feature").
When done:
1. Run all quality gates
2. Run /review to perform a thorough code review of your changes
3. IMPORTANT: You MUST address ALL issues found during code review before proceeding. Do not skip or ignore review feedback. Re-run /review if needed to confirm fixes.
4. Commit all your changes with a descriptive message (include "Closes #<id>" in the commit body to link the commit to the task)
5. Run 'fab issue close <id>' to close the task
6. Run 'fab agent done'
IMPORTANT: Do NOT run 'git push' - merging and pushing happens automatically when you run 'fab agent done'.`,
	}
}

// Orchestrator manages the automatic agent lifecycle for a single project.
type Orchestrator struct {
	project *project.Project
	agents  *agent.Manager
	config  Config // Set at construction, effectively immutable during operation

	// Action queue for manual mode
	actions *ActionQueue

	// Ticket claim registry to prevent duplicate work
	claims *ClaimRegistry

	// Commit log tracks successfully merged commits
	commits *CommitLog

	// Lifecycle management (channels are goroutine-safe: created in Start, closed to signal)
	stopCh chan struct{}
	doneCh chan struct{}
	mu     sync.RWMutex

	// +checklocks:mu
	running bool
}

// New creates a new Orchestrator for the given project.
func New(proj *project.Project, agents *agent.Manager, cfg Config) *Orchestrator {
	return &Orchestrator{
		project: proj,
		config:  cfg,
		agents:  agents,
		actions: NewActionQueue(),
		claims:  NewClaimRegistry(),
		commits: NewCommitLog(DefaultCommitLogSize),
	}
}

// Claims returns the ticket claim registry.
func (o *Orchestrator) Claims() *ClaimRegistry {
	return o.claims
}

// Commits returns the commit log.
func (o *Orchestrator) Commits() *CommitLog {
	return o.commits
}

// Project returns the orchestrator's project.
func (o *Orchestrator) Project() *project.Project {
	return o.project
}

// Config returns the orchestrator's configuration.
func (o *Orchestrator) Config() Config {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.config
}

// SetConfig updates the orchestrator's configuration.
func (o *Orchestrator) SetConfig(cfg Config) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.config = cfg
}

// IsRunning returns true if the orchestrator is running.
func (o *Orchestrator) IsRunning() bool {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.running
}

// Actions returns the action queue for manual mode operations.
func (o *Orchestrator) Actions() *ActionQueue {
	return o.actions
}

// IsAgentIntervening returns true if the user is currently intervening with the given agent.
// This checks the agent's last user input against the orchestrator's intervention silence threshold.
func (o *Orchestrator) IsAgentIntervening(agentID string) bool {
	a, err := o.agents.Get(agentID)
	if err != nil {
		return false
	}
	return o.config.InterventionSilence > 0 && a.IsUserIntervening(o.config.InterventionSilence)
}

// Start begins the orchestration loop.
// Returns an error if the orchestrator is already running.
func (o *Orchestrator) Start() error {
	o.mu.Lock()
	defer o.mu.Unlock()

	if o.running {
		return ErrAlreadyRunning
	}

	o.stopCh = make(chan struct{})
	o.doneCh = make(chan struct{})
	o.running = true

	go o.run()
	return nil
}

// Stop signals the orchestration loop to stop.
// It does not wait for the loop to exit - cleanup happens asynchronously.
func (o *Orchestrator) Stop() {
	o.mu.Lock()
	if !o.running {
		o.mu.Unlock()
		return
	}

	close(o.stopCh)
	o.running = false
	o.mu.Unlock()
}

// StopCh returns a channel that is closed when stop is requested.
func (o *Orchestrator) StopCh() <-chan struct{} {
	o.mu.RLock()
	defer o.mu.RUnlock()
	return o.stopCh
}

// run is the main orchestration loop.
func (o *Orchestrator) run() {
	defer logging.LogPanic("orchestrator-loop", nil)
	defer close(o.doneCh)

	// Determine poll interval
	pollInterval := o.config.PollInterval
	if pollInterval == 0 {
		pollInterval = DefaultPollInterval
	}

	// Initial check for ready issues and spawn agents
	o.checkAndSpawnAgents()

	// Main loop - poll for ready issues
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-o.stopCh:
			return
		case <-ticker.C:
			// Check for ready issues and spawn agents as needed
			o.checkAndSpawnAgents()
		}
	}
}

// checkAndSpawnAgents checks for ready issues and spawns agents for them.
// Only spawns agents when there are unclaimed ready issues available.
func (o *Orchestrator) checkAndSpawnAgents() {
	proj := o.project

	// Check how many agent slots are available
	current := o.agents.CountByProject(proj.Name)
	available := proj.MaxAgents - current
	if available <= 0 {
		return
	}

	// Check for ready issues (issues with no open dependencies)
	readyCount, err := o.countUnclaimedReadyIssues()
	if err != nil {
		slog.Debug("failed to check ready issues",
			"project", proj.Name,
			"error", err,
		)
		return
	}

	// Don't spawn more agents than ready issues
	toSpawn := available
	if readyCount < toSpawn {
		toSpawn = readyCount
	}

	if toSpawn <= 0 {
		return
	}

	slog.Info("spawning agents for ready issues",
		"project", proj.Name,
		"ready_issues", readyCount,
		"spawning", toSpawn,
		"current_agents", current,
		"max_agents", proj.MaxAgents,
	)

	// Spawn the agents
	for i := 0; i < toSpawn; i++ {
		if err := o.spawnAgent(); err != nil {
			slog.Debug("failed to spawn agent",
				"project", proj.Name,
				"error", err,
			)
			break
		}
	}
}

// countUnclaimedReadyIssues returns the count of ready issues that aren't already claimed.
func (o *Orchestrator) countUnclaimedReadyIssues() (int, error) {
	if o.config.IssueBackendFactory == nil {
		// No issue backend configured, return 0 (no auto-spawning)
		return 0, nil
	}

	backend, err := o.config.IssueBackendFactory(o.project.RepoDir())
	if err != nil {
		return 0, fmt.Errorf("create issue backend: %w", err)
	}

	ctx := context.Background()
	readyIssues, err := backend.Ready(ctx)
	if err != nil {
		return 0, fmt.Errorf("get ready issues: %w", err)
	}

	// Count issues that aren't already claimed
	unclaimed := 0
	for _, iss := range readyIssues {
		if !o.claims.IsClaimed(iss.ID) {
			unclaimed++
		}
	}

	return unclaimed, nil
}

// spawnAgent creates and starts a single agent.
func (o *Orchestrator) spawnAgent() error {
	a, err := o.agents.Create(o.project)
	if err != nil {
		return err
	}

	// Set the agent mode from config
	a.SetMode(o.config.DefaultAgentMode)

	// Start the agent process immediately (without prompt)
	if err := a.Start(""); err != nil {
		return fmt.Errorf("start agent process: %w", err)
	}

	// Notify that the agent has started (for read loop setup)
	if o.config.OnAgentStarted != nil {
		o.config.OnAgentStarted(a)
	}

	// Execute kickstart immediately (no approval needed)
	o.executeKickstart(a, o.config.KickstartPrompt)

	return nil
}

// QueueKickstart queues or executes the kickstart action based on mode.
// Returns true if kickstart was queued/executed, false if skipped due to user intervention.
// This should be called when an agent becomes idle to resume automatic task execution.
func (o *Orchestrator) QueueKickstart(a *agent.Agent) bool {
	prompt := o.config.KickstartPrompt
	if prompt == "" {
		return false
	}

	// Skip kickstart if user is currently intervening with this agent
	if o.config.InterventionSilence > 0 && a.IsUserIntervening(o.config.InterventionSilence) {
		slog.Debug("skipping kickstart due to user intervention",
			"agent", a.ID,
			"project", o.project.Name,
			"last_input", a.GetLastUserInput(),
		)
		return false
	}

	if a.IsAutoMode() {
		// Execute immediately
		o.executeKickstart(a, prompt)
	} else {
		// Stage for approval
		o.actions.Add(StagedAction{
			AgentID:   a.ID,
			Project:   o.project.Name,
			Type:      ActionSendMessage,
			Payload:   prompt,
			CreatedAt: time.Now(),
		})
	}
	return true
}

// executeKickstart sends the kickstart prompt to an agent.
func (o *Orchestrator) executeKickstart(a *agent.Agent, prompt string) {
	// Use SendMessage instead of Write
	if err := a.SendMessage(prompt); err != nil {
		// Log error but continue
		return
	}
}

// AgentDoneResult contains the outcome of HandleAgentDone.
type AgentDoneResult struct {
	Merged     bool   // True if merge to main succeeded
	BranchName string // The branch that was processed
	SHA        string // Commit SHA of merge commit (only set if Merged is true)
	MergeError string // Conflict message if merge failed
}

// HandleAgentDone handles an agent signaling task completion.
// If merge succeeds, cleans up the agent and spawns a replacement.
// If merge fails, rebases the worktree and returns error (agent stays running to fix conflicts).
func (o *Orchestrator) HandleAgentDone(agentID, taskID, errorMsg string) (*AgentDoneResult, error) {
	result := &AgentDoneResult{}

	// Try to merge agent's branch into main
	mergeResult, err := o.project.MergeAgentBranch(agentID)
	if err != nil {
		return nil, fmt.Errorf("merge attempt: %w", err)
	}

	result.BranchName = mergeResult.BranchName

	if mergeResult.Merged {
		// Success! Clean up the agent
		result.Merged = true
		result.SHA = mergeResult.SHA
		slog.Info("merged agent branch to main", "agent", agentID, "branch", mergeResult.BranchName, "sha", mergeResult.SHA)

		// Record the commit
		o.commits.Add(CommitRecord{
			SHA:      mergeResult.SHA,
			Branch:   mergeResult.BranchName,
			AgentID:  agentID,
			TaskID:   taskID,
			MergedAt: time.Now(),
		})

		_ = o.agents.Stop(agentID)
		if err := o.agents.Delete(agentID); err != nil {
			return result, err
		}

		// Release claims AFTER successful merge and cleanup
		released := o.claims.ReleaseByAgent(agentID)
		if released > 0 {
			slog.Debug("released ticket claims after merge", "agent", agentID, "count", released)
		}

		// Check for new issues and spawn agents as needed
		o.checkAndSpawnAgents()
	} else {
		// Merge conflict - rebase worktree onto latest main
		// Do NOT release claims - agent must fix conflicts
		result.MergeError = mergeResult.Error.Error()

		if err := o.project.RebaseWorktreeOnMain(agentID); err != nil {
			slog.Warn("failed to rebase worktree after merge conflict", "agent", agentID, "error", err)
		}

		slog.Warn("merge conflict, agent must resolve",
			"agent", agentID,
			"branch", mergeResult.BranchName,
			"error", mergeResult.Error)
	}

	return result, nil
}

// ApproveAction approves and executes a staged action.
// The action is only removed from the queue on successful execution,
// allowing retries if execution fails due to transient errors.
// If the agent is in a terminal state (done/error), the action is removed
// and an appropriate error is returned.
func (o *Orchestrator) ApproveAction(actionID string) error {
	action, ok := o.actions.Get(actionID)
	if !ok {
		return ErrActionNotFound
	}

	// Check if agent can accept input before attempting execution
	a, err := o.agents.Get(action.AgentID)
	if err != nil {
		// Agent no longer exists - remove stale action
		o.actions.Remove(actionID)
		return fmt.Errorf("agent %s not found: %w", action.AgentID, err)
	}

	if a.IsTerminal() {
		// Agent is done or errored - remove stale action
		o.actions.Remove(actionID)
		return fmt.Errorf("agent %s is in %s state", action.AgentID, a.GetState())
	}

	// Execute the action
	if err := o.executeAction(action); err != nil {
		return fmt.Errorf("failed to execute action: %w", err)
	}

	// Success - remove from queue
	o.actions.Remove(actionID)
	return nil
}

// RejectAction rejects and removes a staged action.
func (o *Orchestrator) RejectAction(actionID string, reason string) error {
	_, ok := o.actions.Remove(actionID)
	if !ok {
		return ErrActionNotFound
	}
	return nil
}

// executeAction executes a staged action.
func (o *Orchestrator) executeAction(action StagedAction) error {
	a, err := o.agents.Get(action.AgentID)
	if err != nil {
		return err
	}

	switch action.Type {
	case ActionSendMessage:
		// Use SendMessage instead of Write
		return a.SendMessage(action.Payload)

	case ActionQuit:
		// Send /quit as a message
		return a.SendMessage("/quit")

	default:
		return ErrUnknownActionType
	}
}
