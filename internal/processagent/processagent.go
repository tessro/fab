package processagent

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/logging"
)

// Errors returned by process agent operations.
var (
	ErrAlreadyRunning  = errors.New("process agent is already running")
	ErrNotRunning      = errors.New("process agent is not running")
	ErrProcessNotFound = errors.New("process not found")
	ErrShuttingDown    = errors.New("process agent is shutting down")
)

// StopTimeout is the default duration to wait for graceful shutdown.
const StopTimeout = 5 * time.Second

// Config configures a ProcessAgent.
type Config struct {
	// WorkDir is the working directory for the process.
	WorkDir string

	// BuildCommand is called to create the exec.Cmd to run.
	// The returned command should NOT be started yet.
	BuildCommand func() (*exec.Cmd, error)

	// BuildResumeCommand is called to create an exec.Cmd for resuming a conversation.
	// It receives the thread ID and the message content.
	// If nil, follow-up messages are sent via stdin to the running process.
	// For backends like Codex that use separate processes per turn, this allows
	// spawning a new "exec resume" process.
	BuildResumeCommand func(threadID, message string) (*exec.Cmd, error)

	// InitialPrompt is sent via stdin after the process starts.
	// If empty, no initial prompt is sent.
	InitialPrompt string

	// LargeBuffer enables a larger scanner buffer for reading stdout.
	// Use this when expecting large JSONL messages (e.g., file reads).
	LargeBuffer bool

	// LogStderr if true logs stderr output to slog.
	LogStderr bool

	// LogPrefix is the prefix for log messages (e.g., "manager" or "planner").
	LogPrefix string

	// ProcessMessage is called for each parsed StreamMessage.
	// Return true to stop the read loop early.
	ProcessMessage func(msg *agent.StreamMessage) bool

	// ParseMessage parses a JSONL line from stdout into a StreamMessage.
	// If nil, uses the default Claude backend parser.
	ParseMessage func(line []byte) (*agent.StreamMessage, error)
}

// ProcessAgent manages a Claude Code subprocess with pipe-based I/O.
type ProcessAgent struct {
	mu sync.RWMutex

	// +checklocks:mu
	state State
	// +checklocks:mu
	cmd *exec.Cmd
	// +checklocks:mu
	stdin io.WriteCloser
	// +checklocks:mu
	stdout io.ReadCloser
	// +checklocks:mu
	startedAt time.Time
	// +checklocks:mu
	threadID string // Thread ID for conversation resumption (Codex)

	// Chat history for TUI display
	history *agent.ChatHistory

	// Working directory for the agent
	workDir string

	// Configuration
	config Config

	// Callbacks
	// +checklocks:mu
	onStateChange func(old, new State)
	// +checklocks:mu
	onEntry func(entry agent.ChatEntry)
	// +checklocks:mu
	onThreadIDChange func(threadID string)

	// Read loop control
	readLoopStop chan struct{}
	readLoopDone chan struct{}
}

// New creates a new ProcessAgent with the given configuration.
func New(config Config) *ProcessAgent {
	return &ProcessAgent{
		state:   StateStopped,
		workDir: config.WorkDir,
		config:  config,
		history: agent.NewChatHistory(agent.DefaultChatHistorySize),
	}
}

// State returns the current process state.
func (p *ProcessAgent) State() State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// IsRunning returns true if the process is running or starting.
func (p *ProcessAgent) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state == StateRunning || p.state == StateStarting
}

// StartedAt returns when the process was started.
func (p *ProcessAgent) StartedAt() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.startedAt
}

// History returns the chat history.
func (p *ProcessAgent) History() *agent.ChatHistory {
	return p.history
}

// WorkDir returns the working directory.
func (p *ProcessAgent) WorkDir() string {
	return p.workDir
}

// ThreadID returns the current thread ID for conversation resumption.
func (p *ProcessAgent) ThreadID() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.threadID
}

// SetThreadID sets the thread ID for conversation resumption.
func (p *ProcessAgent) SetThreadID(id string) {
	p.mu.Lock()
	p.threadID = id
	callback := p.onThreadIDChange
	p.mu.Unlock()

	// Call callback OUTSIDE the lock to prevent deadlock
	if callback != nil {
		callback(id)
	}
}

