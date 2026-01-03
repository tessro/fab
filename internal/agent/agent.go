// Package agent provides the Agent type and lifecycle management for Claude Code instances.
package agent

import (
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/creack/pty"
	"github.com/tessro/fab/internal/project"
)

// State represents the current state of an agent.
type State string

const (
	// StateStarting indicates the agent is initializing (PTY spawning).
	StateStarting State = "starting"

	// StateRunning indicates the agent is actively processing (output detected).
	StateRunning State = "running"

	// StateIdle indicates the agent is waiting for input (no recent output).
	StateIdle State = "idle"

	// StateDone indicates the agent completed its task (bd close detected).
	StateDone State = "done"

	// StateError indicates the agent encountered an error or crashed.
	StateError State = "error"
)

// Valid state transitions.
var validTransitions = map[State][]State{
	StateStarting: {StateRunning, StateError},
	StateRunning:  {StateIdle, StateDone, StateError},
	StateIdle:     {StateRunning, StateDone, StateError},
	StateDone:     {StateStarting}, // Can be restarted
	StateError:    {StateStarting}, // Can be restarted
}

// Errors returned by agent operations.
var (
	ErrInvalidTransition = errors.New("invalid state transition")
	ErrAgentNotRunning   = errors.New("agent is not running")
	ErrAgentAlreadyDone  = errors.New("agent has already completed")
	ErrPTYNotStarted     = errors.New("PTY has not been started")
	ErrPTYAlreadyStarted = errors.New("PTY is already running")
)

// Agent represents a Claude Code instance running in a PTY.
type Agent struct {
	ID        string            // Unique identifier (e.g., "a1b2c3")
	Project   *project.Project  // Parent project
	Worktree  *project.Worktree // Assigned worktree
	State     State             // Current state
	Task      string            // Current task ID (e.g., "FAB-25")
	StartedAt time.Time         // When the agent was created
	UpdatedAt time.Time         // Last state change

	// PTY management
	ptyFile *os.File     // PTY file descriptor for I/O
	cmd     *exec.Cmd    // The Claude Code process
	size    *pty.Winsize // Current terminal size

	// Output buffer captures recent PTY output for display/scrollback
	buffer *RingBuffer

	// Done detection
	detector     *Detector           // Pattern detector for completion signals
	onDoneDetect func(match *Match)  // Optional callback when done is detected

	mu            sync.RWMutex
	onStateChange func(old, new State) // Optional callback for state changes

	// Read loop management
	readLoopStop chan struct{}   // Signals read loop to stop
	readLoopDone chan struct{}   // Closed when read loop exits
	readLoopMu   sync.Mutex      // Protects read loop channels
}

// New creates a new Agent in the Starting state.
func New(id string, proj *project.Project, wt *project.Worktree) *Agent {
	now := time.Now()
	return &Agent{
		ID:        id,
		Project:   proj,
		Worktree:  wt,
		State:     StateStarting,
		StartedAt: now,
		UpdatedAt: now,
		buffer:    NewRingBuffer(DefaultBufferSize),
	}
}

// GetState returns the current state (thread-safe).
func (a *Agent) GetState() State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.State
}

// SetTask sets the current task ID.
func (a *Agent) SetTask(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Task = taskID
	a.UpdatedAt = time.Now()
}

// GetTask returns the current task ID.
func (a *Agent) GetTask() string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Task
}

// Transition attempts to move the agent to a new state.
// Returns ErrInvalidTransition if the transition is not allowed.
func (a *Agent) Transition(newState State) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if !a.canTransition(newState) {
		return ErrInvalidTransition
	}

	oldState := a.State
	a.State = newState
	a.UpdatedAt = time.Now()

	// Clear task on completion or error
	if newState == StateDone || newState == StateError {
		a.Task = ""
	}

	// Call state change callback outside the lock would be better,
	// but for simplicity we call it here (callback should be fast)
	if a.onStateChange != nil {
		a.onStateChange(oldState, newState)
	}

	return nil
}

// canTransition checks if transitioning to newState is valid.
// Must be called with lock held.
func (a *Agent) canTransition(newState State) bool {
	allowed, ok := validTransitions[a.State]
	if !ok {
		return false
	}
	for _, s := range allowed {
		if s == newState {
			return true
		}
	}
	return false
}

