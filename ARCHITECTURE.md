# fab - Coding Agent Supervisor

A Go 1.25 CLI tool that supervises multiple Claude Code agents across multiple projects, with automatic task orchestration via beads integration.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                                fab daemon                                    │
│  ┌─────────────┐  ┌─────────────────────────────────────────────────────┐   │
│  │   IPC       │  │  Supervisor                                         │   │
│  │  (Unix      │◄─┤  - Project registry                                 │   │
│  │   socket)   │  │  - Worktree pools                                   │   │
│  └──────┬──────┘  │  - Agent orchestration                              │   │
│         │         │  - Done detection + kickstart                       │   │
│         │         └─────────────────────────────────────────────────────┘   │
│         │                           │                                        │
│         ▼                           ▼                                        │
│  ┌─────────────┐    ┌──────────────────────────────────────────────────┐   │
│  │ CLI / TUI   │    │  Agents (PTY)                                     │   │
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
| `fab start <project> [--all]` | Start orchestration for a project (or all) |
| `fab stop <project> [--all]` | Stop orchestration for a project (or all) |
| `fab status` | Show daemon, supervisor, and agent status |
| `fab attach` | Launch interactive TUI |
| `fab project add <path> [--name NAME] [--max-agents N]` | Register a project |
| `fab project remove <name>` | Unregister a project |
| `fab project list` | List registered projects |

## TUI Layout

```
┌─ fab ─────────────────────────────────────────────────────────────────────┐
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
│ infra             │                                                       │
│ ────────────────  │                                                       │
│   agent-k6l [R]   │                                                       │
├───────────────────┴───────────────────────────────────────────────────────┤
│ j/k:nav  Enter:type  n:new agent  t:new task  d:delete  q:quit           │
└───────────────────────────────────────────────────────────────────────────┘
```

**Key features:**
- Top bar: status summary (agent counts), session stats (beads closed, commits made), usage meter
- Left rail: all agents grouped by project, state indicators [S]tarting/[R]unning/[I]dle/[D]one
- Main pane: selected agent's PTY (scrollable, interactive)
- In-game chat style input: `Enter` opens input line, type message, `Enter` again to send

**Key bindings:**
- `j/k` or arrows: navigate agents
- `n`: spawn new agent (prompts for project)
- `t`: create task via `bd create`
- `d`: delete selected agent
- `Enter`: open input line for PTY (in-game chat style)
- `Esc`: cancel input / return to navigation
- `q`: quit TUI (detach, agents keep running)

## Project & Worktree Model

```go
type Project struct {
    Name      string   // e.g., "myapp"
    Path      string   // e.g., "/home/tess/repos/myapp"
    MaxAgents int      // max concurrent agents (default: 3)
    Running   bool     // orchestration active for this project
    Worktrees []Worktree
}

type Worktree struct {
    Path      string   // e.g., "~/.fab/worktrees/myapp/wt-001"
    InUse     bool
    AgentID   string   // if in use
}
```

**Worktree pool behavior:**
- On project add: create `MaxAgents` worktrees upfront (avoids churn)
- Worktrees live in `~/.fab/worktrees/<project>/wt-NNN/`
- Each agent gets an exclusive worktree from the pool
- When agent exits, worktree returns to pool (git clean/reset)
- Pool size can be adjusted with `fab project set <name> --max-agents N`

## Supervisor Logic

**Agent-driven task picking**: Agents autonomously select their own tasks using `bd ready`. This design keeps the supervisor simple and leverages beads' dependency tracking - agents see only unblocked tasks and pick based on priority. No central scheduler is needed.

1. **Kickstart**: When an agent is spawned or becomes idle, send prompt:
   ```
   Run `bd ready` to find a task, then work on it.
   When done, close it with `bd close <id>`, then run `fab agent done`.
   ```

2. **Done detection**:
   - Primary: `fab agent done` IPC message from agent
   - Fallback: idle timeout (no output for configured duration)

3. **On done**:
   - Merge agent's branch to main (local merge workflow)
   - Return worktree to pool
   - Spawn replacement agent if orchestration is active

4. **User intervention**: User can type in any agent's PTY at any time; supervisor pauses kickstart for that agent until user is done (detected via PTY input silence).

## Usage Tracking

