package supervisor

import (
	"sync"
	"testing"
	"time"

	"github.com/tessro/fab/internal/agent"
)

func TestHeartbeatMonitor_RecordOutput(t *testing.T) {
	agents := agent.NewManager()

	cfg := DefaultHeartbeatConfig()
	cfg.SendMessage = func(agentID, message string) error {
		return nil
	}
	cfg.StopAgent = func(agentID string) error {
		return nil
	}

	hb := NewHeartbeatMonitor(agents, cfg)

	// Record output for an agent
	hb.RecordOutput("test-agent-1")

	// Verify tracker was created
	hb.mu.RLock()
	tracker, ok := hb.trackers["test-agent-1"]
	hb.mu.RUnlock()

	if !ok {
		t.Fatal("expected tracker to be created")
	}

	if tracker.state != HeartbeatNormal {
		t.Errorf("expected state HeartbeatNormal, got %v", tracker.state)
	}

	if tracker.lastOutputTime.IsZero() {
		t.Error("expected lastOutputTime to be set")
	}
}

func TestHeartbeatMonitor_RemoveAgent(t *testing.T) {
	agents := agent.NewManager()

	cfg := DefaultHeartbeatConfig()
	cfg.SendMessage = func(agentID, message string) error {
		return nil
	}
	cfg.StopAgent = func(agentID string) error {
		return nil
	}

	hb := NewHeartbeatMonitor(agents, cfg)

	// Record and then remove
	hb.RecordOutput("test-agent-1")
	hb.RemoveAgent("test-agent-1")

	hb.mu.RLock()
	_, ok := hb.trackers["test-agent-1"]
	hb.mu.RUnlock()

	if ok {
		t.Error("expected tracker to be removed")
	}
}

func TestHeartbeatMonitor_SendsContinueAfterTimeout(t *testing.T) {
	agents := agent.NewManager()

	var continueMessages []string
	var mu sync.Mutex

	cfg := HeartbeatConfig{
		CheckInterval: 10 * time.Millisecond,
		Timeout:       50 * time.Millisecond,
		KillTimeout:   150 * time.Millisecond,
		SendMessage: func(agentID, message string) error {
			mu.Lock()
			continueMessages = append(continueMessages, agentID+":"+message)
			mu.Unlock()
			return nil
		},
		StopAgent: func(agentID string) error {
			return nil
		},
	}

	hb := NewHeartbeatMonitor(agents, cfg)

	// Set up an agent with old output time
	agentID := "test-agent"
	silenceDuration := 100 * time.Millisecond

	hb.mu.Lock()
	hb.trackers[agentID] = &agentHeartbeat{
		lastOutputTime: time.Now().Add(-silenceDuration),
		state:          HeartbeatNormal,
	}
	hb.mu.Unlock()

	// Directly call sendContinue (since checkAgents needs real agents in manager)
	hb.sendContinue(agentID, "test-project", silenceDuration)

	// Verify continue was sent
	mu.Lock()
	defer mu.Unlock()

	if len(continueMessages) != 1 {
		t.Fatalf("expected 1 continue message, got %d", len(continueMessages))
	}

	if continueMessages[0] != "test-agent:continue" {
		t.Errorf("expected 'test-agent:continue', got %s", continueMessages[0])
	}

	// Verify state was updated to warned
	hb.mu.RLock()
	tracker := hb.trackers[agentID]
	hb.mu.RUnlock()

	if tracker.state != HeartbeatWarned {
		t.Errorf("expected state HeartbeatWarned, got %v", tracker.state)
	}
}