// OnThreadIDChange sets a callback for thread ID changes.
func (p *ProcessAgent) OnThreadIDChange(fn func(threadID string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onThreadIDChange = fn
}

// OnStateChange sets a callback for state changes.
func (p *ProcessAgent) OnStateChange(fn func(old, new State)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onStateChange = fn
}

// OnEntry sets a callback for chat entries.
func (p *ProcessAgent) OnEntry(fn func(entry agent.ChatEntry)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onEntry = fn
}

// setState changes the state and calls the callback.
func (p *ProcessAgent) setState(new State) {
	p.mu.Lock()
	old := p.state
	p.state = new
	callback := p.onStateChange
	p.mu.Unlock()

	if callback != nil && old != new {
		callback(old, new)
	}
}

// Start spawns the Claude Code process.
func (p *ProcessAgent) Start() error {
	log := slog.With("component", p.config.LogPrefix)

	log.Debug("ProcessAgent.Start: beginning", "workdir", p.workDir)
	p.mu.Lock()

	if p.state != StateStopped {
		currentState := p.state
		p.mu.Unlock()
		log.Debug("ProcessAgent.Start: already running", "state", currentState)
		return ErrAlreadyRunning
	}

	p.state = StateStarting
	p.startedAt = time.Now()

	// Clear history for fresh session
	p.history = agent.NewChatHistory(agent.DefaultChatHistorySize)

	// Ensure work directory exists
	log.Debug("ProcessAgent.Start: creating work dir", "workdir", p.workDir)
	if err := os.MkdirAll(p.workDir, 0755); err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("ProcessAgent.Start: failed to create work dir", "error", err)
		return fmt.Errorf("create work dir: %w", err)
	}

	// Build the command
	log.Debug("ProcessAgent.Start: building command")
	cmd, err := p.config.BuildCommand()
	if err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("ProcessAgent.Start: failed to build command", "error", err)
		return fmt.Errorf("build command: %w", err)
	}
	cmd.Dir = p.workDir

	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("ProcessAgent.Start: stdin pipe failed", "error", err)
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("ProcessAgent.Start: stdout pipe failed", "error", err)
		return fmt.Errorf("stdout pipe: %w", err)
	}

	// Set up stderr if logging is requested
	var stderr io.ReadCloser
	if p.config.LogStderr {
		stderr, err = cmd.StderrPipe()
		if err != nil {
			stdin.Close()
			stdout.Close()
			p.state = StateStopped
			p.mu.Unlock()
			log.Error("ProcessAgent.Start: stderr pipe failed", "error", err)
			return fmt.Errorf("stderr pipe: %w", err)
		}
	}

	// Start the process
	log.Debug("ProcessAgent.Start: starting process", "cmd", cmd.Path, "dir", cmd.Dir)
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		if stderr != nil {
			stderr.Close()
		}
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("ProcessAgent.Start: process start failed", "error", err)
		return fmt.Errorf("start process: %w", err)
	}

	// Start stderr reader goroutine if logging is enabled
	if stderr != nil {
		stderrLog := log
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				stderrLog.Warn(p.config.LogPrefix+".stderr", "line", line)
			}
		}()
	}
	log.Debug("ProcessAgent.Start: process started", "pid", cmd.Process.Pid)

	p.cmd = cmd
	p.stdin = stdin
	p.stdout = stdout

	// Create read loop channels
	p.readLoopStop = make(chan struct{})
	p.readLoopDone = make(chan struct{})

	p.mu.Unlock()

	// Send initial prompt via stdin if configured
	if p.config.InitialPrompt != "" {
		log.Debug("ProcessAgent.Start: sending initial prompt via stdin")
		if err := p.SendMessage(p.config.InitialPrompt); err != nil {
			log.Error("ProcessAgent.Start: failed to send initial prompt", "error", err)
			// Don't fail - process is running, just log the error
		}
	}

	// Start read loop
	log.Debug("ProcessAgent.Start: starting read loop goroutine")
	go p.runReadLoop()

	p.setState(StateRunning)
	log.Info("ProcessAgent.Start: complete", "pid", cmd.Process.Pid)
	return nil
}

