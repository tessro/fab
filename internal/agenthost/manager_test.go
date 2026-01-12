package agenthost

import (
	"testing"
	"time"

	"github.com/tessro/fab/internal/agent"
)

func TestNewManager(t *testing.T) {
	// Create a test agent
	ag := agent.New("test-agent", nil, nil)
	ag.SetTask("issue-42")
	ag.SetDescription("Testing the manager")

	m := NewManager(ag)

	if m.AgentID() != "test-agent" {
		t.Errorf("AgentID() = %q, want %q", m.AgentID(), "test-agent")
	}

	info := m.AgentInfo()
	if info.ID != "test-agent" {
		t.Errorf("AgentInfo().ID = %q, want %q", info.ID, "test-agent")
	}
	if info.Task != "issue-42" {
		t.Errorf("AgentInfo().Task = %q, want %q", info.Task, "issue-42")
	}
	if info.Description != "Testing the manager" {
		t.Errorf("AgentInfo().Description = %q, want %q", info.Description, "Testing the manager")
	}
	if info.State != "starting" {
		t.Errorf("AgentInfo().State = %q, want %q", info.State, "starting")
	}
}

func TestManager_StreamOffset(t *testing.T) {
	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)

	if m.StreamOffset() != 0 {
		t.Errorf("initial StreamOffset() = %d, want 0", m.StreamOffset())
	}

	// Create and buffer some events
	event := m.createEvent("output", []byte("test"))
	if event.Offset != 1 {
		t.Errorf("first event Offset = %d, want 1", event.Offset)
	}

	event2 := m.createEvent("state", nil)
	if event2.Offset != 2 {
		t.Errorf("second event Offset = %d, want 2", event2.Offset)
	}

	if m.StreamOffset() != 2 {
		t.Errorf("StreamOffset() after 2 events = %d, want 2", m.StreamOffset())
	}
}

func TestManager_GetBufferedEvents(t *testing.T) {
	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)

	// Buffer some events
	for i := 0; i < 5; i++ {
		event := m.createEvent("output", []byte("test"))
		m.mu.Lock()
		m.historyBuffer.Value = event
		m.historyBuffer = m.historyBuffer.Next()
		m.mu.Unlock()
	}

	// Get events from offset 0 (should get all 5)
	events := m.GetBufferedEvents(0)
	if len(events) != 5 {
		t.Errorf("GetBufferedEvents(0) returned %d events, want 5", len(events))
	}

	// Get events from offset 3 (should get 2)
	events = m.GetBufferedEvents(3)
	if len(events) != 2 {
		t.Errorf("GetBufferedEvents(3) returned %d events, want 2", len(events))
	}

	// Get events from offset 5 (should get 0)
	events = m.GetBufferedEvents(5)
	if len(events) != 0 {
		t.Errorf("GetBufferedEvents(5) returned %d events, want 0", len(events))
	}
}

func TestManager_OnStateChange(t *testing.T) {
	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)

	// Track events
	var receivedEvents []*StreamEvent
	m.mu.Lock()
	m.server = nil // Ensure no broadcasting
	m.mu.Unlock()

	// Override the buffer to track events
	originalOffset := m.StreamOffset()

	// Trigger a state change
	if err := ag.MarkRunning(); err != nil {
		t.Fatalf("MarkRunning() error = %v", err)
	}

	// Check that an event was created
	newOffset := m.StreamOffset()
	if newOffset <= originalOffset {
		t.Errorf("StreamOffset did not increase after state change: %d -> %d", originalOffset, newOffset)
	}

	// Get the event from buffer
	events := m.GetBufferedEvents(originalOffset)
	if len(events) == 0 {
		t.Fatal("no events buffered after state change")
	}

	lastEvent := events[len(events)-1]
	if lastEvent.Type != "state" {
		t.Errorf("event.Type = %q, want %q", lastEvent.Type, "state")
	}
	if lastEvent.State != "running" {
		t.Errorf("event.State = %q, want %q", lastEvent.State, "running")
	}

	// Check event agent ID
	if lastEvent.AgentID != "test-agent" {
		t.Errorf("event.AgentID = %q, want %q", lastEvent.AgentID, "test-agent")
	}

	_ = receivedEvents // suppress unused warning
}

func TestManager_OnChatEntry(t *testing.T) {
	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)

	originalOffset := m.StreamOffset()

	// Simulate a chat entry
	entry := agent.ChatEntry{
		Role:      "assistant",
		Content:   "Hello, world!",
		Timestamp: time.Now(),
	}
	m.onChatEntry(entry)

	newOffset := m.StreamOffset()
	if newOffset <= originalOffset {
		t.Errorf("StreamOffset did not increase after chat entry: %d -> %d", originalOffset, newOffset)
	}

	events := m.GetBufferedEvents(originalOffset)
	if len(events) == 0 {
		t.Fatal("no events buffered after chat entry")
	}

	lastEvent := events[len(events)-1]
	if lastEvent.Type != "chat_entry" {
		t.Errorf("event.Type = %q, want %q", lastEvent.Type, "chat_entry")
	}
}

func TestManager_OnOutput(t *testing.T) {
	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)

	originalOffset := m.StreamOffset()

	// Simulate raw output
	m.onOutput([]byte(`{"type":"assistant","message":"test"}`))

	newOffset := m.StreamOffset()
	if newOffset <= originalOffset {
		t.Errorf("StreamOffset did not increase after output: %d -> %d", originalOffset, newOffset)
	}

	events := m.GetBufferedEvents(originalOffset)
	if len(events) == 0 {
		t.Fatal("no events buffered after output")
	}

	lastEvent := events[len(events)-1]
	if lastEvent.Type != "output" {
		t.Errorf("event.Type = %q, want %q", lastEvent.Type, "output")
	}
	if lastEvent.Data == "" {
		t.Error("event.Data is empty, expected output data")
	}
}

func TestManager_AgentInfoNilAgent(t *testing.T) {
	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)

	// Clear the agent
	m.mu.Lock()
	m.agent = nil
	m.mu.Unlock()

	info := m.AgentInfo()
	if info.ID != "test-agent" {
		t.Errorf("AgentInfo().ID = %q, want %q", info.ID, "test-agent")
	}
	if info.State != "error" {
		t.Errorf("AgentInfo().State = %q, want %q", info.State, "error")
	}
}

func TestManager_SendMessageNoAgent(t *testing.T) {
	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)

	// Clear the agent
	m.mu.Lock()
	m.agent = nil
	m.mu.Unlock()

	err := m.SendMessage("test")
	if err != ErrAgentNotSet {
		t.Errorf("SendMessage() error = %v, want %v", err, ErrAgentNotSet)
	}
}

func TestManager_StopNoAgent(t *testing.T) {
	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)

	// Clear the agent
	m.mu.Lock()
	m.agent = nil
	m.mu.Unlock()

	err := m.Stop(false, 5*time.Second)
	if err != ErrAgentNotSet {
		t.Errorf("Stop() error = %v, want %v", err, ErrAgentNotSet)
	}
}

func TestManager_SetServer(t *testing.T) {
	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)

	if m.server != nil {
		t.Error("initial server should be nil")
	}

	s := &Server{}
	m.SetServer(s)

	m.mu.RLock()
	serverSet := m.server == s
	m.mu.RUnlock()

	if !serverSet {
		t.Error("SetServer did not set the server")
	}
}
