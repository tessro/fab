# Architecture

## Purpose

This document provides a high-level architectural overview of fab, showing how its components fit together to supervise coding agents across multiple projects.

## System Overview

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

## Agent Types

All agent types support both Claude and Codex backends via the `agent-backend`, `planner-backend`, and `coding-backend` config keys.

### Task Agents

Standard agents that work on issues. Each runs in an isolated worktree and:

- Claims issues via `fab agent claim <id>`
- Signals completion via `fab agent done`
- Counts against `max-agents` limit per project
- Uses `coding-backend` config (falls back to `agent-backend`, then `claude`)

### Planner Agents

Specialized agents for design and exploration work:

- Run in plan mode with codebase exploration tools
- Write plans explicitly via `fab plan write` (reads from stdin)
- Plans stored in `~/.fab/plans/<id>.md` (or `$FAB_DIR/plans/`)
- Do NOT count against `max-agents` limit
- Identified by `plan:` prefix in TUI
- Managed via `fab agent plan` commands
- Uses `planner-backend` config (falls back to `agent-backend`, then `claude`)

### Manager Agents

Interactive agents for user coordination:

- One per project, runs in dedicated `wt-manager` worktree
- For direct user conversation and task delegation
- Persists across sessions
- Managed via `fab manager` commands
- Uses `agent-backend` config (defaults to `claude`)

## Agent Host Protocol

Each agent runs in a host process with its own Unix socket at `~/.fab/hosts/<agent-id>.sock`. This allows the daemon to restart and reattach to running agents.

```
┌───────────────────────────────────────────────────────────────────────┐
│                           fab daemon                                   │
│                                                                        │
│  fab.sock ◄──────────────────────────────────────────────────────┐    │
│      │                                                            │    │
│      ▼                                                            │    │
│  Supervisor ──────► Orchestrator                                  │    │
│                          │                                        │    │
└──────────────────────────┼────────────────────────────────────────┘    │
                           │                                             │
                           ▼                                             │
     ┌─────────────────────────────────────────────────────┐            │
     │              Agent Host Process                      │            │
     │                                                      │            │
     │  hosts/<id>.sock ◄───────────────────────────────────┼────────────┘
     │       │                                              │
     │       ▼                                              │
     │  ┌──────────────────────────────────────┐           │
     │  │         Claude Code                  │           │
     │  │         (subprocess)                 │           │
     │  └──────────────────────────────────────┘           │
     └─────────────────────────────────────────────────────┘
```

**Protocol version:** 1.0 (defined in `internal/agenthost/protocol.go`)

**Message types:**

- Host management: `host.ping`, `host.status`
- Agent listing: `host.list`
- Stream management: `host.attach`, `host.detach`
- Agent communication: `host.send`
- Lifecycle control: `host.stop`

**Socket path resolution:**

1. `FAB_AGENT_HOST_SOCKET_PATH` environment variable (exact path)
2. `FAB_DIR/hosts/<agent-id>.sock` (base dir override)
3. `~/.fab/hosts/<agent-id>.sock` (default)

## Project & Worktree Model

**Registry** (`~/.config/fab/config.toml`):

```toml
[[projects]]
name = "myapp"
remote-url = "git@github.com:user/myapp.git"
max-agents = 3
issue-backend = "tk"  # or "github", "gh", "linear"
autostart = true
permissions-checker = "manual"  # or "llm"
allowed-authors = ["user@example.com"]
agent-backend = "claude"  # or "codex" (fallback for all agent types)
planner-backend = "claude"  # or "codex" (for planning agents)
coding-backend = "claude"  # or "codex" (for coding/task agents)
merge-strategy = "direct"  # or "pull-request"
linear-team = ""  # Linear team ID (required for "linear" backend)
linear-project = ""  # Linear project ID (optional)
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

## IPC Protocol

### Daemon Protocol

Unix socket server at `~/.fab/fab.sock` with JSON request/response messaging.

**Message categories:**

- Server management: `ping`, `shutdown`
- Supervisor control: `start`, `stop`, `status`
- Project management: `project.add`, `project.remove`, `project.list`, `project.config.show`, `project.config.get`, `project.config.set`
- Agent management: `agent.list`, `agent.create`, `agent.delete`, `agent.abort`, `agent.done`, `agent.claim`, `agent.describe`, `agent.idle`, `agent.input`, `agent.output`
- TUI streaming: `attach`, `detach`, `agent.chat_history`, `agent.send_message`
- Permissions: `permission.request`, `permission.respond`, `permission.list`
- Questions: `question.request`, `question.respond`
- Planning: `plan.start`, `plan.stop`, `plan.list`, `plan.send_message`, `plan.chat_history`
- Manager: `manager.start`, `manager.stop`, `manager.status`, `manager.send_message`, `manager.chat_history`, `manager.clear_history`
- Stats: `stats`, `claim.list`, `commit.list`

### Request/Response Envelope

```json
// Request
{
  "type": "host.ping",
  "id": "req-123",
  "payload": { ... }
}

// Response
{
  "type": "host.ping",
  "id": "req-123",
  "success": true,
  "payload": { ... }
}
```

**Stream events** (sent to attached clients):

```json
{
  "type": "output",
  "agent_id": "abc123",
  "offset": 1024,
  "timestamp": "2024-01-15T10:30:00Z",
  "data": "..."
}
```

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
│   │   ├── issue.go             # issue list/show/ready/create/update/close/commit/comment/plan
│   │   ├── plan.go              # plan write/read/list (storage)
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
│   ├── agenthost/               # Agent host process protocol
│   │   └── protocol.go          # Agent host IPC message types
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
│   │   ├── gh/                  # GitHub backend
│   │   └── linear/              # Linear backend
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
| `fab agent plan <prompt>` | Start a planning agent |
| `fab agent plan list` | List planning agents |
| `fab agent plan stop <id>` | Stop a planning agent |
| **Manager Agent** | |
| `fab manager start <project>` | Start the manager agent for a project |
| `fab manager stop <project>` | Stop the manager agent |
| `fab manager status <project>` | Show manager agent status |
| `fab manager clear <project>` | Clear manager agent's context window |
| **Issue/Task Management** | |
| `fab issue list` | List all issues |
| `fab issue show <id>` | Show issue details |
| `fab issue ready` | List unblocked issues ready to work |
| `fab issue create <title>` | Create a new issue |
| `fab issue update <id>` | Update an issue |
| `fab issue close <id>` | Close an issue |
| `fab issue commit` | Commit and push pending issue changes |
| `fab issue comment <id>` | Add a comment to an issue |
| `fab issue plan <id>` | Upsert a plan section in an issue |
| **Plan Storage** | |
| `fab plan write` | Write plan from stdin (uses FAB_AGENT_ID) |
| `fab plan read <id>` | Read a stored plan |
| `fab plan list` | List stored plans |
| **Hooks** | |
| `fab hook <hook-name>` | Handle Claude Code hook callbacks (PreToolUse, Stop) |
| **Other** | |
| `fab claims` | List active ticket claims |
| `fab branch cleanup` | Clean up merged branches |
| `fab version` | Show version information |

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

## See Also

- [Supervisor](./supervisor.md) - Central daemon request handler
- [Orchestrator](./orchestrator.md) - Per-project agent lifecycle management
- [Issue Backends](./issue-backends.md) - Pluggable issue tracking
- [Permissions](./permissions.md) - Permission system configuration
- [TUI](./tui.md) - Interactive terminal interface
