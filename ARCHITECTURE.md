# 🚌 fab - Coding Agent Supervisor

A Go 1.25 CLI tool that supervises multiple Claude Code agents across multiple projects, with automatic task orchestration via pluggable issue backends.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                fab daemon                                    │
│  ┌─────────────┐  ┌─────────────────────────────────────────────────────┐   │
│  │   IPC       │  │  Supervisor                                         │   │
│  │  (Unix      │◄─┤  - Project registry                                 │   │
│  │   socket)   │  │  - Orchestrators (per-project)                      │   │
│  └──────┬──────┘  │  - Planner agents                                   │   │
│         │         │  - Manager agents                                   │   │
│         │         │  - Permission handling                              │   │
│         │         └─────────────────────────────────────────────────────┘   │
│         │                           │                                        │
│         ▼                           ▼                                        │
│  ┌─────────────┐    ┌──────────────────────────────────────────────────┐   │
│  │ CLI / TUI   │    │  Agents (stream-json)                             │   │
│  │ commands    │    │  ┌─────────┐ ┌─────────┐ ┌─────────┐              │   │
│  └─────────────┘    │  │ Claude  │ │ Claude  │ │ Claude  │ ...         │   │
│                     │  │ Code    │ │ Code    │ │ Code    │              │   │
│                     │  └─────────┘ └─────────┘ └─────────┘              │   │
│                     └──────────────────────────────────────────────────┘   │
└─────────────────────────────────────────────────────────────────────────────┘
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `fab server start` | Start the daemon process |
| `fab server stop` | Stop the daemon |
| `fab server restart` | Restart the daemon |
| `fab status` | Show daemon, supervisor, and agent status |
| `fab tui` / `fab attach` | Launch interactive TUI |
| **Project Management** | |
| `fab project add <remote-url>` | Register a project by git remote URL |
| `fab project remove <name>` | Unregister a project |
| `fab project list` | List registered projects |
| `fab project start <name>` | Start orchestration for a project |
| `fab project stop <name>` | Stop orchestration for a project |
| `fab project config show <name>` | Show project configuration |
| `fab project config get <name> <key>` | Get a config value |
| `fab project config set <name> <key> <value>` | Set a config value |
| **Agent Management** | |
| `fab agent list` | List all agents |
| `fab agent abort <id>` | Abort/kill an agent |
| `fab agent claim <ticket-id>` | Claim a ticket (called by agents) |
| `fab agent done` | Signal task completion (called by agents) |
| `fab agent describe "<text>"` | Set agent description (called by agents) |
| `fab agent plan [prompt]` | Start a planning agent |
| `fab agent plan list` | List planning agents |
| `fab agent plan stop <id>` | Stop a planning agent |
| **Issue/Task Management** | |
| `fab issue list` | List all issues |
| `fab issue show <id>` | Show issue details |
| `fab issue ready` | List unblocked issues ready to work |
| `fab issue create` | Create a new issue |
| `fab issue update <id>` | Update an issue |
| `fab issue close <id>` | Close an issue |
| `fab issue commit <id> <sha>` | Record a commit for an issue |
| **Plan Storage** | |
| `fab plan write` | Write plan from stdin (uses FAB_AGENT_ID) |
| `fab plan read <id>` | Read a stored plan |
| `fab plan list` | List stored plans |
| **Other** | |
| `fab claims` | List active ticket claims |
| `fab branch cleanup` | Clean up merged branches |
| `fab version` | Show version information |

## Agent Types

### Task Agents
Standard agents that work on issues. Each runs in an isolated worktree and:
- Claims issues via `fab agent claim <id>`
- Signals completion via `fab agent done`
- Counts against `max-agents` limit per project

### Planner Agents
Specialized agents for design and exploration work:
- Run in plan mode with codebase exploration tools
- Write plans explicitly via `fab plan write` (reads from stdin)
- Plans stored in `~/.fab/plans/<id>.md` (or `$FAB_DIR/plans/`)
- Do NOT count against `max-agents` limit
- Identified by `plan:` prefix in TUI
- Managed via `fab agent plan start/list/stop`