Parse Claude Code's local JSONL files (`~/.claude/projects/*/sessions/*.jsonl`):
- Sum `usage.input_tokens` and `usage.output_tokens` from assistant messages
- Map to billing blocks (5-hour windows for Pro/Max)
- Display as progress bar in TUI header

Fallback: periodically send `/usage` to an agent and parse response.

## Directory Structure

```
fab/
├── cmd/
│   └── fab/
│       └── main.go              # Entry point
├── internal/
│   ├── cli/
│   │   ├── root.go              # Cobra root command
│   │   ├── server.go            # server start/stop
│   │   ├── supervisor.go        # start/stop/status
│   │   ├── project.go           # project add/remove/list
│   │   └── attach.go            # TUI launch
│   ├── config/
│   │   └── config.go            # ~/.config/fab/config.toml
│   ├── daemon/
│   │   ├── daemon.go            # Daemonization
│   │   ├── server.go            # Unix socket RPC server
│   │   └── protocol.go          # IPC message types
│   ├── ipc/
│   │   └── client.go            # Client for CLI/TUI
│   ├── project/
│   │   ├── project.go           # Project type
│   │   └── worktree.go          # Worktree pool management
│   ├── agent/
│   │   ├── agent.go             # Agent type + lifecycle
│   │   ├── pty.go               # PTY spawning/IO
│   │   └── ringbuffer.go        # Output buffer
│   ├── supervisor/
│   │   ├── supervisor.go        # Main orchestration loop
│   │   └── done.go              # Completion detection
│   ├── usage/
│   │   └── usage.go             # JSONL parsing for usage stats
│   └── tui/
│       ├── app.go               # Bubbletea main model
│       ├── styles.go            # Lipgloss styles
│       └── components/
│           ├── header.go        # Top status summary + usage + stats
│           ├── agentlist.go     # Left rail agent list
│           ├── ptyview.go       # PTY viewport
│           ├── inputline.go     # In-game chat style input
│           └── helpbar.go       # Bottom help bar
├── go.mod
└── go.sum
```

## Dependencies

```go
require (
    github.com/charmbracelet/bubbletea v1.3+
    github.com/charmbracelet/lipgloss v1.1+
    github.com/charmbracelet/bubbles v0.21+
    github.com/creack/pty v1.1+
    github.com/spf13/cobra v1.10+
    github.com/pelletier/go-toml/v2 v2.2+
    github.com/google/uuid v1.6+
)
```

## Implementation Phases

### Phase 1: Foundation
1. Project scaffolding (go.mod, directory structure)
2. Config file support (`~/.config/fab/config.toml`)
3. IPC protocol + Unix socket server/client
4. Daemon lifecycle (start/stop, PID file, signals)
5. CLI: `fab server start/stop`, `fab status`

### Phase 2: Projects & Worktrees
1. Project registry (add/remove/list)
2. Worktree pool creation and management
3. Git operations (checkout, clean, reset)
4. CLI: `fab project add/remove/list`

### Phase 3: Agent Management
1. Agent type with PTY spawning
2. Ring buffer for output capture
3. Agent lifecycle (create, destroy, state machine)
4. Basic supervisor (no orchestration yet)

### Phase 4: TUI
1. Bubbletea app structure
2. Header component (status summary, usage placeholder)
3. Agent list component (grouped by project)
4. PTY view component (scrolling, focus, input)
5. CLI: `fab attach`

### Phase 5: Orchestration
1. Done detection (IPC message, idle timeout fallback)
2. Kickstart logic (agent-driven task picking via `bd ready`)
3. Local merge workflow on task completion
4. CLI: `fab start <project>/stop <project>`

### Phase 6: Usage & Polish
1. JSONL parser for usage stats
2. Usage display in TUI
3. Error handling and recovery
4. Config tuning and validation

## Key Files

| File | Purpose |
|------|---------|
| `internal/daemon/server.go` | Unix socket RPC, bridges CLI to supervisor |
| `internal/supervisor/supervisor.go` | Core orchestration: per-project agent management, kickstart loop |
| `internal/agent/pty.go` | PTY spawning, I/O, resize handling |
| `internal/project/worktree.go` | Worktree pool: create, assign, recycle |
| `internal/tui/app.go` | Bubbletea model, component coordination |
| `internal/tui/components/ptyview.go` | PTY rendering with ANSI passthrough |
| `internal/tui/components/header.go` | Status summary across all projects + usage |
