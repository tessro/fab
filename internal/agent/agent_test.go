package agent

import (
	"errors"
	"sync/atomic"
	"testing"
)

func TestAgent_SetDetector(t *testing.T) {
	a := New("test-1", nil, nil)

	// Initially nil
	if a.Detector() != nil {
		t.Error("expected nil detector initially")
	}

	// Set detector
	d := NewDefaultDetector()
	a.SetDetector(d)

	if a.Detector() == nil {
		t.Error("expected detector to be set")
	}

	// Clear detector
	a.SetDetector(nil)
	if a.Detector() != nil {
		t.Error("expected detector to be cleared")
	}
}

func TestAgent_CheckDone(t *testing.T) {
	a := New("test-1", nil, nil)

	t.Run("nil detector returns nil", func(t *testing.T) {
		match := a.CheckDone()
		if match != nil {
			t.Error("expected nil match without detector")
		}
	})

	t.Run("empty buffer returns nil", func(t *testing.T) {
		a.SetDetector(NewDefaultDetector())
		match := a.CheckDone()
		if match != nil {
			t.Error("expected nil match for empty buffer")
		}
	})

	t.Run("detects completion pattern", func(t *testing.T) {
		a.SetDetector(NewDefaultDetector())
		a.CaptureOutput([]byte("Working on task\n"))
		a.CaptureOutput([]byte("bd close FAB-42\n"))

		match := a.CheckDone()
		if match == nil {
			t.Fatal("expected match")
		}
		if match.Pattern.Name != "beads_close" {
			t.Errorf("expected beads_close pattern, got %s", match.Pattern.Name)
		}
	})
}

func TestAgent_CheckDoneAndTransition(t *testing.T) {
	t.Run("transitions to done on match", func(t *testing.T) {
		a := New("test-1", nil, nil)
		a.SetDetector(NewDefaultDetector())

		// Need to be in Running state first (can't go from Starting to Done)
		_ = a.MarkRunning()

		a.CaptureOutput([]byte("bd close FAB-99\n"))

		match := a.CheckDoneAndTransition()
		if match == nil {
			t.Fatal("expected match")
		}

		if a.GetState() != StateDone {
			t.Errorf("expected state Done, got %s", a.GetState())
		}
	})

	t.Run("no transition without match", func(t *testing.T) {
		a := New("test-1", nil, nil)
		a.SetDetector(NewDefaultDetector())
		_ = a.MarkRunning()

		a.CaptureOutput([]byte("normal output\n"))

		match := a.CheckDoneAndTransition()
		if match != nil {
			t.Error("expected no match")
		}

		if a.GetState() != StateRunning {
			t.Errorf("expected state Running, got %s", a.GetState())
		}
	})

	t.Run("callback invoked on detection", func(t *testing.T) {
		a := New("test-1", nil, nil)
		a.SetDetector(NewDefaultDetector())
		_ = a.MarkRunning()

		var called atomic.Bool
		var matchedText string
		a.OnDoneDetect(func(m *Match) {
			called.Store(true)
			matchedText = m.Text
		})

		a.CaptureOutput([]byte("bd close FAB-77\n"))
		a.CheckDoneAndTransition()

		if !called.Load() {
			t.Error("expected callback to be invoked")
		}
		if matchedText == "" {
			t.Error("expected matched text")
		}
	})
}

func TestAgent_CheckNewOutput(t *testing.T) {
	a := New("test-1", nil, nil)
	a.SetDetector(NewDefaultDetector())

	// Write many lines
	for i := 0; i < 100; i++ {
		a.CaptureOutput([]byte("normal output line\n"))
	}

	// No match in last 10 lines
	match := a.CheckNewOutput(10)
	if match != nil {
		t.Error("expected no match in first 100 lines")
	}

	// Add completion pattern
	a.CaptureOutput([]byte("bd close FAB-123\n"))

	// Should find match in last 5 lines
	match = a.CheckNewOutput(5)
	if match == nil {
		t.Error("expected match in recent output")
	}
}

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

func TestAgent_ReadLoop_NoPTY(t *testing.T) {
	a := New("test-1", nil, nil)

	// StartReadLoop should fail without PTY
	err := a.StartReadLoop(DefaultReadLoopConfig())
	if err != ErrPTYNotStarted {
		t.Errorf("expected ErrPTYNotStarted, got %v", err)
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

	if cfg.BufferSize != 4096 {
		t.Errorf("expected BufferSize 4096, got %d", cfg.BufferSize)
	}

	if cfg.CheckDoneLines != 5 {
		t.Errorf("expected CheckDoneLines 5, got %d", cfg.CheckDoneLines)
	}

	if cfg.OnOutput != nil {
		t.Error("expected OnOutput to be nil by default")
	}

	if cfg.OnError != nil {
		t.Error("expected OnError to be nil by default")
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

func TestAgent_Mode(t *testing.T) {
	a := New("test-1", nil, nil)

	// Default mode is manual
	if a.GetMode() != ModeManual {
		t.Errorf("expected default mode Manual, got %s", a.GetMode())
	}

	if !a.IsManualMode() {
		t.Error("expected IsManualMode to be true by default")
	}

	if a.IsAutoMode() {
		t.Error("expected IsAutoMode to be false by default")
	}

	// Set to auto mode
	a.SetMode(ModeAuto)
	if a.GetMode() != ModeAuto {
		t.Errorf("expected mode Auto, got %s", a.GetMode())
	}

	if !a.IsAutoMode() {
		t.Error("expected IsAutoMode to be true after SetMode(ModeAuto)")
	}

	if a.IsManualMode() {
		t.Error("expected IsManualMode to be false after SetMode(ModeAuto)")
	}

	// Mode is included in Info
	info := a.Info()
	if info.Mode != ModeAuto {
		t.Errorf("expected Info.Mode to be Auto, got %s", info.Mode)
	}
}