// OnStateChange sets a callback that's invoked on state transitions.
func (a *Agent) OnStateChange(fn func(old, new State)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onStateChange = fn
}

// MarkRunning transitions to Running state.
func (a *Agent) MarkRunning() error {
	return a.Transition(StateRunning)
}

// MarkIdle transitions to Idle state.
func (a *Agent) MarkIdle() error {
	return a.Transition(StateIdle)
}

// MarkDone transitions to Done state.
func (a *Agent) MarkDone() error {
	return a.Transition(StateDone)
}

// MarkError transitions to Error state.
func (a *Agent) MarkError() error {
	return a.Transition(StateError)
}

// IsActive returns true if the agent is in Starting, Running, or Idle state.
func (a *Agent) IsActive() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.State == StateStarting || a.State == StateRunning || a.State == StateIdle
}

// IsTerminal returns true if the agent is in Done or Error state.
func (a *Agent) IsTerminal() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.State == StateDone || a.State == StateError
}

// CanAcceptInput returns true if the agent can receive PTY input.
func (a *Agent) CanAcceptInput() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.State == StateRunning || a.State == StateIdle
}

// Reset prepares the agent for reuse (after Done or Error).
// Returns to Starting state, clears task.
func (a *Agent) Reset() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.State != StateDone && a.State != StateError {
		return ErrAgentNotRunning
	}

	a.State = StateStarting
	a.Task = ""
	a.UpdatedAt = time.Now()
	return nil
}

// Info returns a snapshot of agent info for status reporting.
func (a *Agent) Info() AgentInfo {
	a.mu.RLock()
	defer a.mu.RUnlock()

	projectName := ""
	if a.Project != nil {
		projectName = a.Project.Name
	}

	worktreePath := ""
	if a.Worktree != nil {
		worktreePath = a.Worktree.Path
	}

	return AgentInfo{
		ID:        a.ID,
		Project:   projectName,
		Worktree:  worktreePath,
		State:     a.State,
		Task:      a.Task,
		StartedAt: a.StartedAt,
		UpdatedAt: a.UpdatedAt,
	}
}

// AgentInfo is a read-only snapshot of agent state for status reporting.
type AgentInfo struct {
	ID        string
	Project   string
	Worktree  string
	State     State
	Task      string
	StartedAt time.Time
	UpdatedAt time.Time
}

// DefaultPTYSize is the default terminal size for spawned PTYs.
var DefaultPTYSize = &pty.Winsize{
	Rows: 24,
	Cols: 80,
}

// Start spawns Claude Code in a PTY within the agent's worktree.
// The agent must be in Starting state.
func (a *Agent) Start(initialPrompt string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.State != StateStarting {
		return ErrInvalidTransition
	}
	if a.ptyFile != nil {
		return ErrPTYAlreadyStarted
	}

	// Determine working directory
	workDir := ""
	if a.Worktree != nil {
		workDir = a.Worktree.Path
	} else if a.Project != nil {
		workDir = a.Project.Path
	}

	// Build claude command with initial prompt
	args := []string{}
	if initialPrompt != "" {
		args = append(args, "-p", initialPrompt)
	}

	cmd := exec.Command("claude", args...)
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Set terminal size
	size := DefaultPTYSize
	if a.size != nil {
		size = a.size
	}

	// Start the PTY
	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return err
	}

	a.ptyFile = ptmx
	a.cmd = cmd
	a.size = size
	a.UpdatedAt = time.Now()

	return nil
}

// Stop terminates the Claude Code process and closes the PTY.
func (a *Agent) Stop() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.ptyFile == nil {
		return ErrPTYNotStarted
	}

	// Close PTY (this signals the process)
	if err := a.ptyFile.Close(); err != nil {
		// Continue cleanup even if close fails
	}

	// Wait for process to exit (with timeout handled externally if needed)
	if a.cmd != nil && a.cmd.Process != nil {
		_ = a.cmd.Process.Kill()
		_ = a.cmd.Wait()
	}

	a.ptyFile = nil
	a.cmd = nil
	a.UpdatedAt = time.Now()

	return nil
}

// Write sends input to the PTY.
func (a *Agent) Write(p []byte) (int, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.ptyFile == nil {
		return 0, ErrPTYNotStarted
	}
	return a.ptyFile.Write(p)
}

