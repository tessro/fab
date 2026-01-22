# Supervisor

## Purpose

The Supervisor is the central request handler for fab's daemon. It processes IPC requests from CLI commands, manages project orchestrators, coordinates agent lifecycles, handles permissions and user questions, and monitors agent health via heartbeats.

**Non-goals:**
- Does not run the daemon server itself (that's `daemon.Server`)
- Does not directly manage git worktrees (delegated to `project.Project`)
- Does not implement the LLM interaction protocol (delegated to `agent.Agent`)

## Interface

The Supervisor implements `daemon.Handler` and processes requests via the `Handle(ctx, req) *Response` method.

### IPC Message Types

| Category | Messages | Description |
|----------|----------|-------------|
| Server | `ping`, `shutdown` | Health check and graceful shutdown |
| Orchestration | `start`, `stop`, `status`, `agent.done` | Start/stop project orchestration, agent task completion |
| Projects | `project.add`, `project.remove`, `project.list`, `project.set` (deprecated), `project.config.*` | Manage registered projects |
| Agents | `agent.list`, `agent.create`, `agent.delete`, `agent.abort`, `agent.input`, `agent.output`, `agent.send_message`, `agent.chat_history`, `agent.describe`, `agent.idle` | Control agent lifecycle |
| Streaming | `attach`, `detach` | TUI streaming connections |
| Claims | `agent.claim`, `claim.list` | Ticket claim management |
| Commits | `commit.list` | List commits made by agents |
| Stats | `stats` | Aggregate agent statistics |
| Permissions | `permission.request`, `permission.respond`, `permission.list` | Tool permission handling |
| Questions | `question.request`, `question.respond` | AskUserQuestion tool handling |
| Manager | `manager.start`, `manager.stop`, `manager.status`, `manager.send_message`, `manager.chat_history`, `manager.clear_history` | Per-project manager agents |
| Planner | `plan.start`, `plan.stop`, `plan.list`, `plan.send_message`, `plan.chat_history` | Issue planning agents |

## Configuration

Configuration is loaded from `~/.config/fab/config.toml` via `config.GlobalConfig`.

Per-project settings are stored in the project registry.

## Verification

Run the unit tests:
```bash
$ go test ./internal/supervisor/... -v
ok
```

Run the heartbeat tests specifically:
```bash
$ go test ./internal/supervisor/... -run TestHeartbeat -v
ok
```

## Examples

### Starting orchestration for a project

When `fab start <project>` is executed, the CLI sends a `start` request:

1. Supervisor receives the request in `handleStart`
2. Calls `startOrchestrator` which:
   - Registers the project with the agent manager
   - Creates an orchestrator with the configured issue backend
   - Starts the orchestrator's polling loop

### Agent idle notification flow

When Claude Code finishes responding, the Stop hook calls `fab agent idle`:

1. Supervisor receives `agent.idle` in `handleAgentIdle`
2. Transitions the agent to idle state via `agent.MarkIdle()`
3. Calls `orchestrator.ExecuteKickstart()` to potentially resume the agent

### Heartbeat monitor detecting stuck agent

The heartbeat monitor runs periodically (default 30s):

1. Checks each active agent's last output time
2. If silent for 2 minutes: sends "continue" message
3. If still silent after 4 minutes total: kills the agent

## Gotchas

- **Permission timeout**: Permission requests timeout after 5 minutes (`PermissionTimeout`). If the user doesn't respond in time, the request fails.
- **Orchestrator vs agents**: Stopping an orchestrator doesn't automatically stop its agents unless explicitly requested. Use `StopHost` flag during shutdown to control this.
- **Agent state transitions**: Agents must follow valid state transitions. Calling `MarkIdle()` on a non-running agent will fail silently.

## Decisions

**Central handler pattern**: All IPC messages route through a single `Handle` method with a switch on message type. This keeps the protocol centralized and makes it easy to add new message types.

**Heartbeat monitor**: Agents can get stuck waiting for model responses. The heartbeat monitor ensures stuck agents are recovered by sending "continue" or killing them. The 2-minute timeout before "continue" balances responsiveness with avoiding false positives.

**Orchestrator per project**: Each project gets its own orchestrator instance. This isolates project state and allows independent start/stop control.

## Paths

- `internal/supervisor/supervisor.go` - Core Supervisor struct and Handle method
- `internal/supervisor/handle_*.go` - Request handlers by category
- `internal/supervisor/heartbeat.go` - Heartbeat monitor for stuck agent detection
- `internal/supervisor/orchestrator.go` - Orchestrator lifecycle management
- `internal/supervisor/rehydrate.go` - Agent reconnection after daemon restart