// Resume restarts a stopped process without clearing history.
// This allows continuing an ongoing conversation after the underlying process exits.
// If the process is already running, returns ErrAlreadyRunning.
func (p *ProcessAgent) Resume() error {
	log := slog.With("component", p.config.LogPrefix)

	log.Debug("ProcessAgent.Resume: beginning", "workdir", p.workDir)
	p.mu.Lock()

	if p.state != StateStopped {
		currentState := p.state
		p.mu.Unlock()
		log.Debug("ProcessAgent.Resume: already running", "state", currentState)
		return ErrAlreadyRunning
	}

	p.state = StateStarting
	// Keep existing startedAt and history - don't reset them

	// Ensure work directory exists
	log.Debug("ProcessAgent.Resume: creating work dir", "workdir", p.workDir)
	if err := os.MkdirAll(p.workDir, 0755); err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("ProcessAgent.Resume: failed to create work dir", "error", err)
		return fmt.Errorf("create work dir: %w", err)
	}

	// Build the command
	log.Debug("ProcessAgent.Resume: building command")
	cmd, err := p.config.BuildCommand()
	if err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("ProcessAgent.Resume: failed to build command", "error", err)
		return fmt.Errorf("build command: %w", err)
	}
	cmd.Dir = p.workDir

	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("ProcessAgent.Resume: stdin pipe failed", "error", err)
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("ProcessAgent.Resume: stdout pipe failed", "error", err)
		return fmt.Errorf("stdout pipe: %w", err)
	}

	// Set up stderr if logging is requested
	var stderr io.ReadCloser
	if p.config.LogStderr {
		stderr, err = cmd.StderrPipe()
		if err != nil {
			stdin.Close()
			stdout.Close()
			p.state = StateStopped
			p.mu.Unlock()
			log.Error("ProcessAgent.Resume: stderr pipe failed", "error", err)
			return fmt.Errorf("stderr pipe: %w", err)
		}
	}

	// Start the process
	log.Debug("ProcessAgent.Resume: starting process", "cmd", cmd.Path, "dir", cmd.Dir)
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		if stderr != nil {
			stderr.Close()
		}
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("ProcessAgent.Resume: process start failed", "error", err)
		return fmt.Errorf("start process: %w", err)
	}

	// Start stderr reader goroutine if logging is enabled
	if stderr != nil {
		stderrLog := log
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				stderrLog.Warn(p.config.LogPrefix+".stderr", "line", line)
			}
		}()
	}
	log.Debug("ProcessAgent.Resume: process started", "pid", cmd.Process.Pid)

	p.cmd = cmd
	p.stdin = stdin
	p.stdout = stdout

	// Create read loop channels
	p.readLoopStop = make(chan struct{})
	p.readLoopDone = make(chan struct{})

	p.mu.Unlock()

	// Note: Don't send initial prompt on resume - this is a continuation

	// Start read loop
	log.Debug("ProcessAgent.Resume: starting read loop goroutine")
	go p.runReadLoop()

	p.setState(StateRunning)
	log.Info("ProcessAgent.Resume: complete", "pid", cmd.Process.Pid)
	return nil
}

// Stop gracefully stops the process agent.
func (p *ProcessAgent) Stop() error {
	return p.StopWithTimeout(StopTimeout)
}

// StopWithTimeout stops the process with a custom timeout.
func (p *ProcessAgent) StopWithTimeout(timeout time.Duration) error {
	p.mu.Lock()

	if p.state == StateStopped {
		p.mu.Unlock()
		return ErrNotRunning
	}

	if p.state == StateStopping {
		p.mu.Unlock()
		return ErrShuttingDown
	}

	p.state = StateStopping

	// Signal read loop to stop
	if p.readLoopStop != nil {
		close(p.readLoopStop)
	}

	// Close pipes
	if p.stdin != nil {
		p.stdin.Close()
		p.stdin = nil
	}
	if p.stdout != nil {
		p.stdout.Close()
		p.stdout = nil
	}

	cmd := p.cmd
	p.cmd = nil
	p.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		p.setState(StateStopped)
		return nil
	}

	// Try graceful termination
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = cmd.Wait()
		p.setState(StateStopped)
		return nil
	}

	// Wait with timeout
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-done:
		// Clean exit
	case <-time.After(timeout):
		slog.Debug(p.config.LogPrefix+" did not exit gracefully, sending SIGKILL", "timeout", timeout)
		_ = cmd.Process.Kill()
		<-done
	}

	p.setState(StateStopped)
	return nil
}

// SendMessage sends a user message to the process.
// For backends with continuous stdin (Claude Code), the message is written to stdin.
// For backends that require separate processes per turn (Codex), this spawns a
// resume process if a thread ID is available and the process has stopped.
func (p *ProcessAgent) SendMessage(content string) error {
	p.mu.RLock()
	stdin := p.stdin
	state := p.state
	threadID := p.threadID
	p.mu.RUnlock()

	// If process is running, send via stdin (Claude Code style)
	if state == StateRunning || state == StateStarting {
		if stdin == nil {
			return ErrProcessNotFound
		}

		msg := agent.InputMessage{
			Type: "user",
			Message: agent.MessageBody{
				Role:    "user",
				Content: content,
			},
			SessionID: "default",
		}

		data, err := json.Marshal(msg)
		if err != nil {
			return err
		}

		data = append(data, '\n')
		_, err = stdin.Write(data)
		return err
	}

	// Process not running - check if we can resume (Codex style)
	if p.config.BuildResumeCommand != nil && threadID != "" {
		return p.resumeWithMessage(threadID, content)
	}

	return ErrNotRunning
}

