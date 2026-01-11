// Package supervisor provides the daemon request handler and orchestration logic.
package supervisor

import (
	"log/slog"
	"sync"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/logging"
)

// Default heartbeat configuration values.
const (
	// DefaultHeartbeatCheckInterval is how often to check for stuck agents.
	DefaultHeartbeatCheckInterval = 30 * time.Second

	// DefaultHeartbeatTimeout is the duration of silence before sending a "continue" message.
	DefaultHeartbeatTimeout = 2 * time.Minute

	// DefaultHeartbeatKillTimeout is the total duration of silence before killing an agent.
	// This is measured from the last output, not from when "continue" was sent.
	DefaultHeartbeatKillTimeout = 4 * time.Minute
)

// HeartbeatState tracks the intervention state for a single agent.
type HeartbeatState int

const (
	// HeartbeatNormal indicates the agent is producing output normally.
	HeartbeatNormal HeartbeatState = iota

	// HeartbeatWarned indicates a "continue" message has been sent.
	HeartbeatWarned
)

// agentHeartbeat tracks heartbeat state for a single agent.
type agentHeartbeat struct {
	lastOutputTime time.Time      // When the agent last produced output
	state          HeartbeatState // Current intervention state
	warnedAt       time.Time      // When "continue" was sent (if state == HeartbeatWarned)
}

// HeartbeatMonitor monitors agents for inactivity and sends "continue" messages
// or kills stuck agents as needed.
type HeartbeatMonitor struct {
	agents        *agent.Manager
	sendMessage   func(agentID, message string) error
	stopAgent     func(agentID string) error
	checkInterval time.Duration
	timeout       time.Duration
	killTimeout   time.Duration

	mu sync.RWMutex
	// +checklocks:mu
	trackers map[string]*agentHeartbeat // agent ID -> tracker

	stopCh chan struct{}
	doneCh chan struct{}
}

// HeartbeatConfig configures the heartbeat monitor.
type HeartbeatConfig struct {
	// CheckInterval is how often to check for stuck agents.
	CheckInterval time.Duration

	// Timeout is the duration of silence before sending a "continue" message.
	Timeout time.Duration

	// KillTimeout is the total duration of silence before killing an agent.
	KillTimeout time.Duration

	// SendMessage sends a message to an agent. Required.
	SendMessage func(agentID, message string) error

	// StopAgent stops an agent. Required.
	StopAgent func(agentID string) error
}

// DefaultHeartbeatConfig returns the default heartbeat configuration.
func DefaultHeartbeatConfig() HeartbeatConfig {
	return HeartbeatConfig{
		CheckInterval: DefaultHeartbeatCheckInterval,
		Timeout:       DefaultHeartbeatTimeout,
		KillTimeout:   DefaultHeartbeatKillTimeout,
	}
}

// NewHeartbeatMonitor creates a new heartbeat monitor.
func NewHeartbeatMonitor(agents *agent.Manager, cfg HeartbeatConfig) *HeartbeatMonitor {
	if cfg.CheckInterval == 0 {
		cfg.CheckInterval = DefaultHeartbeatCheckInterval
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = DefaultHeartbeatTimeout
	}
	if cfg.KillTimeout == 0 {
		cfg.KillTimeout = DefaultHeartbeatKillTimeout
	}

	return &HeartbeatMonitor{
		agents:        agents,
		sendMessage:   cfg.SendMessage,
		stopAgent:     cfg.StopAgent,
		checkInterval: cfg.CheckInterval,
		timeout:       cfg.Timeout,
		killTimeout:   cfg.KillTimeout,
		trackers:      make(map[string]*agentHeartbeat),
	}
}

// Start begins the heartbeat monitoring loop.
func (h *HeartbeatMonitor) Start() {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Check if already running by seeing if stopCh is open
	if h.stopCh != nil {
		select {
		case <-h.stopCh:
			// Channel is closed, was stopped - OK to restart
		default:
			// Channel is open, still running
			return
		}
	}

	h.stopCh = make(chan struct{})
	h.doneCh = make(chan struct{})

	go h.run()
}

