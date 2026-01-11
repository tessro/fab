package agent

import (
	"errors"
	"testing"
	"time"
)

func TestAgent_StateTransitions(t *testing.T) {
	a := New("test-1", nil, nil)

	// Initial state is Starting
	if a.GetState() != StateStarting {
		t.Errorf("expected Starting, got %s", a.GetState())
	}

	// Starting -> Running
	if err := a.MarkRunning(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if a.GetState() != StateRunning {
		t.Errorf("expected Running, got %s", a.GetState())
	}

	// Running -> Idle
	if err := a.MarkIdle(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if a.GetState() != StateIdle {
		t.Errorf("expected Idle, got %s", a.GetState())
	}

	// Idle -> Done
	if err := a.MarkDone(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if a.GetState() != StateDone {
		t.Errorf("expected Done, got %s", a.GetState())
	}

	// Task should be cleared on Done
	a.SetTask("TEST-1")
	_ = a.Reset()
	_ = a.MarkRunning()
	a.SetTask("TEST-2")
	_ = a.MarkDone()
	if a.GetTask() != "" {
		t.Errorf("expected empty task after Done, got %s", a.GetTask())
	}
}

func TestAgent_InvalidTransitions(t *testing.T) {
	a := New("test-1", nil, nil)

	// Can't go Starting -> Idle
	if err := a.MarkIdle(); err != ErrInvalidTransition {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}

	// Can't go Starting -> Done
	if err := a.MarkDone(); err != ErrInvalidTransition {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}

	// Move to Running
	_ = a.MarkRunning()

	// Can't go Running -> Starting
	if err := a.Transition(StateStarting); err != ErrInvalidTransition {
		t.Errorf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestAgent_IsActive(t *testing.T) {
	a := New("test-1", nil, nil)

	// Starting is active
	if !a.IsActive() {
		t.Error("expected Starting to be active")
	}

	// Running is active
	_ = a.MarkRunning()
	if !a.IsActive() {
		t.Error("expected Running to be active")
	}

	// Idle is active
	_ = a.MarkIdle()
	if !a.IsActive() {
		t.Error("expected Idle to be active")
	}

	// Done is not active
	_ = a.MarkDone()
	if a.IsActive() {
		t.Error("expected Done to not be active")
	}
}

func TestAgent_IsTerminal(t *testing.T) {
	a := New("test-1", nil, nil)

	// Starting is not terminal
	if a.IsTerminal() {
		t.Error("expected Starting to not be terminal")
	}

	// Done is terminal
	_ = a.MarkRunning()
	_ = a.MarkDone()
	if !a.IsTerminal() {
		t.Error("expected Done to be terminal")
	}

	// Error is terminal
	a2 := New("test-2", nil, nil)
	_ = a2.MarkRunning()
	_ = a2.MarkError()
	if !a2.IsTerminal() {
		t.Error("expected Error to be terminal")
	}
}

func TestAgent_ReadLoop_NoProcess(t *testing.T) {
	a := New("test-1", nil, nil)

	// StartReadLoop should fail without process
	err := a.StartReadLoop(DefaultReadLoopConfig())
	if err != ErrProcessNotStarted {
		t.Errorf("expected ErrProcessNotStarted, got %v", err)
	}

	// Should not be running
	if a.IsReadLoopRunning() {
		t.Error("expected read loop to not be running")
	}
}

func TestAgent_ReadLoop_StopNoop(t *testing.T) {
	a := New("test-1", nil, nil)

	// StopReadLoop should be safe to call when not running
	a.StopReadLoop() // Should not panic

	if a.IsReadLoopRunning() {
		t.Error("expected read loop to not be running")
	}
}

func TestDefaultReadLoopConfig(t *testing.T) {
	cfg := DefaultReadLoopConfig()

	if cfg.OnEntry != nil {
		t.Error("expected OnEntry to be nil by default")
	}

	if cfg.OnOutput != nil {
		t.Error("expected OnOutput to be nil by default")
	}

	if cfg.OnError != nil {
		t.Error("expected OnError to be nil by default")
	}
}

func TestMaxScanTokenSize(t *testing.T) {
	// MaxScanTokenSize must be large enough for large file reads (at least 1MB)
	if MaxScanTokenSize < 1024*1024 {
		t.Errorf("MaxScanTokenSize too small: %d bytes, should be at least 1MB", MaxScanTokenSize)
	}

	// Current value should be 10MB
	expectedSize := 10 * 1024 * 1024
	if MaxScanTokenSize != expectedSize {
		t.Errorf("MaxScanTokenSize = %d, want %d", MaxScanTokenSize, expectedSize)
	}
}

func TestAgent_ExitError(t *testing.T) {
	a := New("test-1", nil, nil)

	// Initially nil
	if a.ExitError() != nil {
		t.Error("expected nil exit error initially")
	}

	// Set exit error
	testErr := errors.New("test error")
	a.setExitError(testErr)

	if a.ExitError() != testErr {
		t.Errorf("expected exit error to be set")
	}

	// Clear exit error
	a.setExitError(nil)
	if a.ExitError() != nil {
		t.Error("expected exit error to be cleared")
	}
}

func TestAgent_ExitCode(t *testing.T) {
	a := New("test-1", nil, nil)

	// No cmd, returns -1
	if a.ExitCode() != -1 {
		t.Errorf("expected -1 for no cmd, got %d", a.ExitCode())
	}
}

func TestAgent_IsCommandNotFound(t *testing.T) {
	a := New("test-1", nil, nil)

	// No error, not command not found
	if a.IsCommandNotFound() {
		t.Error("expected false when no exit error")
	}

	// Non-exit error, not command not found
	a.setExitError(errors.New("random error"))
	if a.IsCommandNotFound() {
		t.Error("expected false for non-exit error")
	}
}

func TestAgent_ErrorState(t *testing.T) {
	a := New("test-1", nil, nil)

	// Starting -> Error is valid
	if err := a.MarkError(); err != nil {
		t.Errorf("unexpected error transitioning to Error: %v", err)
	}
	if a.GetState() != StateError {
		t.Errorf("expected Error, got %s", a.GetState())
	}

	// Error is terminal
	if !a.IsTerminal() {
		t.Error("expected Error to be terminal")
	}

	// Error is not active
	if a.IsActive() {
		t.Error("expected Error to not be active")
	}

	// Can restart from Error
	if err := a.Reset(); err != nil {
		t.Errorf("unexpected error resetting from Error: %v", err)
	}
	if a.GetState() != StateStarting {
		t.Errorf("expected Starting after reset, got %s", a.GetState())
	}
}

func TestAgent_UserIntervention(t *testing.T) {
	a := New("test-1", nil, nil)

	// Initially not intervening (no user input ever)
	if a.IsUserIntervening(60 * time.Second) {
		t.Error("expected not intervening when no user input")
	}

	// Last user input should be zero
	if !a.GetLastUserInput().IsZero() {
		t.Error("expected zero LastUserInput initially")
	}

	// Mark user input
	a.MarkUserInput()

	// Should now be intervening
	if !a.IsUserIntervening(60 * time.Second) {
		t.Error("expected intervening after MarkUserInput")
	}

	// Last user input should be recent
	if time.Since(a.GetLastUserInput()) > time.Second {
		t.Error("expected LastUserInput to be recent after MarkUserInput")
	}

	// With a very short threshold, should not be intervening (time has passed)
	time.Sleep(10 * time.Millisecond)
	if a.IsUserIntervening(1 * time.Millisecond) {
		t.Error("expected not intervening with very short threshold")
	}

	// With a longer threshold, should still be intervening
	if !a.IsUserIntervening(1 * time.Hour) {
		t.Error("expected intervening with long threshold")
	}
}

func TestAgent_UserIntervention_MultipleInputs(t *testing.T) {
	a := New("test-1", nil, nil)

	// Mark input
	a.MarkUserInput()
	first := a.GetLastUserInput()

	// Wait a bit and mark again
	time.Sleep(10 * time.Millisecond)
	a.MarkUserInput()
	second := a.GetLastUserInput()

	// Second input should be after first
	if !second.After(first) {
		t.Error("expected second input timestamp to be after first")
	}
}

func TestDefaultInterventionSilence(t *testing.T) {
	// Verify the default constant
	if DefaultInterventionSilence != 60*time.Second {
		t.Errorf("expected DefaultInterventionSilence to be 60s, got %v", DefaultInterventionSilence)
	}
}

func TestFlexContent_String(t *testing.T) {
	// String content should parse as string
	input := `{"type":"tool_result","tool_use_id":"123","content":"hello world"}`
	msg, err := ParseStreamMessage([]byte(`{"type":"user","message":{"role":"user","content":[` + input + `]}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Message == nil || len(msg.Message.Content) != 1 {
		t.Fatal("expected one content block")
	}
	if string(msg.Message.Content[0].Content) != "hello world" {
		t.Errorf("expected 'hello world', got %q", msg.Message.Content[0].Content)
	}
}

func TestFlexContent_Array(t *testing.T) {
	// Array content should parse and join text parts
	input := `{"type":"tool_result","tool_use_id":"123","content":[{"type":"text","text":"part1"},{"type":"text","text":"part2"}]}`
	msg, err := ParseStreamMessage([]byte(`{"type":"user","message":{"role":"user","content":[` + input + `]}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Message == nil || len(msg.Message.Content) != 1 {
		t.Fatal("expected one content block")
	}
	expected := "part1\npart2"
	if string(msg.Message.Content[0].Content) != expected {
		t.Errorf("expected %q, got %q", expected, msg.Message.Content[0].Content)
	}
}

func TestFlexContent_EmptyArray(t *testing.T) {
	// Empty array should result in empty string
	input := `{"type":"tool_result","tool_use_id":"123","content":[]}`
	msg, err := ParseStreamMessage([]byte(`{"type":"user","message":{"role":"user","content":[` + input + `]}}`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msg.Message == nil || len(msg.Message.Content) != 1 {
		t.Fatal("expected one content block")
	}
	if string(msg.Message.Content[0].Content) != "" {
		t.Errorf("expected empty string, got %q", msg.Message.Content[0].Content)
	}
}
