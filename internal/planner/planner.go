// Package planner provides planning agents for creating implementation plans.
// Planning agents are specialized Claude Code instances that run in plan mode
// and write their plans to .fab/plans/ when they exit via ExitPlanMode.
package planner

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

// Errors returned by planner operations.
var (
	ErrAlreadyRunning  = errors.New("planner is already running")
	ErrNotRunning      = errors.New("planner is not running")
	ErrProcessNotFound = errors.New("planner process not found")
	ErrShuttingDown    = errors.New("planner is shutting down")
)

// StopTimeout is the duration to wait for graceful shutdown.
const StopTimeout = 5 * time.Second

// State represents the planner agent state.
type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
)

// Planner is a planning agent that creates implementation plans.
type Planner struct {
	mu sync.RWMutex

	// Identification
	id      string // Unique identifier
	project string // Project name (optional)

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

	// Working directory for the planner
	workDir string

	// Initial prompt for the planning task
	prompt string

	// Plan file path (set when ExitPlanMode is detected)
	// +checklocks:mu
	planFile string

	// Callbacks
	// +checklocks:mu
	onStateChange func(old, new State)
	// +checklocks:mu
	onEntry func(entry agent.ChatEntry)
	// +checklocks:mu
	onPlanComplete func(planFile string)

	// Read loop control
	readLoopStop chan struct{}
	readLoopDone chan struct{}
}

// New creates a new planner.
func New(id, project, workDir, prompt string) *Planner {
	return &Planner{
		id:      id,
		project: project,
		state:   StateStopped,
		workDir: workDir,
		prompt:  prompt,
		history: agent.NewChatHistory(agent.DefaultChatHistorySize),
	}
}

// ID returns the planner's unique identifier.
func (p *Planner) ID() string {
	return p.id
}

// Project returns the planner's project name.
func (p *Planner) Project() string {
	return p.project
}

// State returns the current planner state.
func (p *Planner) State() State {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// IsRunning returns true if the planner is running.
func (p *Planner) IsRunning() bool {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state == StateRunning || p.state == StateStarting
}

// StartedAt returns when the planner was started.
func (p *Planner) StartedAt() time.Time {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.startedAt
}

// History returns the chat history.
func (p *Planner) History() *agent.ChatHistory {
	return p.history
}

// WorkDir returns the working directory.
func (p *Planner) WorkDir() string {
	return p.workDir
}

// PlanFile returns the path to the plan file, if generated.
func (p *Planner) PlanFile() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.planFile
}

// OnStateChange sets a callback for state changes.
func (p *Planner) OnStateChange(fn func(old, new State)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onStateChange = fn
}

// OnEntry sets a callback for chat entries.
func (p *Planner) OnEntry(fn func(entry agent.ChatEntry)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onEntry = fn
}

// OnPlanComplete sets a callback for when the plan is complete.
func (p *Planner) OnPlanComplete(fn func(planFile string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onPlanComplete = fn
}

// setState changes the state and calls the callback.
func (p *Planner) setState(new State) {
	p.mu.Lock()
	old := p.state
	p.state = new
	callback := p.onStateChange
	p.mu.Unlock()

	if callback != nil && old != new {
		callback(old, new)
	}
}

// Start spawns the planner Claude Code instance in plan mode.
func (p *Planner) Start() error {
	p.mu.Lock()

	if p.state != StateStopped {
		p.mu.Unlock()
		return ErrAlreadyRunning
	}

	p.state = StateStarting
	p.startedAt = time.Now()

	// Clear history for fresh session
	p.history = agent.NewChatHistory(agent.DefaultChatHistorySize)

	// Ensure work directory exists
	if err := os.MkdirAll(p.workDir, 0755); err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		return fmt.Errorf("create work dir: %w", err)
	}

	// Get fab binary path for hooks
	fabPath, err := os.Executable()
	if err != nil {
		fabPath = "fab"
	}

	// Build settings with hooks that route to fab daemon
	hookTimeoutSec := 5 * 60 // 5 minutes in seconds
	settings := map[string]interface{}{
		"hooks": map[string]interface{}{
			"PreToolUse": []interface{}{
				map[string]interface{}{
					"matcher": "*",
					"hooks": []interface{}{
						map[string]interface{}{
							"type":    "command",
							"command": fabPath + " hook PreToolUse",
							"timeout": hookTimeoutSec,
						},
					},
				},
			},
		},
	}

	settingsJSON, err := json.Marshal(settings)
	if err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		return fmt.Errorf("marshal settings: %w", err)
	}

	// Build claude command with plan mode
	// Use -p to pass the initial prompt with plan mode instruction
	planPrompt := buildPlanModePrompt(p.prompt, p.id)

	cmd := exec.Command("claude",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "plan",
		"--settings", string(settingsJSON),
		"-p", planPrompt)
	cmd.Dir = p.workDir

	// Set environment
	cmd.Env = append(os.Environ(),
		"FAB_PLANNER_ID="+p.id,
		"FAB_PLANNER_PROJECT="+p.project,
	)

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

	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		p.state = StateStopped
		p.mu.Unlock()
		return fmt.Errorf("start process: %w", err)
	}

	p.cmd = cmd
	p.stdin = stdin
	p.stdout = stdout

	// Create read loop channels
	p.readLoopStop = make(chan struct{})
	p.readLoopDone = make(chan struct{})

	p.mu.Unlock()

	// Start read loop
	go p.runReadLoop()

	p.setState(StateRunning)
	return nil
}

