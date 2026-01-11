// Package planner provides planning agents for creating implementation plans.
// Planning agents are specialized Claude Code instances that run in plan mode
// and write their plans to .fab/plans/ when they exit via ExitPlanMode.
package planner

import (
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/tessro/fab/internal/agent"
	"github.com/tessro/fab/internal/backend"
	"github.com/tessro/fab/internal/plugin"
	"github.com/tessro/fab/internal/processagent"
)

// Re-export errors from processagent for backward compatibility.
var (
	ErrAlreadyRunning  = processagent.ErrAlreadyRunning
	ErrNotRunning      = processagent.ErrNotRunning
	ErrProcessNotFound = processagent.ErrProcessNotFound
	ErrShuttingDown    = processagent.ErrShuttingDown
)

// StopTimeout is the duration to wait for graceful shutdown.
const StopTimeout = processagent.StopTimeout

// State represents the planner agent state.
type State = processagent.State

const (
	StateStopped  = processagent.StateStopped
	StateStarting = processagent.StateStarting
	StateRunning  = processagent.StateRunning
	StateStopping = processagent.StateStopping
)

// Planner is a planning agent that creates implementation plans.
type Planner struct {
	*processagent.ProcessAgent

	mu sync.RWMutex

	// Identification
	id      string // Unique identifier
	project string // Project name (optional)

	// Initial prompt for the planning task
	prompt string

	// Compiled plan prompt (includes instructions + user prompt)
	planPrompt string

	// Backend for CLI command building
	backend backend.Backend

	// Plan file path (set when ExitPlanMode is detected)
	// +checklocks:mu
	planFile string

	// User-set description for the planner
	// +checklocks:mu
	description string

	// Callback for plan completion
	// +checklocks:mu
	onPlanComplete func(planFile string)
	// +checklocks:mu
	onInfoChange func()
}

// New creates a new planner.
func New(id, project, workDir, prompt string, b backend.Backend) *Planner {
	// Build the plan prompt
	planPrompt := buildPlanModePrompt(prompt, id)

	p := &Planner{
		id:         id,
		project:    project,
		prompt:     prompt,
		planPrompt: planPrompt,
		backend:    b,
	}

	config := processagent.Config{
		WorkDir:       workDir,
		LogPrefix:     "planner",
		InitialPrompt: planPrompt,
		LargeBuffer:   true,
		LogStderr:     true,
		BuildCommand: func() (*exec.Cmd, error) {
			return p.buildCommand()
		},
		ProcessMessage: func(msg *agent.StreamMessage) bool {
			return p.processMessage(msg)
		},
	}

	p.ProcessAgent = processagent.New(config)
	return p
}

// ID returns the planner's unique identifier.
func (p *Planner) ID() string {
	return p.id
}

// Project returns the planner's project name.
func (p *Planner) Project() string {
	return p.project
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
	p.description = desc
	callback := p.onInfoChange
	p.mu.Unlock()

	// Call callback OUTSIDE the lock to prevent deadlock
	if callback != nil {
		callback()
	}
}

// OnPlanComplete sets a callback for when the plan is complete.
func (p *Planner) OnPlanComplete(fn func(planFile string)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onPlanComplete = fn
}

// OnInfoChange sets a callback for when the description changes.
func (p *Planner) OnInfoChange(fn func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onInfoChange = fn
}

// Start spawns the planner Claude Code instance in plan mode.
func (p *Planner) Start() error {
	return p.ProcessAgent.Start()
}

// Stop gracefully stops the planner.
func (p *Planner) Stop() error {
	return p.ProcessAgent.Stop()
}

// StopWithTimeout stops the planner with a custom timeout.
func (p *Planner) StopWithTimeout(timeout time.Duration) error {
	return p.ProcessAgent.StopWithTimeout(timeout)
}

// SendMessage sends a user message to the planner.
func (p *Planner) SendMessage(content string) error {
	return p.ProcessAgent.SendMessage(content)
}

// buildCommand creates the exec.Cmd for the CLI process using the backend.
func (p *Planner) buildCommand() (*exec.Cmd, error) {
	// Build command using backend
	// FAB_AGENT_ID uses "plan:" prefix to match TUI agent ID format and enable
	// permission handling (including LLM auth) via the standard agent flow.
	// InitialPrompt is passed to the backend so it can be included as a command-line
	// argument for backends that require it (e.g., Codex).
	return p.backend.BuildCommand(backend.CommandConfig{
		WorkDir:       p.WorkDir(),
		AgentID:       "plan:" + p.id,
		InitialPrompt: p.planPrompt,
		PluginDir:     plugin.DefaultInstallDir(),
	})
}

// processMessage handles stream messages for planner-specific logic.
// Returns true if the read loop should stop.
func (p *Planner) processMessage(msg *agent.StreamMessage) bool {
	// Check for ExitPlanMode tool use
	if msg.Message != nil {
		for _, block := range msg.Message.Content {
			if block.Type == "tool_use" && block.Name == "ExitPlanMode" {
				slog.Info("planner detected ExitPlanMode", "planner", p.id)
				p.handleExitPlanMode()
			}
		}
	}
	return false // Continue reading
}

// handleExitPlanMode is called when ExitPlanMode tool use is detected.
// It writes the plan to a file and notifies the callback.
func (p *Planner) handleExitPlanMode() {
	// Get the plan content from the system prompt file that Claude Code wrote
	// The plan is expected to be in .claude/plan.md in the working directory
	planSourcePath := filepath.Join(p.WorkDir(), ".claude", "plan.md")

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
	entries := p.History().Entries(0)
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
		State:       p.State(),
		WorkDir:     p.WorkDir(),
		StartedAt:   p.StartedAt(),
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

## FIRST: Set Your Status

Before doing anything else, set your description so users can see what you're planning:

Run: fab agent describe "<brief 2-5 word summary>"

Example: fab agent describe "planning auth system"

This appears in the TUI and helps users track your progress.

## Your Task

%s

## Instructions

1. **Explore the codebase** thoroughly to understand:
   - Project structure and architecture
   - Existing patterns and conventions
   - Related code that will be affected

2. **Design the implementation** by identifying:
   - What needs to change and where
   - Dependencies between changes
   - Any technical risks or considerations

3. **Create detailed GitHub issues** for each discrete piece of work:
   Use: fab issue create "Title" --description "Detailed description" --type feature/task/bug

   **Specify dependencies** between issues using --depends-on:
   fab issue create "Title" --depends-on 42,43 --description "..."

   This ensures issues are worked on in the correct order. Issues with dependencies
   won't appear in 'fab issue ready' until their dependencies are closed.

   Each issue should:
   - Be independently implementable by an agent
   - Have clear acceptance criteria
   - Include context about why this change is needed
   - Reference related files or code
   - Be small enough to complete in one session (ideally <100 lines changed)

4. **Write a plan summary** to .fab/plans/%s.md containing:
   - High-level overview of the approach
   - List of issues created with their relationships
   - Implementation order and dependencies
   - Any architectural decisions made

5. **Complete your session**:
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
