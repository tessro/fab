// Package manager provides the manager agent for interactive user conversation.
// The manager agent is a dedicated Claude Code instance that knows about all
// registered projects and can invoke fab CLI commands to coordinate work.
package manager

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/logging"
)

// Errors returned by manager operations.
var (
	ErrAlreadyRunning  = errors.New("manager agent is already running")
	ErrNotRunning      = errors.New("manager agent is not running")
	ErrProcessNotFound = errors.New("manager process not found")
	ErrShuttingDown    = errors.New("manager agent is shutting down")
)

// StopTimeout is the duration to wait for graceful shutdown.
const StopTimeout = 5 * time.Second

// State represents the manager agent state.
type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
)

// Manager is the manager agent that coordinates user interaction across all projects.
type Manager struct {
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

	// Chat history for TUI display
	history *agent.ChatHistory

	// Working directory for the manager
	workDir string

	// AllowedPatterns are Bash command patterns allowed without prompting.
	// Uses fab pattern syntax (e.g., "fab:*" for prefix match).
	allowedPatterns []string

	// Callbacks
	// +checklocks:mu
	onStateChange func(old, new State)
	// +checklocks:mu
	onEntry func(entry agent.ChatEntry)

	// Read loop control
	readLoopStop chan struct{}
	readLoopDone chan struct{}
}

// New creates a new manager agent.
// allowedPatterns specifies Bash command patterns that are allowed without prompting.
// Uses fab pattern syntax (e.g., "fab:*" for prefix match).
func New(workDir string, allowedPatterns []string) *Manager {
	return &Manager{
		state:           StateStopped,
		workDir:         workDir,
		allowedPatterns: allowedPatterns,
		history:         agent.NewChatHistory(agent.DefaultChatHistorySize),
	}
}

// DefaultWorkDir returns the default working directory for the manager.
func DefaultWorkDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".fab", "manager")
}

// State returns the current manager state.
func (m *Manager) State() State {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state
}

// IsRunning returns true if the manager is running.
func (m *Manager) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.state == StateRunning || m.state == StateStarting
}

// StartedAt returns when the manager was started.
func (m *Manager) StartedAt() time.Time {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.startedAt
}

// History returns the chat history.
func (m *Manager) History() *agent.ChatHistory {
	return m.history
}

// OnStateChange sets a callback for state changes.
func (m *Manager) OnStateChange(fn func(old, new State)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onStateChange = fn
}

// OnEntry sets a callback for chat entries.
func (m *Manager) OnEntry(fn func(entry agent.ChatEntry)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.onEntry = fn
}

// setState changes the state and calls the callback.
func (m *Manager) setState(new State) {
	m.mu.Lock()
	old := m.state
	m.state = new
	callback := m.onStateChange
	m.mu.Unlock()

	if callback != nil && old != new {
		callback(old, new)
	}
}

// buildSettings creates the Claude Code settings with allowed tool permissions.
// It converts fab pattern syntax (e.g., "fab:*") to Claude Code format (e.g., "Bash(fab *)").
func (m *Manager) buildSettings() map[string]any {
	allowedTools := m.buildAllowedTools()
	return map[string]any{
		"permissions": map[string]any{
			"allow": allowedTools,
		},
	}
}

// buildAllowedTools converts fab patterns to Claude Code allowedTools format.
// Fab patterns use the format "prefix:*" for prefix matching.
// Claude Code uses "Bash(prefix *)" for Bash command prefix matching.
func (m *Manager) buildAllowedTools() []string {
	var tools []string

	for _, pattern := range m.allowedPatterns {
		tool := convertPatternToClaudeCode(pattern)
		if tool != "" {
			tools = append(tools, tool)
		}
	}

	return tools
}