func TestHeartbeatMonitor_KillsAgentAfterKillTimeout(t *testing.T) {
	agents := agent.NewManager()

	var killedAgents []string
	var mu sync.Mutex

	cfg := HeartbeatConfig{
		CheckInterval: 10 * time.Millisecond,
		Timeout:       50 * time.Millisecond,
		KillTimeout:   100 * time.Millisecond,
		SendMessage: func(agentID, message string) error {
			return nil
		},
		StopAgent: func(agentID string) error {
			mu.Lock()
			killedAgents = append(killedAgents, agentID)
			mu.Unlock()
			return nil
		},
	}

	hb := NewHeartbeatMonitor(agents, cfg)

	// Set up an agent that has been warned and is past kill timeout
	agentID := "stuck-agent"
	silenceDuration := 200 * time.Millisecond

	hb.mu.Lock()
	hb.trackers[agentID] = &agentHeartbeat{
		lastOutputTime: time.Now().Add(-silenceDuration),
		state:          HeartbeatWarned,
		warnedAt:       time.Now().Add(-100 * time.Millisecond),
	}
	hb.mu.Unlock()

	// Directly call killAgent (since checkAgents needs real agents in manager)
	hb.killAgent(agentID, "test-project", silenceDuration)

	// Verify agent was killed
	mu.Lock()
	defer mu.Unlock()

	if len(killedAgents) != 1 {
		t.Fatalf("expected 1 killed agent, got %d", len(killedAgents))
	}

	if killedAgents[0] != "stuck-agent" {
		t.Errorf("expected 'stuck-agent', got %s", killedAgents[0])
	}

	// Verify tracker was removed
	hb.mu.RLock()
	_, ok := hb.trackers[agentID]
	hb.mu.RUnlock()

	if ok {
		t.Error("expected tracker to be removed after kill")
	}
}

func TestHeartbeatMonitor_DoesNotActOnRecentOutput(t *testing.T) {
	agents := agent.NewManager()

	var actions []string
	var mu sync.Mutex

	cfg := HeartbeatConfig{
		CheckInterval: 10 * time.Millisecond,
		Timeout:       100 * time.Millisecond,
		KillTimeout:   200 * time.Millisecond,
		SendMessage: func(agentID, message string) error {
			mu.Lock()
			actions = append(actions, "continue:"+agentID)
			mu.Unlock()
			return nil
		},
		StopAgent: func(agentID string) error {
			mu.Lock()
			actions = append(actions, "kill:"+agentID)
			mu.Unlock()
			return nil
		},
	}

	hb := NewHeartbeatMonitor(agents, cfg)

	// Record recent output
	hb.RecordOutput("active-agent")

	// Run a check
	hb.checkAgents()

	// Verify no actions were taken
	mu.Lock()
	defer mu.Unlock()

	if len(actions) != 0 {
		t.Errorf("expected no actions on recent output, got %v", actions)
	}
}

func TestHeartbeatMonitor_StartStop(t *testing.T) {
	agents := agent.NewManager()

	cfg := DefaultHeartbeatConfig()
	cfg.CheckInterval = 10 * time.Millisecond
	cfg.SendMessage = func(agentID, message string) error {
		return nil
	}
	cfg.StopAgent = func(agentID string) error {
		return nil
	}

	hb := NewHeartbeatMonitor(agents, cfg)

	// Start should not block
	hb.Start()

	// Give it time to run a few cycles
	time.Sleep(50 * time.Millisecond)

	// Stop should not block
	hb.Stop()

	// Verify we can start again after stop
	hb.Start()
	time.Sleep(20 * time.Millisecond)
	hb.Stop()
}

func TestHeartbeatMonitor_OutputResetsWarning(t *testing.T) {
	agents := agent.NewManager()

	cfg := DefaultHeartbeatConfig()
	cfg.SendMessage = func(agentID, message string) error {
		return nil
	}
	cfg.StopAgent = func(agentID string) error {
		return nil
	}

	hb := NewHeartbeatMonitor(agents, cfg)

	// Set up a warned agent
	hb.mu.Lock()
	hb.trackers["test-agent"] = &agentHeartbeat{
		lastOutputTime: time.Now().Add(-3 * time.Minute),
		state:          HeartbeatWarned,
		warnedAt:       time.Now().Add(-1 * time.Minute),
	}
	hb.mu.Unlock()

	// New output should reset state
	hb.RecordOutput("test-agent")

	hb.mu.RLock()
	tracker := hb.trackers["test-agent"]
	hb.mu.RUnlock()

	if tracker.state != HeartbeatNormal {
		t.Errorf("expected state HeartbeatNormal after output, got %v", tracker.state)
	}
}
