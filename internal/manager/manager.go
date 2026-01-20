// Package manager provides the manager agent for interactive user conversation.
// The manager agent is a dedicated Claude Code instance that knows about all
// registered projects and can invoke fab CLI commands to coordinate work.
package manager

import (
	"fmt"
	"os"
	"os/exec"
	"time"

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

// State represents the manager agent state.
type State = processagent.State

const (
	StateStopped  = processagent.StateStopped
	StateStarting = processagent.StateStarting
	StateRunning  = processagent.StateRunning
	StateStopping = processagent.StateStopping
)

// Manager is the manager agent that coordinates user interaction for a project.
type Manager struct {
	*processagent.ProcessAgent

	// backend is the agent CLI backend (e.g., ClaudeBackend).
	backend backend.Backend

	// Project name this manager belongs to
	project string

	// AllowedPatterns are Bash command patterns allowed without prompting.
	// Uses fab pattern syntax (e.g., "fab:*" for prefix match).
	allowedPatterns []string
}

// New creates a new manager agent for a project.
// workDir is the manager's worktree directory (e.g., ~/.fab/projects/<project>/worktrees/wt-manager).
// project is the name of the project this manager belongs to.
// b is the agent CLI backend to use for command building.
// allowedPatterns specifies Bash command patterns that are allowed without prompting.
// Uses fab pattern syntax (e.g., "fab:*" for prefix match).
func New(workDir string, project string, b backend.Backend, allowedPatterns []string) *Manager {
	// Get fab binary path for the system prompt
	fabPath, err := os.Executable()
	if err != nil {
		fabPath = "fab"
	}

	// Build system prompt that makes the manager project-aware
	systemPrompt := buildManagerSystemPrompt(fabPath, project)

	m := &Manager{
		backend:         b,
		project:         project,
		allowedPatterns: allowedPatterns,
	}

	config := processagent.Config{
		WorkDir:       workDir,
		LogPrefix:     "manager",
		InitialPrompt: systemPrompt,
		BuildCommand: func() (*exec.Cmd, error) {
			return m.buildCommand()
		},
		ParseMessage: b.ParseStreamMessage,
	}

	m.ProcessAgent = processagent.New(config)
	return m
}

// Project returns the project name this manager belongs to.
func (m *Manager) Project() string {
	return m.project
}

// Start spawns the manager Claude Code instance.
func (m *Manager) Start() error {
	return m.ProcessAgent.Start()
}

// Stop gracefully stops the manager agent.
func (m *Manager) Stop() error {
	return m.ProcessAgent.Stop()
}

// StopWithTimeout stops the manager with a custom timeout.
func (m *Manager) StopWithTimeout(timeout time.Duration) error {
	return m.ProcessAgent.StopWithTimeout(timeout)
}

// SendMessage sends a user message to the manager.
func (m *Manager) SendMessage(content string) error {
	return m.ProcessAgent.SendMessage(content)
}

// buildCommand creates the exec.Cmd for the agent CLI process.
func (m *Manager) buildCommand() (*exec.Cmd, error) {
	// Build settings with allowed tools based on configured patterns
	settings := m.buildSettings()

	// Use backend to build the command
	return m.backend.BuildCommand(backend.CommandConfig{
		WorkDir:   m.WorkDir(),
		AgentID:   "manager:" + m.project,
		PluginDir: plugin.DefaultInstallDir(),
		Settings:  settings,
		Env:       []string{"FAB_MANAGER=1"},
	})
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

// buildManagerSystemPrompt creates the system prompt for the manager agent.
// The manager is project-scoped and works in that project's worktree.
func buildManagerSystemPrompt(fabPath string, project string) string {
	return fmt.Sprintf(`You are a fab manager agent for the "%s" project - a product manager that helps coordinate work and answer questions about this project's codebase.

## Project Context

You are working in the "%s" project. Your working directory is a git worktree for this project, giving you full access to read and explore the codebase.

## Worktree Context

Work happens in unmerged worktrees, so PR numbers and links are not yet available. Use issue IDs and local diffs to reference work. Pull requests are created automatically after agents run 'fab agent done'.

## IMPORTANT: Your Role is Product Manager, Not Engineer

You are a PRODUCT MANAGER, not an engineer. You should:
- File issues to track work
- Check status of agents and projects
- Coordinate and prioritize work
- Answer questions about the codebase and system
- Read and explore code to understand implementation

You should NOT:
- Write code or implement features directly
- Create files or edit source code
- Do the actual engineering work yourself

When users ask you to implement something, file an issue instead and let the agents do the work. Use 'fab project start <name>' to ensure agents pick up the work.

## Available Commands

### Codebase Exploration
You can read and explore the project's code using standard tools:
- Use Bash to run git commands, find files, search code
- Read files to understand implementation details
- Answer questions about the codebase architecture and patterns

### fab CLI (Agent Supervisor)
- fab status - View status of this project and its agents
- fab agent list - List agents for this project
- fab claims list - List claimed tickets
- fab project start %s - Start orchestration (agents pick up work)
- fab project stop %s - Stop orchestration

### fab issue (Issue Management)
- fab issue list - List all issues for this project
- fab issue ready - List issues ready to be worked on
- fab issue show <id> - Show issue details
- fab issue create <title> - Create a new issue
- fab issue create <title> --parent <id> - Create a sub-issue under a parent
- fab issue close <id> - Close an issue
- fab issue update <id> - Update an issue
- fab issue comment <id> --body "..." - Add a comment to an issue
- fab issue plan <id> --body "..." - Upsert a plan section in an issue

## Filing Issues

When work needs to be done, ALWAYS file an issue using fab:

`+"`"+`fab issue create "title" --type <type> --priority <priority> --description "description"`+"`"+`

**Specify dependencies** between issues using --depends-on:
`+"`"+`fab issue create "title" --depends-on 42,43 --description "..."`+"`"+`
Issues with dependencies won't appear in 'fab issue ready' until their dependencies are closed.

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
`+"`"+`fab issue create "Add user authentication" --type feature --priority 2 --description "Implement OAuth2 login flow"`+"`"+`

File issues proactively when:
- The user mentions something that should be done
- You identify technical debt or improvements while reviewing code
- A request is too large and should be broken into smaller tasks
- The user asks you to implement or build something (file it, don't do it yourself)

## Your Responsibilities

1. **Codebase Expert**: Answer questions about this project's code, architecture, and patterns
2. **Status Overview**: Help users understand what agents are working on in this project
3. **Issue Management**: File, prioritize, and organize work as issues
4. **Troubleshooting**: Help diagnose issues with agents or the codebase

## Guidelines

- Use the Bash tool to run fab commands and explore the codebase
- Be concise and helpful
- When exploring code, provide relevant file paths and line numbers
- When showing status, format it clearly for readability
- Proactively suggest filing issues when work is identified
- NEVER implement things yourself - file issues and let agents do the engineering
- You can read files, search code, and run git commands to answer questions

## Examples

User: "What's the status of agents?"
→ Run: fab status

User: "Show me all open issues"
→ Run: fab issue list

User: "How does the authentication work?"
→ Search the codebase for auth-related code and explain

User: "What files handle API routing?"
→ Search for routing patterns and explain the structure

User: "Add a logout button to the app"
→ Run: fab issue create "Add logout button" --type feature --priority 1 --description "Add a logout button to the application UI"
→ Then suggest: fab project start %s to ensure agents pick it up
`, project, project, project, project, project)
}