// Read reads output from the PTY.
func (a *Agent) Read(p []byte) (int, error) {
	a.mu.RLock()
	ptyFile := a.ptyFile
	a.mu.RUnlock()

	if ptyFile == nil {
		return 0, ErrPTYNotStarted
	}
	return ptyFile.Read(p)
}

// PTY returns the PTY file for direct access (e.g., io.Copy).
// Returns nil if the PTY hasn't been started.
func (a *Agent) PTY() io.ReadWriter {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.ptyFile == nil {
		return nil
	}
	return a.ptyFile
}

// Resize changes the PTY terminal size.
func (a *Agent) Resize(rows, cols uint16) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.ptyFile == nil {
		return ErrPTYNotStarted
	}

	newSize := &pty.Winsize{
		Rows: rows,
		Cols: cols,
	}

	if err := pty.Setsize(a.ptyFile, newSize); err != nil {
		return err
	}

	a.size = newSize
	return nil
}

// ProcessState returns the current state of the underlying process.
// Returns nil if the process hasn't started or has already been waited on.
func (a *Agent) ProcessState() *os.ProcessState {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.cmd == nil {
		return nil
	}
	return a.cmd.ProcessState
}

// PID returns the process ID of the Claude Code process.
// Returns -1 if the process hasn't started.
func (a *Agent) PID() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.cmd == nil || a.cmd.Process == nil {
		return -1
	}
	return a.cmd.Process.Pid
}

// Buffer returns the output ring buffer for direct access.
// The buffer is safe for concurrent use.
func (a *Agent) Buffer() *RingBuffer {
	return a.buffer
}

// Output returns the last n lines of captured output.
// If n <= 0, returns all captured output.
func (a *Agent) Output(n int) []byte {
	return a.buffer.Last(n)
}

// CaptureOutput writes data to both the PTY and the output buffer.
// This is typically called in the supervisor's read loop.
func (a *Agent) CaptureOutput(p []byte) {
	a.buffer.Write(p)
}

// SetDetector configures the pattern detector for done detection.
// Pass nil to disable detection.
func (a *Agent) SetDetector(d *Detector) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.detector = d
}

// Detector returns the current detector (may be nil).
func (a *Agent) Detector() *Detector {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.detector
}

// OnDoneDetect sets a callback that's invoked when a done pattern is detected.
// The callback receives the match information.
func (a *Agent) OnDoneDetect(fn func(match *Match)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.onDoneDetect = fn
}

// CheckDone checks the output buffer for completion patterns.
// Returns the match if a completion pattern is found, nil otherwise.
// Does not transition state - caller decides whether to mark done.
func (a *Agent) CheckDone() *Match {
	a.mu.RLock()
	detector := a.detector
	a.mu.RUnlock()

	if detector == nil {
		return nil
	}

	return detector.CheckBuffer(a.buffer)
}

// CheckDoneAndTransition checks for done patterns and transitions to Done state if found.
// Returns the match if detected and transition succeeded, nil otherwise.
// If a done callback is set, it's invoked before the transition.
func (a *Agent) CheckDoneAndTransition() *Match {
	match := a.CheckDone()
	if match == nil {
		return nil
	}

	// Get callback while holding read lock
	a.mu.RLock()
	callback := a.onDoneDetect
	a.mu.RUnlock()

	// Invoke callback before transition
	if callback != nil {
		callback(match)
	}

	// Attempt transition (may fail if state doesn't allow it)
	if err := a.MarkDone(); err != nil {
		return nil
	}

	return match
}

// CheckNewOutput checks only the most recent lines for completion patterns.
// More efficient than CheckDone when called frequently on new output.
// The numLines parameter specifies how many recent lines to check.
func (a *Agent) CheckNewOutput(numLines int) *Match {
	a.mu.RLock()
	detector := a.detector
	a.mu.RUnlock()

	if detector == nil {
		return nil
	}

	lines := a.buffer.Lines(numLines)
	return detector.CheckLines(lines)
}

// ReadLoopConfig configures the read loop behavior.
type ReadLoopConfig struct {
	// OnOutput is called whenever data is read from the PTY.
	// The callback receives the raw bytes read. It should not block.
	OnOutput func(data []byte)

	// OnError is called when a read error occurs (other than EOF).
	// If nil, errors are silently ignored.
	OnError func(err error)

	// BufferSize is the size of the read buffer. Default: 4096.
	BufferSize int

	// CheckDoneLines is the number of recent lines to check for done patterns.
	// Default: 5. Set to 0 to disable done detection in the read loop.
	CheckDoneLines int
}

