package processagent

import (
	"os/exec"
	"testing"
	"time"
)

func TestNewProcessAgent(t *testing.T) {
	config := Config{
		WorkDir:   "/tmp/test",
		LogPrefix: "test",
		BuildCommand: func() (*exec.Cmd, error) {
			return exec.Command("echo", "test"), nil
		},
	}

	p := New(config)

	if p.State() != StateStopped {
		t.Errorf("initial state = %v, want %v", p.State(), StateStopped)
	}

	if p.WorkDir() != "/tmp/test" {
		t.Errorf("WorkDir() = %v, want /tmp/test", p.WorkDir())
	}

	if p.IsRunning() {
		t.Error("IsRunning() = true for new agent, want false")
	}
}

func TestState(t *testing.T) {
	tests := []struct {
		state State
		want  string
	}{
		{StateStopped, "stopped"},
		{StateStarting, "starting"},
		{StateRunning, "running"},
		{StateStopping, "stopping"},
	}

	for _, tt := range tests {
		if string(tt.state) != tt.want {
			t.Errorf("State %v = %q, want %q", tt.state, string(tt.state), tt.want)
		}
	}
}

func TestOnStateChangeCallback(t *testing.T) {
	config := Config{
		WorkDir:   t.TempDir(),
		LogPrefix: "test",
		BuildCommand: func() (*exec.Cmd, error) {
			// Use a command that exits immediately
			return exec.Command("true"), nil
		},
	}

	p := New(config)

	var stateChanges []struct{ old, new State }
	p.OnStateChange(func(old, new State) {
		stateChanges = append(stateChanges, struct{ old, new State }{old, new})
	})

	// Start and wait a bit for the process to complete
	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Wait for process to exit naturally
	time.Sleep(100 * time.Millisecond)

	// Should have at least one state change (stopped -> running)
	if len(stateChanges) == 0 {
		t.Error("expected at least one state change callback")
	}

	foundStarting := false
	for _, change := range stateChanges {
		if change.old == StateStarting && change.new == StateRunning {
			foundStarting = true
			break
		}
	}
	if !foundStarting {
		t.Errorf("expected starting -> running transition, got %v", stateChanges)
	}
}

func TestStopNotRunning(t *testing.T) {
	config := Config{
		WorkDir:   "/tmp/test",
		LogPrefix: "test",
		BuildCommand: func() (*exec.Cmd, error) {
			return exec.Command("echo", "test"), nil
		},
	}

	p := New(config)

	err := p.Stop()
	if err != ErrNotRunning {
		t.Errorf("Stop() error = %v, want %v", err, ErrNotRunning)
	}
}

func TestStartAlreadyRunning(t *testing.T) {
	config := Config{
		WorkDir:   t.TempDir(),
		LogPrefix: "test",
		BuildCommand: func() (*exec.Cmd, error) {
			// Long-running command
			return exec.Command("sleep", "10"), nil
		},
	}

	p := New(config)

	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = p.Stop() }()

	// Try to start again
	err := p.Start()
	if err != ErrAlreadyRunning {
		t.Errorf("second Start() error = %v, want %v", err, ErrAlreadyRunning)
	}
}

func TestSendMessageNotRunning(t *testing.T) {
	config := Config{
		WorkDir:   "/tmp/test",
		LogPrefix: "test",
		BuildCommand: func() (*exec.Cmd, error) {
			return exec.Command("echo", "test"), nil
		},
	}

	p := New(config)

	err := p.SendMessage("hello")
	if err != ErrNotRunning {
		t.Errorf("SendMessage() error = %v, want %v", err, ErrNotRunning)
	}
}

func TestStartedAt(t *testing.T) {
	config := Config{
		WorkDir:   t.TempDir(),
		LogPrefix: "test",
		BuildCommand: func() (*exec.Cmd, error) {
			return exec.Command("sleep", "10"), nil
		},
	}

	p := New(config)

	// Not started yet - should be zero
	if !p.StartedAt().IsZero() {
		t.Error("StartedAt() should be zero before Start()")
	}

	before := time.Now()
	if err := p.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	after := time.Now()
	defer func() { _ = p.Stop() }()

	startedAt := p.StartedAt()
	if startedAt.Before(before) || startedAt.After(after) {
		t.Errorf("StartedAt() = %v, want between %v and %v", startedAt, before, after)
	}
}
