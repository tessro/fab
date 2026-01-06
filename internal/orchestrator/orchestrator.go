// Package orchestrator manages the automatic agent lifecycle for projects.
package orchestrator

import (
	"fmt"
	"sync"
	"time"

	"log/slog"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/logging"
	"github.com/tessro/fab/internal/project"
)

// Config configures orchestrator behavior.
type Config struct {
	// DefaultAgentMode is propagated to new agents.
	// ModeManual (default) stages actions for user confirmation.
	// ModeAuto executes actions immediately.
	DefaultAgentMode agent.Mode

	// KickstartPrompt is sent to agents when they start.
	KickstartPrompt string

	// OnAgentStarted is called after an agent's PTY is started.
	// Use this to set up output reading/broadcasting.
	OnAgentStarted func(*agent.Agent)
}

// DefaultConfig returns the default orchestrator configuration.
func DefaultConfig() Config {
	return Config{
		DefaultAgentMode: agent.DefaultMode,
		KickstartPrompt: `Run 'tk ready' to find available tasks.
Pick one and run 'fab agent claim <id>' to claim it.
If already claimed, pick another from the list.
When done:
1. Run all quality gates
2. Run 'tk close <id>' to close the task
3. Commit your work (include the ticket change in .tickets/)
4. Run 'fab agent done'
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
	}
}

// Claims returns the ticket claim registry.
func (o *Orchestrator) Claims() *ClaimRegistry {
	return o.claims
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

	// Initial spawn of agents up to capacity
	o.spawnAgentsToCapacity()

	// Main loop - handle events
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-o.stopCh:
			return
		case <-ticker.C:
			// Periodically check if we need to spawn more agents
			o.spawnAgentsToCapacity()
		}
	}
}

// spawnAgentsToCapacity creates agents up to the project's MaxAgents limit.
func (o *Orchestrator) spawnAgentsToCapacity() {
	proj := o.project
	current := o.agents.CountByProject(proj.Name)
	available := proj.MaxAgents - current

	for i := 0; i < available; i++ {
		a, err := o.agents.Create(proj)
		if err != nil {
			// No more worktrees available or other error
			break
		}

		// Set the agent mode from config
		a.SetMode(o.config.DefaultAgentMode)

		// Start the PTY immediately (without prompt)
		if err := a.Start(""); err != nil {
			// Failed to start PTY, skip this agent
			continue
		}

		// Notify that the agent has started (for read loop setup)
		if o.config.OnAgentStarted != nil {
			o.config.OnAgentStarted(a)
		}

		// Queue kickstart action (will write to stdin)
		o.queueKickstart(a)
	}
}

// queueKickstart queues or executes the kickstart action based on mode.
func (o *Orchestrator) queueKickstart(a *agent.Agent) {
	prompt := o.config.KickstartPrompt
	if prompt == "" {
		return
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
		slog.Info("merged agent branch to main", "agent", agentID, "branch", mergeResult.BranchName)

		_ = o.agents.Stop(agentID)
		if err := o.agents.Delete(agentID); err != nil {
			return result, err
		}

		// Release claims AFTER successful merge and cleanup
		released := o.claims.ReleaseByAgent(agentID)
		if released > 0 {
			slog.Debug("released ticket claims after merge", "agent", agentID, "count", released)
		}

		// Spawn a replacement agent
		o.spawnAgentsToCapacity()
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