// DefaultReadLoopConfig returns the default read loop configuration.
func DefaultReadLoopConfig() ReadLoopConfig {
	return ReadLoopConfig{
		BufferSize:     4096,
		CheckDoneLines: 5,
	}
}

// StartReadLoop starts a goroutine that continuously reads from the PTY.
// Data is captured to the ring buffer and the OnOutput callback is called.
// The loop runs until StopReadLoop is called or the PTY returns EOF/error.
// Returns an error if the read loop is already running or PTY is not started.
func (a *Agent) StartReadLoop(cfg ReadLoopConfig) error {
	a.readLoopMu.Lock()
	defer a.readLoopMu.Unlock()

	// Check if already running
	if a.readLoopStop != nil {
		select {
		case <-a.readLoopDone:
			// Previous loop finished, clean up
		default:
			return errors.New("read loop already running")
		}
	}

	// Verify PTY is started
	a.mu.RLock()
	ptyFile := a.ptyFile
	a.mu.RUnlock()

	if ptyFile == nil {
		return ErrPTYNotStarted
	}

	// Apply defaults
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 4096
	}
	if cfg.CheckDoneLines <= 0 {
		cfg.CheckDoneLines = 5
	}

	// Create control channels
	a.readLoopStop = make(chan struct{})
	a.readLoopDone = make(chan struct{})

	go a.runReadLoop(cfg)

	return nil
}

// runReadLoop is the main read loop goroutine.
func (a *Agent) runReadLoop(cfg ReadLoopConfig) {
	defer close(a.readLoopDone)

	buf := make([]byte, cfg.BufferSize)

	for {
		// Check for stop signal
		select {
		case <-a.readLoopStop:
			return
		default:
		}

		// Read from PTY (this may block)
		n, err := a.Read(buf)

		if n > 0 {
			data := buf[:n]

			// Capture to ring buffer
			a.CaptureOutput(data)

			// Call output callback
			if cfg.OnOutput != nil {
				cfg.OnOutput(data)
			}

			// Transition to running if we were starting
			if a.GetState() == StateStarting {
				_ = a.MarkRunning()
			}

			// Check for done patterns
			if cfg.CheckDoneLines > 0 {
				if match := a.CheckNewOutput(cfg.CheckDoneLines); match != nil {
					// Get callback while holding read lock
					a.mu.RLock()
					callback := a.onDoneDetect
					a.mu.RUnlock()

					// Invoke callback before transition
					if callback != nil {
						callback(match)
					}

					// Attempt transition
					_ = a.MarkDone()
					return
				}
			}
		}

		if err != nil {
			if err == io.EOF {
				// PTY closed, mark agent as done or error
				if a.IsActive() {
					_ = a.MarkDone()
				}
				return
			}

			// Handle read errors
			if cfg.OnError != nil {
				cfg.OnError(err)
			}

			// Check if we should stop
			select {
			case <-a.readLoopStop:
				return
			default:
			}

			// For other errors, check if PTY is still valid
			a.mu.RLock()
			ptyFile := a.ptyFile
			a.mu.RUnlock()

			if ptyFile == nil {
				return
			}
		}
	}
}

// StopReadLoop stops the read loop goroutine.
// It blocks until the loop has exited.
// Safe to call if the loop is not running.
func (a *Agent) StopReadLoop() {
	a.readLoopMu.Lock()
	stopCh := a.readLoopStop
	doneCh := a.readLoopDone
	a.readLoopMu.Unlock()

	if stopCh == nil {
		return
	}

	// Signal stop
	select {
	case <-stopCh:
		// Already closed
	default:
		close(stopCh)
	}

	// Wait for loop to exit
	if doneCh != nil {
		<-doneCh
	}
}

// IsReadLoopRunning returns true if the read loop is currently running.
func (a *Agent) IsReadLoopRunning() bool {
	a.readLoopMu.Lock()
	defer a.readLoopMu.Unlock()

	if a.readLoopStop == nil {
		return false
	}

	select {
	case <-a.readLoopDone:
		return false
	default:
		return true
	}
}
