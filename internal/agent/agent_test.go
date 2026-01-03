package agent

import (
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