// convertPatternToClaudeCode converts a fab permission pattern to Claude Code format.
// Fab pattern syntax:
//   - "prefix:*" matches commands starting with "prefix"
//   - "exact" matches the exact string
//
// Claude Code format:
//   - "Bash(prefix *)" matches Bash commands starting with "prefix"
//   - "Bash(exact)" matches exact Bash command
func convertPatternToClaudeCode(pattern string) string {
	if pattern == "" {
		return ""
	}

	// Handle prefix patterns (ends with :*)
	if len(pattern) > 2 && pattern[len(pattern)-2:] == ":*" {
		prefix := pattern[:len(pattern)-2]
		return fmt.Sprintf("Bash(%s *)", prefix)
	}

	// Handle catch-all pattern
	if pattern == ":*" {
		return "Bash(*)"
	}

	// Exact match
	return fmt.Sprintf("Bash(%s)", pattern)
}

// Start spawns the manager Claude Code instance.
func (m *Manager) Start() error {
	m.mu.Lock()

	if m.state != StateStopped {
		m.mu.Unlock()
		return ErrAlreadyRunning
	}

	m.state = StateStarting
	m.startedAt = time.Now()

	// Clear history for fresh session
	m.history = agent.NewChatHistory(agent.DefaultChatHistorySize)

	// Ensure work directory exists
	if err := os.MkdirAll(m.workDir, 0755); err != nil {
		m.state = StateStopped
		m.mu.Unlock()
		return fmt.Errorf("create work dir: %w", err)
	}

	// Get fab binary path for the system prompt
	fabPath, err := os.Executable()
	if err != nil {
		fabPath = "fab"
	}

	// Build system prompt that makes the manager fab-aware
	systemPrompt := buildManagerSystemPrompt(fabPath)

	// Build settings with allowed tools based on configured patterns
	settings := m.buildSettings()
	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		m.state = StateStopped
		m.mu.Unlock()
		return fmt.Errorf("marshal settings: %w", err)
	}

	// Build claude command
	cmd := exec.Command("claude",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "default",
		"--settings", string(settingsJSON),
		"-p", systemPrompt)
	cmd.Dir = m.workDir

	// Set environment
	cmd.Env = append(os.Environ(), "FAB_MANAGER=1")

	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		m.state = StateStopped
		m.mu.Unlock()
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		m.state = StateStopped
		m.mu.Unlock()
		return fmt.Errorf("stdout pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		m.state = StateStopped
		m.mu.Unlock()
		return fmt.Errorf("start process: %w", err)
	}

	m.cmd = cmd
	m.stdin = stdin
	m.stdout = stdout

	// Create read loop channels
	m.readLoopStop = make(chan struct{})
	m.readLoopDone = make(chan struct{})

	m.mu.Unlock()

	// Start read loop
	go m.runReadLoop()

	m.setState(StateRunning)
	return nil
}

// Stop gracefully stops the manager agent.
func (m *Manager) Stop() error {
	return m.StopWithTimeout(StopTimeout)
}

// StopWithTimeout stops the manager with a custom timeout.
func (m *Manager) StopWithTimeout(timeout time.Duration) error {
	m.mu.Lock()

	if m.state == StateStopped {
		m.mu.Unlock()
		return ErrNotRunning
	}

	if m.state == StateStopping {
		m.mu.Unlock()
		return ErrShuttingDown
	}

	m.state = StateStopping

	// Signal read loop to stop
	if m.readLoopStop != nil {
		close(m.readLoopStop)
	}

	// Close pipes
	if m.stdin != nil {
		m.stdin.Close()
		m.stdin = nil
	}
	if m.stdout != nil {
		m.stdout.Close()
		m.stdout = nil
	}

	cmd := m.cmd
	m.cmd = nil
	m.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		m.setState(StateStopped)
		return nil
	}

	// Try graceful termination
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		_ = cmd.Wait()
		m.setState(StateStopped)
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
		slog.Debug("manager did not exit gracefully, sending SIGKILL", "timeout", timeout)
		_ = cmd.Process.Kill()
		<-done
	}

	m.setState(StateStopped)
	return nil
}

