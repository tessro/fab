// Package agent provides the Agent type and lifecycle management for Claude Code instances.
package agent

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"github.com/tessro/fab/internal/project"
)

// State represents the current state of an agent.
type State string

const (
	// StateStarting indicates the agent is initializing.
	StateStarting State = "starting"

	// StateRunning indicates the agent is actively processing (output detected).
	StateRunning State = "running"

	// StateIdle indicates the agent is waiting for input (no recent output).
	StateIdle State = "idle"

	// StateDone indicates the agent completed its task.
	StateDone State = "done"

	// StateError indicates the agent encountered an error or crashed.
	StateError State = "error"
)

// Mode controls how orchestrator actions are executed for an agent.
type Mode string

const (
	// ModeAuto executes orchestrator actions immediately without user confirmation.
	ModeAuto Mode = "auto"

	// ModeManual stages orchestrator actions for user confirmation via TUI.
	ModeManual Mode = "manual"
)

// DefaultMode is the default agent mode (manual for safety).
const DefaultMode = ModeManual

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
	ErrInvalidTransition  = errors.New("invalid state transition")
	ErrAgentNotRunning    = errors.New("agent is not running")
	ErrAgentAlreadyDone   = errors.New("agent has already completed")
	ErrProcessNotStarted  = errors.New("process has not been started")
	ErrProcessAlreadyRuns = errors.New("process is already running")
	ErrProcessExited      = errors.New("process exited unexpectedly")
)

// Agent represents a Claude Code instance with pipe-based I/O.
type Agent struct {
	ID        string            // Unique identifier (e.g., "a1b2c3")
	Project   *project.Project  // Parent project
	Worktree  *project.Worktree // Assigned worktree
	StartedAt time.Time         // When the agent was created

	// +checklocks:mu
	State State // Current state
	// +checklocks:mu
	Mode Mode // Orchestrator action mode (auto/manual)
	// +checklocks:mu
	Task string // Current task ID (e.g., "FAB-25")
	// +checklocks:mu
	UpdatedAt time.Time // Last state change

	// Process management with pipes
	// +checklocks:mu
	stdin io.WriteCloser // Pipe to send input to Claude Code
	// +checklocks:mu
	stdout io.ReadCloser // Pipe to read output from Claude Code
	// +checklocks:mu
	cmd *exec.Cmd // The Claude Code process

	// Chat history stores parsed messages for display/scrollback
	history *ChatHistory

	mu sync.RWMutex
	// +checklocks:mu
	onStateChange func(old, new State) // Optional callback for state changes

	// Read loop management (channels are goroutine-safe: created before goroutine, closed to signal)
	readLoopStop chan struct{} // Signals read loop to stop
	readLoopDone chan struct{} // Closed when read loop exits
	readLoopMu   sync.Mutex    // Protects starting/checking read loop state

	// Exit information
	// +checklocks:mu
	exitErr error // Error from process exit (nil for clean exit)
	// +checklocks:mu
	stopping bool // True when Stop() has been called
}

// New creates a new Agent in the Starting state with the default mode.
func New(id string, proj *project.Project, wt *project.Worktree) *Agent {
	now := time.Now()
	return &Agent{
		ID:        id,
		Project:   proj,
		Worktree:  wt,
		State:     StateStarting,
		Mode:      DefaultMode,
		StartedAt: now,
		UpdatedAt: now,
		history:   NewChatHistory(DefaultChatHistorySize),
	}
}

// GetState returns the current state (thread-safe).
func (a *Agent) GetState() State {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.State
}

// GetMode returns the current mode (thread-safe).
func (a *Agent) GetMode() Mode {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.Mode
}

// SetMode sets the orchestrator action mode.
func (a *Agent) SetMode(mode Mode) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Mode = mode
	a.UpdatedAt = time.Now()
}

// IsAutoMode returns true if the agent is in auto mode.
func (a *Agent) IsAutoMode() bool {
	return a.GetMode() == ModeAuto
}

// IsManualMode returns true if the agent is in manual mode.
func (a *Agent) IsManualMode() bool {
	return a.GetMode() == ModeManual
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

	if !a.canTransition(newState) {
		a.mu.Unlock()
		return ErrInvalidTransition
	}

	oldState := a.State
	a.State = newState
	a.UpdatedAt = time.Now()

	// Clear task on completion or error
	if newState == StateDone || newState == StateError {
		a.Task = ""
	}

	// Get callback before releasing lock
	callback := a.onStateChange
	a.mu.Unlock()

	// Call callback OUTSIDE the lock to prevent deadlock:
	// callback -> emit -> broadcast -> socket write (can block)
	// If we held the lock, Info() calls would block waiting for us
	if callback != nil {
		callback(oldState, newState)
	}

	return nil
}

// canTransition checks if transitioning to newState is valid.
//
// +checklocks:a.mu
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