// Stop gracefully stops the planner.
func (p *Planner) Stop() error {
	return p.StopWithTimeout(StopTimeout)
}

// StopWithTimeout stops the planner with a custom timeout.
func (p *Planner) StopWithTimeout(timeout time.Duration) error {
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
		slog.Debug("planner did not exit gracefully, sending SIGKILL", "timeout", timeout)
		_ = cmd.Process.Kill()
		<-done
	}

	p.setState(StateStopped)
	return nil
}

// SendMessage sends a user message to the planner.
func (p *Planner) SendMessage(content string) error {
	p.mu.RLock()
	stdin := p.stdin
	state := p.state
	p.mu.RUnlock()

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
func (p *Planner) runReadLoop() {
	defer logging.LogPanic("planner-read-loop", nil)
	defer close(p.readLoopDone)

	p.mu.RLock()
	stdout := p.stdout
	p.mu.RUnlock()

	if stdout == nil {
		return
	}

	scanner := bufio.NewScanner(stdout)

	for scanner.Scan() {
		select {
		case <-p.readLoopStop:
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
			slog.Warn("planner readloop: parse error", "error", err)
			continue
		}

		if msg == nil {
			continue
		}

		// Check for ExitPlanMode tool use
		if msg.Message != nil {
			for _, block := range msg.Message.Content {
				if block.Type == "tool_use" && block.Name == "ExitPlanMode" {
					slog.Info("planner detected ExitPlanMode", "id", p.id)
					p.handleExitPlanMode()
				}
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
	p.mu.Lock()
	if p.state == StateRunning {
		p.state = StateStopped
	}
	p.mu.Unlock()
}

// handleExitPlanMode is called when ExitPlanMode tool use is detected.
// It writes the plan to a file and notifies the callback.
func (p *Planner) handleExitPlanMode() {
	// Get the plan content from the system prompt file that Claude Code wrote
	// The plan is expected to be in .claude/plan.md in the working directory
	planSourcePath := filepath.Join(p.workDir, ".claude", "plan.md")

	// Read the plan content
	content, err := os.ReadFile(planSourcePath)
	if err != nil {
		slog.Warn("planner: could not read plan file", "path", planSourcePath, "error", err)
		// Try to extract from chat history as fallback
		content = p.extractPlanFromHistory()
	}

	// Write to .fab/plans/<agentId>.md
	plansDir := filepath.Join(os.Getenv("HOME"), ".fab", "plans")
	if err := os.MkdirAll(plansDir, 0755); err != nil {
		slog.Error("planner: could not create plans directory", "error", err)
		return
	}

	planPath := filepath.Join(plansDir, p.id+".md")
	if err := os.WriteFile(planPath, content, 0644); err != nil {
		slog.Error("planner: could not write plan file", "path", planPath, "error", err)
		return
	}

	slog.Info("planner: plan written", "path", planPath)

	// Update plan file path
	p.mu.Lock()
	p.planFile = planPath
	callback := p.onPlanComplete
	p.mu.Unlock()

	// Call completion callback
	if callback != nil {
		callback(planPath)
	}
}

// extractPlanFromHistory extracts plan content from chat history as a fallback.
func (p *Planner) extractPlanFromHistory() []byte {
	entries := p.history.Entries(0)
	var content string
	for _, entry := range entries {
		if entry.Role == "assistant" && entry.Content != "" {
			content += entry.Content + "\n\n"
		}
	}
	return []byte(content)
}

// Info returns a snapshot of planner info for status reporting.
func (p *Planner) Info() PlannerInfo {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return PlannerInfo{
		ID:        p.id,
		Project:   p.project,
		State:     p.state,
		WorkDir:   p.workDir,
		StartedAt: p.startedAt,
		PlanFile:  p.planFile,
	}
}

// PlannerInfo is a read-only snapshot of planner state.
type PlannerInfo struct {
	ID        string
	Project   string
	State     State
	WorkDir   string
	StartedAt time.Time
	PlanFile  string
}

// buildPlanModePrompt creates the prompt for plan mode with instructions.
func buildPlanModePrompt(userPrompt, plannerID string) string {
	return fmt.Sprintf(`You are in plan mode. Your task is to create an implementation plan for the following request:

%s

## Instructions

1. Explore the codebase to understand the existing architecture
2. Design an implementation approach
3. Write your plan to a file (the system will track it)
4. When your plan is complete, use the ExitPlanMode tool to finalize it

Your plan should include:
- Summary of the task
- Key files that will be modified
- Step-by-step implementation approach
- Potential risks or considerations

Focus on creating a clear, actionable plan that can be executed by another agent.

Planner ID: %s
`, userPrompt, plannerID)
}