// Stop signals the heartbeat monitor to stop.
func (h *HeartbeatMonitor) Stop() {
	h.mu.Lock()
	stopCh := h.stopCh
	doneCh := h.doneCh
	h.mu.Unlock()

	if stopCh == nil {
		return
	}

	// Close stopCh if not already closed
	select {
	case <-stopCh:
		// Already closed
	default:
		close(stopCh)
	}

	// Wait for run loop to finish
	if doneCh != nil {
		<-doneCh
	}
}

// RecordOutput records that an agent has produced output.
// This should be called whenever a ChatEntry is received from an agent.
func (h *HeartbeatMonitor) RecordOutput(agentID string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	tracker, ok := h.trackers[agentID]
	if !ok {
		tracker = &agentHeartbeat{}
		h.trackers[agentID] = tracker
	}

	tracker.lastOutputTime = time.Now()
	tracker.state = HeartbeatNormal
}

// RemoveAgent removes tracking for an agent (e.g., when it's deleted).
func (h *HeartbeatMonitor) RemoveAgent(agentID string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.trackers, agentID)
}

// run is the main monitoring loop.
func (h *HeartbeatMonitor) run() {
	defer logging.LogPanic("heartbeat-monitor", nil)
	defer close(h.doneCh)

	ticker := time.NewTicker(h.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-h.stopCh:
			return
		case <-ticker.C:
			h.checkAgents()
		}
	}
}

// checkAgents checks all active agents for inactivity.
func (h *HeartbeatMonitor) checkAgents() {
	now := time.Now()

	// Get list of active agents
	activeAgents := h.agents.ActiveAgents()

	for _, a := range activeAgents {
		h.checkAgent(a, now)
	}

	// Clean up trackers for agents that no longer exist
	h.cleanupTrackers()
}

// checkAgent checks a single agent for inactivity.
func (h *HeartbeatMonitor) checkAgent(a *agent.Agent, now time.Time) {
	agentID := a.ID
	info := a.Info()

	h.mu.Lock()
	tracker, ok := h.trackers[agentID]
	if !ok {
		// No output recorded yet - start tracking from now
		tracker = &agentHeartbeat{
			lastOutputTime: now,
			state:          HeartbeatNormal,
		}
		h.trackers[agentID] = tracker
	}
	lastOutput := tracker.lastOutputTime
	state := tracker.state
	h.mu.Unlock()

	silenceDuration := now.Sub(lastOutput)

	switch state {
	case HeartbeatNormal:
		if silenceDuration >= h.timeout {
			// Agent has been silent for too long - send "continue"
			h.sendContinue(agentID, info.Project, silenceDuration)
		}

	case HeartbeatWarned:
		if silenceDuration >= h.killTimeout {
			// Agent is still stuck after "continue" - kill it
			h.killAgent(agentID, info.Project, silenceDuration)
		}
	}
}

// sendContinue sends a "continue" message to an agent and updates state to Warned.
func (h *HeartbeatMonitor) sendContinue(agentID, project string, silenceDuration time.Duration) {
	slog.Info("agent stuck, sending continue message",
		"agent", agentID,
		"project", project,
		"silence_duration", silenceDuration.Round(time.Second),
	)

	if h.sendMessage != nil {
		if err := h.sendMessage(agentID, "continue"); err != nil {
			slog.Warn("failed to send continue message",
				"agent", agentID,
				"error", err,
			)
			return
		}
	}

	// Update state to warned
	h.mu.Lock()
	if tracker, ok := h.trackers[agentID]; ok {
		tracker.state = HeartbeatWarned
		tracker.warnedAt = time.Now()
	}
	h.mu.Unlock()
}

// killAgent kills an agent that is stuck even after "continue" was sent.
func (h *HeartbeatMonitor) killAgent(agentID, project string, silenceDuration time.Duration) {
	slog.Warn("agent still stuck after continue, killing",
		"agent", agentID,
		"project", project,
		"silence_duration", silenceDuration.Round(time.Second),
	)

	if h.stopAgent != nil {
		if err := h.stopAgent(agentID); err != nil {
			slog.Error("failed to kill stuck agent",
				"agent", agentID,
				"error", err,
			)
			return
		}
	}

	// Remove tracker since agent is dead
	h.RemoveAgent(agentID)
}

// cleanupTrackers removes trackers for agents that no longer exist.
func (h *HeartbeatMonitor) cleanupTrackers() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for agentID := range h.trackers {
		if _, err := h.agents.Get(agentID); err != nil {
			delete(h.trackers, agentID)
		}
	}
}