// CanAcceptInput returns true if the agent can receive input.
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
		Mode:      a.Mode,
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
	Mode      Mode
	Task      string
	StartedAt time.Time
	UpdatedAt time.Time
}

// Start spawns Claude Code with pipe-based I/O within the agent's worktree.
// The agent must be in Starting state.
// If initialPrompt is provided, it will be sent as the first message.
func (a *Agent) Start(initialPrompt string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.State != StateStarting {
		return ErrInvalidTransition
	}
	if a.cmd != nil {
		return ErrProcessAlreadyRuns
	}

	// Determine working directory
	workDir := ""
	if a.Worktree != nil {
		workDir = a.Worktree.Path
	} else if a.Project != nil {
		workDir = a.Project.Path
	}

	// Build claude command with stream-json mode (no -p for multi-turn)
	// --verbose is required when using --output-format stream-json
	cmd := exec.Command("claude",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose")
	if workDir != "" {
		cmd.Dir = workDir
	}

	// Set environment variable for agent identification
	cmd.Env = append(os.Environ(), "FAB_AGENT_ID="+a.ID)

	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return err
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return err
	}

	a.stdin = stdin
	a.stdout = stdout
	a.cmd = cmd
	a.UpdatedAt = time.Now()

	// Send initial prompt if provided
	if initialPrompt != "" {
		if err := a.sendMessageLocked(initialPrompt); err != nil {
			// Log but don't fail - process is running
		}
	}

	return nil
}

// Stop terminates the Claude Code process and closes the pipes.
func (a *Agent) Stop() error {
	a.mu.Lock()

	if a.cmd == nil {
		a.mu.Unlock()
		return ErrProcessNotStarted
	}

	// Mark as stopping to prevent read loop from calling Wait()
	a.stopping = true

	// Close pipes (this signals the process)
	if a.stdin != nil {
		a.stdin.Close()
		a.stdin = nil
	}
	if a.stdout != nil {
		a.stdout.Close()
		a.stdout = nil
	}

	// Get process reference before releasing lock
	cmd := a.cmd
	a.cmd = nil
	a.UpdatedAt = time.Now()
	a.mu.Unlock()

	// Kill process - don't wait, cleanup happens asynchronously
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		// Reap the zombie in background to avoid resource leak
		go cmd.Wait()
	}

	return nil
}

// SendMessage sends a user message to Claude Code via stdin as JSON.
func (a *Agent) SendMessage(content string) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.sendMessageLocked(content)
}

