# CLI Reference

## Purpose

The fab CLI provides commands for managing the daemon, projects, agents, issues, and permissions. All commands communicate with the daemon via Unix socket IPC.

**Non-goals:**
- Does not run agents directly (agents are managed by the daemon)
- Does not modify git repositories (delegated to agent worktrees)

## Commands

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

## Verification

Run command help to verify installation:

```bash
$ fab --help
fab is a coding agent supervisor

Usage:
  fab [command]
...
```

Check command parsing:

```bash
$ fab server --help
Commands for managing the fab daemon

Usage:
  fab server [command]
...
```

## Examples

### Starting the daemon

```bash
fab server start
🚌 fab daemon started
```

### Adding and starting a project

```bash
fab project add git@github.com:user/repo.git --name myproject
fab project start myproject
```

### Checking status

```bash
fab status
Daemon: running
Projects: 2 active
Agents: 5 running
```

### Managing agent lifecycle

From within an agent worktree:

```bash
fab issue ready           # List available issues
fab agent claim 42        # Claim issue #42
fab agent describe "Fixing auth bug"
# ... do work ...
fab agent done            # Signal completion
```

## Paths

- `internal/cli/root.go` - Root command and global flags
- `internal/cli/server.go` - Daemon start/stop/restart
- `internal/cli/project.go` - Project management commands
- `internal/cli/agent.go` - Agent management commands
- `internal/cli/issue.go` - Issue/ticket commands
- `internal/cli/plan.go` - Plan storage commands
- `internal/cli/manager.go` - Manager agent commands
- `internal/cli/attach.go` - TUI launch command
- `internal/cli/status.go` - Status display command
- `internal/cli/hook.go` - Permission hook callbacks