### Manager Agents
Interactive agents for user coordination:
- One per project, runs in dedicated `wt-manager` worktree
- For direct user conversation and task delegation
- Persists across sessions

## TUI Layout

```
┌─ 🚌 fab ──────────────────────────────────────────────────────────────────┐
│ 6 agents (5 run, 1 idle)  │  12 closed  8 commits  │  Usage: ████░░ 67%  │
├───────────────────┬───────────────────────────────────────────────────────┤
│ myapp             │                                                       │
│ ────────────────  │  $ claude                                             │
│ > agent-a1b [R]   │  I'll help you implement that feature.               │
│   agent-c2d [R]   │                                                       │
│   agent-e3f [I]   │  Let me start by reading the code...                 │
│                   │                                                       │
│ api-svc           │  Reading src/handlers/auth.go...                     │
│ ────────────────  │                                                       │
│   agent-g4h [R]   │  I see the authentication handler. Now let me...     │
│   agent-i5j [D]   │                                                       │
│                   │                                                       │
│ plan:abc123 [R]   │                                                       │
│ ────────────────  │                                                       │
├───────────────────┴───────────────────────────────────────────────────────┤
│ j/k:nav  Enter:chat  a:approve  r:reject  d:delete  q:quit               │
└───────────────────────────────────────────────────────────────────────────┘
```

**Key features:**
- Top bar: status summary (agent counts), session stats (tickets closed, commits made), usage meter
- Left rail: all agents grouped by project, state indicators [S]tarting/[R]unning/[I]dle/[D]one
- Main pane: selected agent's chat view (scrollable, interactive)
- Permission requests and user questions displayed inline

**Key bindings:**
- `j/k` or arrows: navigate agents
- `Enter`: open input line for chat
- `y`: approve pending permission
- `n`: deny pending permission
- `d`: delete selected agent
- `Esc`: cancel input / return to navigation
- `q`: quit TUI (detach, agents keep running)

## Project & Worktree Model

**Registry** (`~/.config/fab/config.toml`):
```toml
[projects.myapp]
remote-url = "git@github.com:user/myapp.git"
max-agents = 3
issue-backend = "tk"
autostart = true
permissions-checker = "manual"
allowed-authors = ["user@example.com"]
```

**Project directory structure** (`~/.fab/projects/<name>/`):
```
myapp/
├── repo/                    # Cloned git repository
│   └── .tickets/            # Issue files (tk backend)
├── worktrees/               # Agent worktrees
│   ├── wt-abc123/           # Agent worktree
│   └── wt-def456/           # Another agent worktree
└── manager/                 # Manager agent worktree
    └── wt-manager/
```

**Worktree pool behavior:**
- Pool created when project is added (size = `max-agents`)
- Each agent gets exclusive worktree from pool
- Worktree returned to pool when agent signals `fab agent done`
- Orchestrator handles merge to main and worktree reset

## Issue Backend System

The issue backend abstraction (`internal/issue/`) supports pluggable task tracking:

```go
type Backend interface {
    ReadBackend
    WriteBackend
}

type ReadBackend interface {
    Get(ctx context.Context, id string) (*Issue, error)
    List(ctx context.Context, opts ListOptions) ([]*Issue, error)
    Ready(ctx context.Context) ([]*Issue, error)  // Unblocked issues
}

type WriteBackend interface {
    Create(ctx context.Context, i *Issue) error
    Update(ctx context.Context, i *Issue) error
    Close(ctx context.Context, id string) error
    Commit(ctx context.Context, id, sha string) error
}
```

**Implementations:**
- **tk** (default): Plain text TOML files in `.tickets/` directory
- **gh**: GitHub Issues API integration

