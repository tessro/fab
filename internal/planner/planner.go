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
	"github.com/tessro/fab/internal/plugin"
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

	// User-set description for the planner
	// +checklocks:mu
	description string

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

// SetDescription sets the planner's description.
func (p *Planner) SetDescription(desc string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.description = desc
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
	// Create a scoped logger with planner ID and project for all logging in this method
	log := slog.With("planner", p.id, "project", p.project)

	log.Debug("planner.Start: beginning", "workdir", p.workDir)
	p.mu.Lock()

	if p.state != StateStopped {
		p.mu.Unlock()
		log.Debug("planner.Start: already running", "state", p.state)
		return ErrAlreadyRunning
	}

	p.state = StateStarting
	p.startedAt = time.Now()

	// Clear history for fresh session
	p.history = agent.NewChatHistory(agent.DefaultChatHistorySize)

	// Ensure work directory exists
	log.Debug("planner.Start: creating work dir", "workdir", p.workDir)
	if err := os.MkdirAll(p.workDir, 0755); err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("planner.Start: failed to create work dir", "error", err)
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

	// Build claude command
	// Use -p to pass the initial prompt with planning instructions
	// Note: We use "default" permission mode, not "plan" mode, because:
	// - Claude Code's "plan" mode blocks all non-readonly operations
	// - The planner needs to run fab commands (issue create, agent done)
	// - The fab hook system handles permission control
	planPrompt := buildPlanModePrompt(p.prompt, p.id)

	log.Debug("planner.Start: building command", "prompt_len", len(planPrompt))

	// NOTE: Do not use -p flag with --input-format stream-json.
	// The prompt must be sent via stdin as a JSON message after starting.
	cmd := exec.Command("claude",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--verbose",
		"--permission-mode", "default",
		"--plugin-dir", plugin.DefaultInstallDir(),
		"--settings", string(settingsJSON))
	cmd.Dir = p.workDir

	// Set environment
	// FAB_AGENT_ID uses "plan:" prefix to match TUI agent ID format and enable
	// permission handling (including LLM auth) via the standard agent flow.
	cmd.Env = append(os.Environ(),
		"FAB_AGENT_ID=plan:"+p.id,
	)

	log.Debug("planner.Start: setting up pipes")

	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("planner.Start: stdin pipe failed", "error", err)
		return fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("planner.Start: stdout pipe failed", "error", err)
		return fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("planner.Start: stderr pipe failed", "error", err)
		return fmt.Errorf("stderr pipe: %w", err)
	}

	// Start the process
	log.Debug("planner.Start: starting claude process", "cmd", cmd.Path, "dir", cmd.Dir)
	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		stderr.Close()
		p.state = StateStopped
		p.mu.Unlock()
		log.Error("planner.Start: process start failed", "error", err)
		return fmt.Errorf("start process: %w", err)
	}

	// Start stderr reader goroutine to log any errors from Claude
	// Capture log variable for use in goroutine
	stderrLog := log
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			line := scanner.Text()
			stderrLog.Warn("planner.stderr", "line", line)
		}
	}()
	log.Debug("planner.Start: claude process started", "pid", cmd.Process.Pid)

	p.cmd = cmd
	p.stdin = stdin
	p.stdout = stdout

	// Create read loop channels
	p.readLoopStop = make(chan struct{})
	p.readLoopDone = make(chan struct{})

	p.mu.Unlock()

	// Send initial prompt via stdin (required for --input-format stream-json)
	log.Debug("planner.Start: sending initial prompt via stdin")
	if err := p.sendInitialPrompt(planPrompt); err != nil {
		log.Error("planner.Start: failed to send initial prompt", "error", err)
		// Don't fail - process is running, just log the error
	}

	// Start read loop
	log.Debug("planner.Start: starting read loop goroutine")
	go p.runReadLoop()

	p.setState(StateRunning)
	log.Info("planner.Start: complete", "pid", cmd.Process.Pid)
	return nil
}

