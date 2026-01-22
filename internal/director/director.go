// Package director provides the director agent for cross-project coordination.
// The director agent is a dedicated Claude Code instance that has visibility
// across all projects and can coordinate work globally.
package director

import (
	"fmt"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/tessro/fab/internal/backend"
	"github.com/tessro/fab/internal/plugin"
	"github.com/tessro/fab/internal/processagent"
	"github.com/tessro/fab/internal/project"
	"github.com/tessro/fab/internal/registry"
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

// State represents the director agent state.
type State = processagent.State

const (
	StateStopped  = processagent.StateStopped
	StateStarting = processagent.StateStarting
	StateRunning  = processagent.StateRunning
	StateStopping = processagent.StateStopping
)

// Director is the director agent that coordinates work across all projects.
type Director struct {
	*processagent.ProcessAgent

	// backend is the agent CLI backend (e.g., ClaudeBackend).
	backend backend.Backend

	// AllowedPatterns are Bash command patterns allowed without prompting.
	// Uses fab pattern syntax (e.g., "fab:*" for prefix match).
	allowedPatterns []string

	// registry provides access to all registered projects.
	registry *registry.Registry
}

// New creates a new director agent.
// workDir is the director's working directory (e.g., ~/.fab/projects/).
// b is the agent CLI backend to use for command building.
// allowedPatterns specifies Bash command patterns that are allowed without prompting.
// Uses fab pattern syntax (e.g., "fab:*" for prefix match).
// reg provides access to all registered projects.
func New(workDir string, b backend.Backend, allowedPatterns []string, reg *registry.Registry) *Director {
	// Gather projects from registry for system prompt
	var projects []*project.Project
	if reg != nil {
		projects = reg.List()
	}

	// Build system prompt that makes the director project-aware
	systemPrompt := buildDirectorSystemPrompt(projects)

	d := &Director{
		backend:         b,
		allowedPatterns: allowedPatterns,
		registry:        reg,
	}

	config := processagent.Config{
		WorkDir:       workDir,
		LogPrefix:     "director",
		InitialPrompt: systemPrompt,
		BuildCommand: func() (*exec.Cmd, error) {
			return d.buildCommand()
		},
		ParseMessage: b.ParseStreamMessage,
	}

	d.ProcessAgent = processagent.New(config)
	return d
}

// Registry returns the registry for accessing projects.
func (d *Director) Registry() *registry.Registry {
	return d.registry
}

// Start spawns the director Claude Code instance.
func (d *Director) Start() error {
	return d.ProcessAgent.Start()
}

// Stop gracefully stops the director agent.
func (d *Director) Stop() error {
	return d.ProcessAgent.Stop()
}

// Resume restarts a stopped director without clearing history.
func (d *Director) Resume() error {
	return d.ProcessAgent.Resume()
}

// StopWithTimeout stops the director with a custom timeout.
func (d *Director) StopWithTimeout(timeout time.Duration) error {
	return d.ProcessAgent.StopWithTimeout(timeout)
}

// SendMessage sends a user message to the director.
func (d *Director) SendMessage(content string) error {
	return d.ProcessAgent.SendMessage(content)
}

// buildCommand creates the exec.Cmd for the agent CLI process.
func (d *Director) buildCommand() (*exec.Cmd, error) {
	// Build settings with allowed tools based on configured patterns
	settings := d.buildSettings()

	// Use backend to build the command
	return d.backend.BuildCommand(backend.CommandConfig{
		WorkDir:   d.WorkDir(),
		AgentID:   "director",
		PluginDir: plugin.DefaultInstallDir(),
		Settings:  settings,
		Env:       []string{"FAB_DIRECTOR=1"},
	})
}

// buildSettings creates the Claude Code settings with allowed tool permissions.
// It converts fab pattern syntax (e.g., "fab:*") to Claude Code format (e.g., "Bash(fab *)").
func (d *Director) buildSettings() map[string]any {
	allowedTools := d.buildAllowedTools()
	return map[string]any{
		"permissions": map[string]any{
			"allow": allowedTools,
		},
	}
}

// buildAllowedTools converts fab patterns to Claude Code allowedTools format.
// Fab patterns use the format "prefix:*" for prefix matching.
// Claude Code uses "Bash(prefix *)" for Bash command prefix matching.
func (d *Director) buildAllowedTools() []string {
	var tools []string

	for _, pattern := range d.allowedPatterns {
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

// buildDirectorSystemPrompt creates the system prompt for the director agent.
// The director has global visibility across all projects.
// projects is the list of all registered projects to include in the prompt.
func buildDirectorSystemPrompt(projects []*project.Project) string {
	var sb strings.Builder

	sb.WriteString(`You are the fab director agent - a technical director that coordinates work across all registered projects.

## Your Role

You are a CTO-level coordinator, NOT an engineer. You should:
- Understand the big picture across all projects
- Coordinate work between projects
- File issues to track cross-project work
- Help with high-level architecture decisions
- Answer questions about any project's codebase

You should NOT:
- Write code or implement features directly
- Make changes to source files
- Do the actual engineering work

When users ask you to implement something, file an issue in the appropriate project and let the agents do the work. Use 'fab project start <name>' to ensure agents pick up the work.

## Projects Under Your Direction

`)

	// Add dynamic project list
	projectSummary := buildProjectSummary(projects)
	sb.WriteString(projectSummary)

	sb.WriteString(`
## Worktree Context

Work happens in unmerged worktrees, so PR numbers and links are not yet available. Use issue IDs and local diffs to reference work. Pull requests are created automatically after agents run 'fab agent done'.

## Available Commands

### Project Management
- fab status - View all projects and their agents
- fab project list - List all registered projects
- fab project start <name> - Start orchestration for a project
- fab project stop <name> - Stop orchestration for a project

### Issue Management (Cross-Project)
- fab issue list --project <name> - List issues for a specific project
- fab issue ready --project <name> - List ready issues for a project
- fab issue show <id> --project <name> - Show issue details
- fab issue create <title> --project <name> - Create issue in a specific project
- fab issue create <title> --project <name> --parent <id> - Create a sub-issue
- fab issue close <id> --project <name> - Close an issue
- fab issue comment <id> --project <name> --body "..." - Add a comment
- fab issue plan <id> --project <name> --body "..." - Upsert a plan section

### Agent Coordination
- fab agent list - List all agents across all projects
- fab claims list - List claimed tickets across projects

### Codebase Exploration
You can read files from any project by using absolute paths:
- ~/.fab/projects/<project-name>/repo/<path>

## Filing Cross-Project Issues

When work spans multiple projects or needs to be filed in a specific project:

` + "`" + `fab issue create "title" --project <name> --type <type> --priority <priority> --description "description"` + "`" + `

**Specify dependencies** between issues using --depends-on:
` + "`" + `fab issue create "title" --project <name> --depends-on 42,43 --description "..."` + "`" + `
Issues with dependencies won't appear in 'fab issue ready' until their dependencies are closed.

Issue types:
- task: General work items
- feature: New functionality
- bug: Something that needs fixing
- chore: Maintenance work

Priority levels:
- 0: Low priority
- 1: Medium priority (default)
- 2: High priority

## Example Tasks

"What projects are currently running?"
→ Run: fab status

"Start work on the auth-service project"
→ Run: fab project start auth-service

"Create a feature request for the api project"
→ Run: fab issue create "Add rate limiting" --project api --type feature --description "..."

"How does the database connection work in the backend project?"
→ Read files from ~/.fab/projects/backend/repo/...

## Your Responsibilities

1. **Cross-Project Coordination**: Identify and manage work that spans multiple projects
2. **Architectural Oversight**: Make decisions about project structure and dependencies
3. **Global Status**: Help users understand what's happening across all projects
4. **Strategic Planning**: Break down large initiatives into project-specific tasks
5. **Resource Allocation**: Prioritize work and ensure projects have appropriate focus

## Guidelines

- Use the Bash tool to run fab commands and explore codebases
- Be concise and helpful
- When exploring code, provide relevant file paths and line numbers
- When showing status, format it clearly for readability
- Proactively suggest filing issues when cross-project work is identified
- NEVER implement things yourself - file issues and let agents do the engineering
- Think about dependencies between projects when planning work
- Consider the big picture when making recommendations
`)

	return sb.String()
}

// buildProjectSummary generates a formatted summary of all registered projects.
// Returns a string suitable for inclusion in the director's system prompt.
func buildProjectSummary(projects []*project.Project) string {
	if len(projects) == 0 {
		return "No projects registered yet. Use 'fab project add <remote-url>' to add projects.\n"
	}

	// Sort projects by name for consistent ordering
	sorted := make([]*project.Project, len(projects))
	copy(sorted, projects)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Name < sorted[j].Name
	})

	var sb strings.Builder

	for _, p := range sorted {
		// Determine status
		status := "stopped"
		if p.IsRunning() {
			status = "running"
		}

		// Count active agents
		activeAgents := p.ActiveAgentCount()

		sb.WriteString(fmt.Sprintf("### %s\n", p.Name))
		sb.WriteString(fmt.Sprintf("- Remote: %s\n", p.RemoteURL))
		sb.WriteString(fmt.Sprintf("- Status: %s\n", status))
		sb.WriteString(fmt.Sprintf("- Active agents: %d/%d\n", activeAgents, p.MaxAgents))
		sb.WriteString(fmt.Sprintf("- Path: ~/.fab/projects/%s/repo/\n", p.Name))
		sb.WriteString("\n")
	}

	return sb.String()
}