// resumeWithMessage spawns a new process to resume a conversation with a message.
// This is used by Codex which requires separate processes per turn.
func (p *ProcessAgent) resumeWithMessage(threadID, content string) error {
	log := slog.With("component", p.config.LogPrefix)
	log.Debug("ProcessAgent.resumeWithMessage: resuming conversation", "thread_id", threadID)

	p.mu.Lock()

	if p.state != StateStopped {
		p.mu.Unlock()
		return ErrAlreadyRunning
	}

	p.state = StateStarting

	// Build resume command
	cmd, err := p.config.BuildResumeCommand(threadID, content)
	if err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		return fmt.Errorf("build resume command: %w", err)
	}
	cmd.Dir = p.workDir

	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		p.state = StateStopped
		p.mu.Unlock()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	// Set up stderr if logging is requested
	var stderr io.ReadCloser
	if p.config.LogStderr {
		stderr, err = cmd.StderrPipe()
		if err != nil {
			stdin.Close()
			stdout.Close()
			p.state = StateStopped
			p.mu.Unlock()
			return fmt.Errorf("stderr pipe: %w", err)
		}
	}

	// Start the process
	log.Debug("ProcessAgent.resumeWithMessage: starting resume process", "cmd", cmd.Path, "dir", cmd.Dir)
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		if stderr != nil {
			stderr.Close()
		}
		p.state = StateStopped
		p.mu.Unlock()
		return fmt.Errorf("start process: %w", err)
	}

	// Start stderr reader goroutine if logging is enabled
	if stderr != nil {
		stderrLog := log
		go func() {
			scanner := bufio.NewScanner(stderr)
			for scanner.Scan() {
				line := scanner.Text()
				stderrLog.Warn(p.config.LogPrefix+".stderr", "line", line)
			}
		}()
	}
	log.Debug("ProcessAgent.resumeWithMessage: process started", "pid", cmd.Process.Pid)

	p.cmd = cmd
	p.stdin = stdin
	p.stdout = stdout

	// Create new read loop channels
	p.readLoopStop = make(chan struct{})
	p.readLoopDone = make(chan struct{})

	p.mu.Unlock()

	// Start read loop
	go p.runReadLoop()

	p.setState(StateRunning)
	log.Info("ProcessAgent.resumeWithMessage: complete", "pid", cmd.Process.Pid, "thread_id", threadID)
	return nil
}

// runReadLoop reads and parses output from Claude Code.
func (p *ProcessAgent) runReadLoop() {
	defer logging.LogPanic(p.config.LogPrefix+"-read-loop", nil)
	defer close(p.readLoopDone)

	log := slog.With("component", p.config.LogPrefix)

	log.Debug("ProcessAgent.runReadLoop: starting")

	p.mu.RLock()
	stdout := p.stdout
	p.mu.RUnlock()

	if stdout == nil {
		log.Warn("ProcessAgent.runReadLoop: stdout is nil, exiting")
		return
	}

	scanner := bufio.NewScanner(stdout)
	if p.config.LargeBuffer {
		scanner.Buffer(make([]byte, 0, 64*1024), agent.MaxScanTokenSize)
	}
	lineCount := 0
	log.Debug("ProcessAgent.runReadLoop: waiting for first line from claude")

	for scanner.Scan() {
		select {
		case <-p.readLoopStop:
			log.Debug("ProcessAgent.runReadLoop: stop signal received", "lines_read", lineCount)
			return
		default:
		}

		line := scanner.Bytes()
		lineCount++

		if lineCount == 1 {
			log.Debug("ProcessAgent.runReadLoop: received first line", "len", len(line))
		}

		if len(line) == 0 {
			continue
		}

		// Parse stream message using configured parser or default
		var msg *agent.StreamMessage
		var err error
		if p.config.ParseMessage != nil {
			msg, err = p.config.ParseMessage(line)
		} else {
			msg, err = agent.ParseStreamMessage(line)
		}
		if err != nil {
			log.Warn("ProcessAgent.runReadLoop: parse error", "error", err, "line_num", lineCount)
			continue
		}

		if msg == nil {
			continue
		}

		// Capture thread ID from system init messages (Codex thread.started)
		if msg.ThreadID != "" {
			p.SetThreadID(msg.ThreadID)
			log.Debug("ProcessAgent.runReadLoop: captured thread ID", "thread_id", msg.ThreadID)
		}

		// Let embedder process the message first
		if p.config.ProcessMessage != nil {
			if p.config.ProcessMessage(msg) {
				// Embedder requested stop
				log.Debug("ProcessAgent.runReadLoop: embedder requested stop", "lines_read", lineCount)
				return
			}
		}

		// Convert to chat entries
		entries := msg.ToChatEntries()
		for _, entry := range entries {
			p.history.Add(entry)

			// Call entry callback
			p.mu.RLock()
			callback := p.onEntry
			p.mu.RUnlock()

			if callback != nil {
				callback(entry)
			}
		}
	}

	// Scanner finished - process likely exited
	scanErr := scanner.Err()
	log.Debug("ProcessAgent.runReadLoop: scanner finished", "lines_read", lineCount, "error", scanErr)

	p.mu.RLock()
	wasRunning := p.state == StateRunning
	p.mu.RUnlock()

	if wasRunning {
		p.setState(StateStopped)
	}
}