// sendInitialPrompt sends the initial prompt to Claude via stdin.
func (p *Planner) sendInitialPrompt(prompt string) error {
	p.mu.RLock()
	stdin := p.stdin
	p.mu.RUnlock()

	if stdin == nil {
		return fmt.Errorf("stdin not available")
	}

	msg := agent.InputMessage{
		Type: "user",
		Message: agent.MessageBody{
			Role:    "user",
			Content: prompt,
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

	// Create a scoped logger with planner ID for all logging in this loop
	log := slog.With("planner", p.id)

	log.Debug("planner.runReadLoop: starting")

	p.mu.RLock()
	stdout := p.stdout
	p.mu.RUnlock()

	if stdout == nil {
		log.Warn("planner.runReadLoop: stdout is nil, exiting")
		return
	}

	scanner := bufio.NewScanner(stdout)
	lineCount := 0
	log.Debug("planner.runReadLoop: waiting for first line from claude")

	for scanner.Scan() {
		select {
		case <-p.readLoopStop:
			log.Debug("planner.runReadLoop: stop signal received", "lines_read", lineCount)
			return
		default:
		}

		line := scanner.Bytes()
		lineCount++

		if lineCount == 1 {
			log.Debug("planner.runReadLoop: received first line", "len", len(line))
		}

		if len(line) == 0 {
			continue
		}

		// Parse stream message
		msg, err := agent.ParseStreamMessage(line)
		if err != nil {
			log.Warn("planner.runReadLoop: parse error", "error", err, "line_num", lineCount)
			continue
		}

		if msg == nil {
			continue
		}

		// Check for ExitPlanMode tool use
		if msg.Message != nil {
			for _, block := range msg.Message.Content {
				if block.Type == "tool_use" && block.Name == "ExitPlanMode" {
					log.Info("planner detected ExitPlanMode")
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
	scanErr := scanner.Err()
	log.Debug("planner.runReadLoop: scanner finished", "lines_read", lineCount, "error", scanErr)

	p.mu.Lock()
	if p.state == StateRunning {
		p.state = StateStopped
		log.Debug("planner.runReadLoop: state set to stopped")
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
		ID:          p.id,
		Project:     p.project,
		State:       p.state,
		WorkDir:     p.workDir,
		StartedAt:   p.startedAt,
		PlanFile:    p.planFile,
		Description: p.description,
	}
}

// PlannerInfo is a read-only snapshot of planner state.
type PlannerInfo struct {
	ID          string
	Project     string
	State       State
	WorkDir     string
	StartedAt   time.Time
	PlanFile    string
	Description string
}

// buildPlanModePrompt creates the prompt for the planning agent.
// The planner receives instructions to explore the codebase, create issues,
// and write a plan summary before completing via 'fab agent done'.
func buildPlanModePrompt(userPrompt, plannerID string) string {
	return fmt.Sprintf(`You are a Product Manager planning agent. Your job is to break down high-level features into detailed, actionable engineering tasks.

## Your Task

%s

## Instructions

1. **Explore the codebase** thoroughly to understand:
   - Project structure and architecture
   - Existing patterns and conventions
   - Related code that will be affected

2. **Set your status** immediately after understanding the task:
   Run: fab agent describe "<brief 2-5 word description of what you're planning>"

3. **Design the implementation** by identifying:
   - What needs to change and where
   - Dependencies between changes
   - Any technical risks or considerations

4. **Create detailed GitHub issues** for each discrete piece of work:
   Use: fab issue create "Title" --description "Detailed description" --type feature/task/bug

   Each issue should:
   - Be independently implementable by an agent
   - Have clear acceptance criteria
   - Include context about why this change is needed
   - Reference related files or code
   - Be small enough to complete in one session (ideally <100 lines changed)

5. **Write a plan summary** to .fab/plans/%s.md containing:
   - High-level overview of the approach
   - List of issues created with their relationships
   - Implementation order and dependencies
   - Any architectural decisions made

6. **Complete your session**:
   Run: fab agent done

## Best Practices for Issue Creation

- **Atomic tasks**: Each issue should do ONE thing well
- **Clear titles**: "Add user authentication middleware" not "Auth stuff"
- **Detailed descriptions**: Include WHY, WHAT, and HOW
- **Test considerations**: Mention what tests should be added
- **No implementation**: You are PLANNING only - do not write code

## Example Issue Format

Title: Add rate limiting middleware to API endpoints

Description:
## Summary
Implement rate limiting to prevent API abuse and ensure fair usage.

## Implementation Details
- Add new middleware in internal/api/middleware/ratelimit.go
- Use token bucket algorithm with Redis backend
- Default: 100 requests/minute per API key
- Return 429 status when limit exceeded

## Files to Modify
- internal/api/middleware/ratelimit.go (new)
- internal/api/router.go (add middleware)
- internal/config/config.go (add rate limit settings)

## Testing
- Unit tests for rate limiter logic
- Integration test for rate limit headers
- Load test to verify limits work under pressure

Planner ID: %s
`, userPrompt, plannerID, plannerID)
}
