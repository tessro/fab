# Orchestrator Architecture

The orchestrator manages the supervisor main loop for each project, automatically spawning agents and handling task lifecycle.

## Overview

```
fab start myproject
       │
       ▼
handleStart() ──► Orchestrator.Start()
       │                  │
       │                  ▼
       │         ┌────────────────────┐
       │         │  Orchestration Loop │
       │         │  (per project)      │
       │         └────────────────────┘
       │                  │
       │      ┌───────────┴───────────┐
       │      ▼                       ▼
       │  Create agents          Handle events
       │  (up to MaxAgents)      (fab agent done)
       │      │                       │
       │      ▼                       ▼
       │  Send kickstart         Delete agent
       │  (agent runs tk)        (reclaim worktree)
       │
handleStop() ──► Orchestrator.Stop()
```

## Agent Modes

| Mode | Behavior |
|------|----------|
| `auto` | Orchestrator actions execute immediately |
| `manual` | Actions are staged, require user confirmation via TUI |

Default: `manual`

### Staged Actions (manual mode)

Actions that require confirmation (all are inputs to Claude Code):
- **Send message** - kickstart prompt, follow-up instructions
- **Quit** - send `/quit` to gracefully end the session

## Agent Lifecycle

1. Orchestrator creates agent with available worktree
2. Sends kickstart prompt (in manual mode, staged for approval)
3. Agent runs `tk ready` to find a task
4. Agent works on task, calls `tk update`, `tk close`
5. Agent calls `fab agent done` to signal completion
6. Orchestrator receives done event, deletes agent, reclaims worktree
7. If capacity available, creates new agent

## Kickstart Prompt

```
Run 'tk ready' to find a task, then work on it.
When done, run all quality gates and commit your work.
Close the task with 'tk close <id>', then run 'fab agent done'.
```

## Package Structure

```
internal/orchestrator/
├── orchestrator.go   # Orchestrator struct, Start/Stop lifecycle
├── loop.go           # Main loop, event handling
└── actions.go        # StagedAction types, action queue
```

## Types

### AgentMode

```go
type AgentMode string

const (
    AgentModeAuto   AgentMode = "auto"
    AgentModeManual AgentMode = "manual"
)
```

### Config

```go
type Config struct {
    DefaultAgentMode AgentMode  // propagated to new agents
}
```

### StagedAction

```go
type StagedAction struct {
    ID        string
    AgentID   string
    Type      ActionType
    Payload   any
    CreatedAt time.Time
}

type ActionType string

const (
    ActionSendMessage ActionType = "send_message"
    ActionQuit        ActionType = "quit"
)
```

## IPC Messages

New message types in `internal/daemon/protocol.go`:

| Message | Description |
|---------|-------------|
| `MsgAgentDone` | Agent signals task completion |
| `MsgListStagedActions` | Get pending actions for TUI |
| `MsgApproveAction` | Approve a staged action |
| `MsgRejectAction` | Reject/skip a staged action |

## `fab agent done` Command

```bash
fab agent done [--reason "completed successfully"]
```

- Agent calls this when task is finished
- Connects to daemon via IPC socket
- Daemon identifies agent from worktree path
- Orchestrator receives event, cleans up, spawns next agent