**Issue type:**
```go
type Issue struct {
    ID           string
    Title        string
    Description  string
    Status       Status  // open, closed, blocked
    Priority     int
    Type         string
    Dependencies []string
    Labels       []string
    Links        []Link
    Created      time.Time
    Updated      time.Time
}
```

## Orchestrator Logic

Each project gets an `Orchestrator` that manages the agent lifecycle:

1. **Task polling**: Periodically checks `backend.Ready()` for unblocked issues
2. **Agent spawning**: Creates agents up to `max-agents` with kickstart prompt
3. **Claim tracking**: Prevents multiple agents claiming the same issue
4. **Done handling**: On `fab agent done`:
   - Merges agent's branch to main
   - Records commit via `backend.Commit()`
   - Returns worktree to pool
   - Spawns replacement agent if tasks remain

## Permission System

Claude Code tool permissions can be handled via:

1. **Manual**: TUI prompts user to approve/deny each permission request
2. **LLM**: LLM evaluates requests for safety and task consistency
3. **Rules**: Pattern-based rules in `permissions.toml`

Permission requests flow through the `fab hook` command, which the Claude Code plugin calls before tool execution.

## IPC Protocol

Unix socket server at `~/.fab/fab.sock` with JSON request/response messaging.

**Message categories:**
- Server management: `ping`, `shutdown`
- Supervisor control: `start`, `stop`, `status`
- Project management: `project.add`, `project.remove`, `project.list`, `project.config.*`
- Agent management: `agent.list`, `agent.create`, `agent.delete`, `agent.abort`, `agent.done`, `agent.claim`, `agent.describe`
- TUI streaming: `attach`, `detach`, `agent.chat_history`, `agent.send_message`
- Orchestrator: `orchestrator.actions`, `orchestrator.approve`, `orchestrator.reject`
- Permissions: `permission.request`, `permission.respond`, `permission.list`
- Questions: `question.request`, `question.respond`
- Planning: `plan.start`, `plan.stop`, `plan.list`, `plan.send_message`, `plan.chat_history`
- Manager: `manager.start`, `manager.stop`, `manager.status`, `manager.send_message`, `manager.chat_history`
- Stats: `stats`, `claim.list`, `commit.list`

## Directory Structure

