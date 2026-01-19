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

## Verification

Build and run the test suite:

```bash
$ go build ./...
$ go test ./...
ok      github.com/tessro/fab/internal/...
```

Check that the daemon starts successfully:

```bash
$ fab server start
🚌 fab daemon started
$ fab status
Daemon: running
```

## Examples

### Basic workflow

Start the daemon, add a project, and launch the TUI:

```bash
fab server start
fab project add git@github.com:user/repo.git --name myproject
fab project start myproject
fab tui
```

### Agent claiming a ticket

From within an agent worktree:

```bash
fab issue ready           # List available issues
fab agent claim 42        # Claim issue #42
fab agent describe "Implementing feature X"
# ... do work ...
fab agent done            # Signal completion
```

## See Also

- [CLI Reference](./cli.md) - Complete command reference and directory structure
- [Supervisor](./supervisor.md) - Central daemon request handler
- [Orchestrator](./orchestrator.md) - Per-project agent lifecycle management
- [Issue Backends](./issue-backends.md) - Pluggable issue tracking
- [Permissions](./permissions.md) - Permission system configuration
- [TUI](./tui.md) - Interactive terminal interface