// sendMessageLocked sends a message while holding the lock.
//
// +checklocks:a.mu
func (a *Agent) sendMessageLocked(content string) error {
	if a.stdin == nil {
		return ErrProcessNotStarted
	}

	msg := InputMessage{
		Type: "user",
		Message: MessageBody{
			Role:    "user",
			Content: content,
		},
		SessionID:       "default",
		ParentToolUseID: nil,
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Write JSON followed by newline
	data = append(data, '\n')
	_, err = a.stdin.Write(data)
	return err
}

// Write sends raw input to the process stdin.
// Deprecated: Use SendMessage for structured input.
func (a *Agent) Write(p []byte) (int, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.stdin == nil {
		return 0, ErrProcessNotStarted
	}
	return a.stdin.Write(p)
}

// Read reads raw output from the process stdout.
func (a *Agent) Read(p []byte) (int, error) {
	a.mu.RLock()
	stdout := a.stdout
	a.mu.RUnlock()

	if stdout == nil {
		return 0, ErrProcessNotStarted
	}
	return stdout.Read(p)
}

// Resize is a no-op for pipe-based I/O.
// Pipes don't have terminal dimensions.
func (a *Agent) Resize(rows, cols uint16) error {
	// No-op: pipes don't support terminal resizing
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

// ExitError returns the error from process exit, if any.
// Returns nil for clean exits (exit code 0) or if process is still running.
func (a *Agent) ExitError() error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.exitErr
}

// setExitError stores the exit error (called internally).
func (a *Agent) setExitError(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.exitErr = err
}

// waitForProcess waits for the process to exit and returns its exit status.
// Returns nil for clean exit, *exec.ExitError for non-zero exit, or other error.
func (a *Agent) waitForProcess() error {
	a.mu.RLock()
	cmd := a.cmd
	stopping := a.stopping
	a.mu.RUnlock()

	// If Stop() was called, it handles waiting for the process
	if stopping || cmd == nil {
		return nil
	}

	// Wait for process to exit (if not already waited)
	return cmd.Wait()
}

// ExitCode returns the exit code of the process, or -1 if not exited or error.
func (a *Agent) ExitCode() int {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if a.cmd == nil || a.cmd.ProcessState == nil {
		return -1
	}
	return a.cmd.ProcessState.ExitCode()
}

// IsCommandNotFound returns true if the exit error indicates the command wasn't found.
func (a *Agent) IsCommandNotFound() bool {
	a.mu.RLock()
	exitErr := a.exitErr
	a.mu.RUnlock()

	if exitErr == nil {
		return false
	}

	var exitError *exec.ExitError
	if errors.As(exitErr, &exitError) {
		// Exit code 127 is typically "command not found" in Unix shells
		// Exit code 126 is "command found but not executable"
		code := exitError.ExitCode()
		return code == 127 || code == 126
	}
	return false
}

// History returns the chat history for direct access.
// The history is safe for concurrent use.
func (a *Agent) History() *ChatHistory {
	return a.history
}

// Output returns the last n chat entries as formatted text.
// If n <= 0, returns all entries.
func (a *Agent) Output(n int) []byte {
	entries := a.history.Entries(n)
	var result []byte
	for _, entry := range entries {
		if entry.Content != "" {
			result = append(result, []byte(entry.Content+"\n")...)
		}
		if entry.ToolName != "" {
			result = append(result, []byte("["+entry.ToolName+"] "+entry.ToolInput+"\n")...)
		}
		if entry.ToolResult != "" {
			result = append(result, []byte(entry.ToolResult+"\n")...)
		}
	}
	return result
}

// AddChatEntry adds a parsed chat entry to the history.
// This is typically called by the read loop when parsing stream output.
func (a *Agent) AddChatEntry(entry ChatEntry) {
	a.history.Add(entry)
}

// ReadLoopConfig configures the read loop behavior.
type ReadLoopConfig struct {
	// OnEntry is called whenever a chat entry is parsed from stream output.
	// The callback receives the parsed entry. It should not block.
	OnEntry func(entry ChatEntry)

	// OnOutput is called with the raw JSONL data for each line.
	// This is useful for broadcasting raw output.
	OnOutput func(data []byte)

	// OnError is called when a read/parse error occurs (other than EOF).
	// If nil, errors are silently ignored.
	OnError func(err error)
}

// DefaultReadLoopConfig returns the default read loop configuration.
func DefaultReadLoopConfig() ReadLoopConfig {
	return ReadLoopConfig{}
}

// StartReadLoop starts a goroutine that continuously reads JSONL from stdout.
// Each line is parsed as a StreamMessage and converted to ChatEntry items.
// The loop runs until StopReadLoop is called or stdout returns EOF/error.
// Returns an error if the read loop is already running or process is not started.
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

	// Verify process is started
	a.mu.RLock()
	stdout := a.stdout
	a.mu.RUnlock()

	if stdout == nil {
		return ErrProcessNotStarted
	}

	// Create control channels
	a.readLoopStop = make(chan struct{})
	a.readLoopDone = make(chan struct{})

	go a.runReadLoop(cfg)

	return nil
}

// runReadLoop is the main read loop goroutine that parses JSONL output.
func (a *Agent) runReadLoop(cfg ReadLoopConfig) {
	defer close(a.readLoopDone)

	// Get stdout reference
	a.mu.RLock()
	stdout := a.stdout
	a.mu.RUnlock()

	if stdout == nil {
		return
	}

	scanner := bufio.NewScanner(stdout)

	for scanner.Scan() {
		// Check for stop signal
		select {
		case <-a.readLoopStop:
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Call raw output callback
		if cfg.OnOutput != nil {
			cfg.OnOutput(line)
		}

		// Parse the JSONL line as a StreamMessage
		msg, err := ParseStreamMessage(line)
		if err != nil {
			if cfg.OnError != nil {
				cfg.OnError(err)
			}
			continue
		}

		if msg == nil {
			continue
		}

		// Convert to chat entries and add to history
		entries := msg.ToChatEntries()
		for _, entry := range entries {
			a.AddChatEntry(entry)

			// Call entry callback
			if cfg.OnEntry != nil {
				cfg.OnEntry(entry)
			}
		}

		// Transition to running if we were starting
		if a.GetState() == StateStarting {
			_ = a.MarkRunning()
		}
	}

	// Scanner finished - check for errors or EOF
	if err := scanner.Err(); err != nil {
		if cfg.OnError != nil {
			cfg.OnError(err)
		}
	}

	// Stdout closed - wait for process and check exit status
	if a.IsActive() {
		exitErr := a.waitForProcess()
		if exitErr != nil {
			// Non-zero exit or signal - this is an error
			a.setExitError(exitErr)
			_ = a.MarkError()
		} else {
			// Clean exit (exit code 0)
			_ = a.MarkDone()
		}
	}
}

// StopReadLoop signals the read loop goroutine to stop.
// It does not wait for the loop to exit - cleanup happens asynchronously.
// Safe to call if the loop is not running.
func (a *Agent) StopReadLoop() {
	a.readLoopMu.Lock()
	stopCh := a.readLoopStop
	a.readLoopMu.Unlock()

	if stopCh == nil {
		return
	}

	// Signal stop (non-blocking)
	select {
	case <-stopCh:
		// Already closed
	default:
		close(stopCh)
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