```
fab/
├── cmd/
│   └── fab/
│       └── main.go              # Entry point
├── internal/
│   ├── cli/                     # CLI commands (Cobra)
│   │   ├── root.go              # Root command
│   │   ├── server.go            # server start/stop/restart
│   │   ├── project.go           # project add/remove/list/start/stop/config
│   │   ├── agent.go             # agent list/abort/claim/done/describe
│   │   ├── issue.go             # issue list/show/ready/create/update/close/commit
│   │   ├── plan.go              # plan start/list/stop/chat
│   │   ├── manager.go           # manager commands
│   │   ├── attach.go            # tui/attach command
│   │   ├── status.go            # status command
│   │   ├── claims.go            # claims list
│   │   ├── branch.go            # branch cleanup
│   │   ├── hook.go              # Permission hook callbacks
│   │   └── version.go           # version command
│   ├── daemon/                  # IPC server
│   │   ├── server.go            # Unix socket RPC server
│   │   ├── client.go            # Client for CLI/TUI
│   │   ├── protocol.go          # IPC message types
│   │   ├── permissions.go       # Permission request handling
│   │   ├── questions.go         # User question handling
│   │   └── errors.go            # Error types
│   ├── supervisor/              # Request handler
│   │   ├── supervisor.go        # Main handler implementation
│   │   ├── handle_*.go          # Per-category handlers
│   │   └── helpers.go           # Shared utilities
│   ├── orchestrator/            # Per-project orchestration
│   │   ├── orchestrator.go      # Orchestration loop
│   │   ├── claims.go            # Ticket claim tracking
│   │   └── commits.go           # Commit tracking
│   ├── agent/                   # Agent management
│   │   ├── agent.go             # Agent type + lifecycle
│   │   ├── manager.go           # Agent registry
│   │   ├── chathistory.go       # Chat history buffer
│   │   └── streamjson.go        # Stream-JSON protocol parsing
│   ├── project/                 # Project management
│   │   ├── project.go           # Project type
│   │   └── worktree.go          # Worktree pool management
│   ├── registry/                # Project persistence
│   │   └── registry.go          # TOML config load/save
│   ├── issue/                   # Issue backend abstraction
│   │   ├── backend.go           # Backend interface
│   │   ├── issue.go             # Issue type
│   │   ├── resolver.go          # Backend resolution
│   │   ├── tk/                  # tk backend (TOML files)
│   │   └── gh/                  # GitHub backend
│   ├── planner/                 # Planning agents
│   │   ├── planner.go           # Planner type
│   │   └── manager.go           # Planner registry
│   ├── manager/                 # Manager agents
│   │   └── manager.go           # Manager type + lifecycle
│   ├── config/                  # Configuration
│   │   ├── global.go            # Global config loading
│   │   └── validate.go          # Config validation
│   ├── rules/                   # Permission rules
│   │   ├── rules.go             # Rule types
│   │   ├── matcher.go           # Pattern matching
│   │   └── evaluator.go         # Rule evaluation
│   ├── llmauth/                 # LLM-based permissions
│   │   └── llmauth.go           # LLM permission checker
│   ├── tui/                     # Terminal UI (Bubbletea)
│   │   ├── tui.go               # Main model
│   │   ├── update.go            # Update logic
│   │   ├── header.go            # Status bar
│   │   ├── agentlist.go         # Agent list component
│   │   ├── chatview.go          # Chat view component
│   │   ├── inputline.go         # Input line component
│   │   ├── helpbar.go           # Help bar component
│   │   ├── planner.go           # Planner view
│   │   ├── manager.go           # Manager view
│   │   ├── mode.go              # View modes
│   │   ├── keybindings.go       # Key bindings
│   │   ├── styles.go            # Lipgloss styles
│   │   └── commands.go          # Bubbletea commands
│   ├── usage/                   # Usage tracking
│   │   └── usage.go             # JSONL parsing for usage stats
│   ├── event/                   # Event system
│   │   └── emitter.go           # Generic event emitter
│   ├── plugin/                  # Claude Code plugin
│   │   └── plugin.go            # Plugin installation
│   ├── logging/                 # Logging
│   │   └── logging.go           # Structured logging setup
│   ├── id/                      # ID generation
│   │   └── id.go                # Short ID utilities
│   └── version/                 # Version info
│       └── version.go           # Build version
├── go.mod
└── go.sum
```

## Dependencies

```go
require (
    github.com/BurntSushi/toml v1.6.0
    github.com/charmbracelet/bubbles v0.21.0
    github.com/charmbracelet/bubbletea v1.3.10
    github.com/charmbracelet/lipgloss v1.1.0
    github.com/spf13/cobra v1.10.2
)
```

## Key Files

| File | Purpose |
|------|---------|
| `internal/daemon/server.go` | Unix socket RPC server |
| `internal/daemon/protocol.go` | IPC message types (40+ message types) |
| `internal/supervisor/supervisor.go` | Main request handler, implements daemon.Handler |
| `internal/orchestrator/orchestrator.go` | Per-project agent lifecycle and task orchestration |
| `internal/agent/agent.go` | Agent type, state machine, process management |
| `internal/agent/streamjson.go` | Stream-JSON protocol parsing for Claude Code I/O |
| `internal/project/worktree.go` | Worktree pool: create, assign, recycle |
| `internal/issue/backend.go` | Pluggable issue backend interface |
| `internal/registry/registry.go` | Project configuration persistence |
| `internal/tui/tui.go` | Bubbletea main model |
| `internal/tui/chatview.go` | Chat message rendering and interaction |
| `internal/planner/planner.go` | Planning agent implementation |