// SendMessage sends a user message to the manager.
func (m *Manager) SendMessage(content string) error {
	m.mu.RLock()
	stdin := m.stdin
	state := m.state
	m.mu.RUnlock()

	if state != StateRunning {
		return ErrNotRunning
	}

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

// runReadLoop reads and parses output from Claude Code.
func (m *Manager) runReadLoop() {
	defer logging.LogPanic("manager-read-loop", nil)
	defer close(m.readLoopDone)

	m.mu.RLock()
	stdout := m.stdout
	m.mu.RUnlock()

	if stdout == nil {
		return
	}

	scanner := bufio.NewScanner(stdout)

	for scanner.Scan() {
		select {
		case <-m.readLoopStop:
			return
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Parse stream message
		msg, err := agent.ParseStreamMessage(line)
		if err != nil {
			slog.Warn("manager readloop: parse error", "error", err)
			continue
		}

		if msg == nil {
			continue
		}

		// Convert to chat entries
		entries := msg.ToChatEntries()
		for _, entry := range entries {
			m.history.Add(entry)

			// Call entry callback
			m.mu.RLock()
			callback := m.onEntry
			m.mu.RUnlock()

			if callback != nil {
				callback(entry)
			}
		}
	}

	// Scanner finished - process likely exited
	m.mu.RLock()
	wasRunning := m.state == StateRunning
	m.mu.RUnlock()

	if wasRunning {
		m.setState(StateStopped)
	}
}

// buildManagerSystemPrompt creates the system prompt for the manager agent.
func buildManagerSystemPrompt(fabPath string) string {
	return fmt.Sprintf(`You are a fab manager agent - a helpful assistant that coordinates work across multiple Claude Code agent instances.

## Your Capabilities

You have access to the following tools via the command line:

### fab CLI (Agent Supervisor)
- %s status - View status of all projects and agents
- %s project list - List all registered projects
- %s project start <name> - Start orchestration for a project
- %s project stop <name> - Stop orchestration for a project
- %s agent list - List all running agents
- %s claims list - List all claimed tickets

### fab issue (Issue Management)
- fab issue list - List all issues
- fab issue ready - List issues ready to be worked on
- fab issue show <id> - Show issue details
- fab issue create <title> - Create a new issue
- fab issue close <id> - Close an issue
- fab issue update <id> - Update an issue

### Filing Issues

When the user identifies work that needs to be done, you can file issues to track it:

` + "`" + `fab issue create "title" --type <type> --priority <priority> --description "description"` + "`" + `

Issue types:
- task: General work items
- feature: New functionality
- bug: Something that needs fixing
- chore: Maintenance work (cleanup, refactoring, etc.)

Priority levels:
- 0: Low priority
- 1: Medium priority (default)
- 2: High priority

Example:
` + "`" + `fab issue create "Add user authentication" --type feature --priority 2 --description "Implement OAuth2 login flow"` + "`" + `

File issues proactively when:
- The user mentions something that should be done later
- You identify technical debt or improvements while reviewing status
- A user request is too large and should be broken into smaller tasks

## Your Role

1. **Status Overview**: Help users understand what's happening across their agent fleet
2. **Work Coordination**: Help users manage which projects and tickets are being worked on
3. **Issue Triage**: Help users prioritize and assign work
4. **Troubleshooting**: Help diagnose issues with agents or projects

## Guidelines

- Use the Bash tool to run fab commands to gather information
- Be concise and helpful
- When showing status, format it clearly for readability
- Proactively suggest actions when appropriate
- If asked to start work on something, use the appropriate fab commands

## Examples

User: "What's the status of all agents?"
→ Run: %s status

User: "Show me blocked issues"
→ Run: fab issue list --status blocked

User: "Start working on the fab project"
→ Run: %s project start fab
`, fabPath, fabPath, fabPath, fabPath, fabPath, fabPath, fabPath, fabPath)
}
