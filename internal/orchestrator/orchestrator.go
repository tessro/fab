// Package orchestrator manages the automatic agent lifecycle for projects.
package orchestrator

import (
	"sync"
	"time"

	"github.com/tessro/fab/internal/agent"
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
}

// DefaultConfig returns the default orchestrator configuration.
func DefaultConfig() Config {
	return Config{
		DefaultAgentMode: agent.DefaultMode,
		KickstartPrompt: `Run 'bd ready' to find a task, then work on it.
When done, run all quality gates and push your work.
Close the task with 'bd close <id>', then run 'fab agent done'.`,
	}
}

// Orchestrator manages the automatic agent lifecycle for a single project.
type Orchestrator struct {
	project *project.Project
	config  Config
	agents  *agent.Manager

	// Action queue for manual mode
	actions *ActionQueue

	// Lifecycle management
	stopCh chan struct{}
	doneCh chan struct{}
	mu     sync.RWMutex

	running bool
}

// New creates a new Orchestrator for the given project.
func New(proj *project.Project, agents *agent.Manager, cfg Config) *Orchestrator {
	return &Orchestrator{
		project: proj,
		config:  cfg,
		agents:  agents,
		actions: NewActionQueue(),
	}
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

// Stop gracefully stops the orchestration loop.
// Blocks until the loop has stopped.
func (o *Orchestrator) Stop() {
	o.mu.Lock()
	if !o.running {
		o.mu.Unlock()
		return
	}

	close(o.stopCh)
	doneCh := o.doneCh
	o.mu.Unlock()

	// Wait for the loop to exit
	<-doneCh

	o.mu.Lock()
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

		// Queue kickstart action
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
	// Start the agent's PTY with the prompt
	if err := a.Start(prompt); err != nil {
		// Log error but continue
		return
	}
}

// HandleAgentDone handles an agent signaling task completion.
func (o *Orchestrator) HandleAgentDone(agentID string, reason string) error {
	// Stop and delete the agent
	if err := o.agents.Stop(agentID); err != nil {
		// Continue anyway to clean up
	}

	if err := o.agents.Delete(agentID); err != nil {
		return err
	}

	// Spawn a replacement agent if capacity allows
	o.spawnAgentsToCapacity()

	return nil
}

// ApproveAction approves and executes a staged action.
func (o *Orchestrator) ApproveAction(actionID string) error {
	action, ok := o.actions.Remove(actionID)
	if !ok {
		return ErrActionNotFound
	}

	return o.executeAction(action)
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
		if a.GetState() == agent.StateStarting {
			// Agent hasn't started yet, start with the message as prompt
			return a.Start(action.Payload)
		}
		// Send message to running agent
		_, err := a.Write([]byte(action.Payload + "\n"))
		return err

	case ActionQuit:
		// Send /quit command
		_, err := a.Write([]byte("/quit\n"))
		return err

	default:
		return ErrUnknownActionType
	}
}
