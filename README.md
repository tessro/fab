# 🚌 fab

A coding agent supervisor that manages multiple Claude Code instances across projects with automatic task orchestration.

## Features

- 🤖 **Multi-agent orchestration** - Run multiple Claude Code agents in parallel across different projects
- 🌲 **Elastic worktree pool** - Each agent gets its own git worktree; pool size scales from 1-100 agents per project
- 🎫 **Pluggable issue backends** - Automatic task assignment from tk, GitHub Issues, or Linear
- ✅ **Done detection** - Recognizes when agents complete tasks and recycles them for new work
- 📺 **Interactive TUI** - Monitor and interact with all agents from a single terminal interface
- 🛡️ **Strong permission controls** - TOML-based rule engine with pattern matching for fine-grained access control
- 🧠 **LLM-based authorization** - Fully autonomous operation using an LLM to evaluate permission requests
- 📋 **Plan mode** - Dedicated planning agents for exploring codebases and designing implementation approaches

## Installation

### From source

```bash
git clone https://github.com/tessro/fab
cd fab
go build -o fab ./cmd/fab
```

### With Go

```bash
go install github.com/tessro/fab/cmd/fab@latest
```

## Quick Start

1. **Start the daemon**

   ```bash
   fab server start
   ```

2. **Add a project** (from local path or git URL)

   ```bash
   fab project add /path/to/your/project --name myproject --max-agents 3
   # or
   fab project add git@github.com:user/repo.git --name myproject
   ```

3. **Configure permissions** (optional: enable autonomous mode)

   ```bash
   fab project config set myproject permissions-checker llm
   ```

4. **Start orchestration**

   ```bash
   fab project start myproject
   ```

5. **Watch agents work**

   ```bash
   fab tui
   ```

## CLI Commands

### Server

| Command | Description |
|---------|-------------|
| `fab server start` | Start the daemon process |
| `fab server stop` | Stop the daemon |
| `fab server restart` | Restart the daemon |

### Projects

| Command | Description |
|---------|-------------|
| `fab project add <path-or-url>` | Register a project |
| `fab project list` | List registered projects |
| `fab project start [name] [--all]` | Start orchestration |
| `fab project stop [name] [--all]` | Stop orchestration |
| `fab project remove <name>` | Unregister a project |
| `fab project config show <project>` | Show all configuration |
| `fab project config set <project> <key> <value>` | Set configuration |

### Agents

| Command | Description |
|---------|-------------|
| `fab agent list [--project name]` | List running agents |
| `fab agent abort <id> [--force]` | Stop an agent |
| `fab agent claim <ticket-id>` | Claim a ticket (used by agents) |
| `fab agent done` | Signal task completion (used by agents) |
| `fab agent describe <description>` | Set agent status (used by agents) |

### Issues

| Command | Description |
|---------|-------------|
| `fab issue list [--status open]` | List issues |
| `fab issue show <id>` | Show issue details |
| `fab issue ready` | List ready/unblocked issues |
| `fab issue create <title>` | Create a new issue |
| `fab issue close <id>` | Close an issue |

### Planning

| Command | Description |
|---------|-------------|
| `fab plan [prompt]` | Start a planning agent |
| `fab plan --project <name> [prompt]` | Plan in a project worktree |
| `fab plan list` | List planning agents |
| `fab plan chat <id>` | Chat with a planning agent |
| `fab plan stop <id>` | Stop a planning agent |

### Other

| Command | Description |
|---------|-------------|
| `fab status [-a]` | Show daemon and project status |
| `fab tui` / `fab attach` | Launch interactive TUI |
| `fab branch cleanup` | Clean up merged fab/* branches |
| `fab claims` | List claimed tickets |

## How It Works

fab creates a pool of git worktrees for each project. When orchestration starts, agents are spawned and assigned worktrees. Each agent:

1. Runs `fab issue ready` to find an available task
2. Claims the task with `fab agent claim <id>`
3. Works on the task in its isolated worktree
4. Commits changes
5. Closes the task with `fab issue close <id>`
6. Signals completion with `fab agent done` (rebases onto main and merges)

The orchestrator then recycles the worktree for the next agent.

## Configuration

### Global Configuration

Config lives at `~/.config/fab/config.toml`:

```toml
# Logging level: debug, info, warn, error
log_level = "info"

# API Provider Configuration
[providers.anthropic]
api-key = "sk-ant-..."  # Or use ANTHROPIC_API_KEY env var

[providers.openai]
api-key = "sk-..."      # Or use OPENAI_API_KEY env var

# LLM Authorization Settings
[llm_auth]
provider = "anthropic"  # or "openai"
model = "claude-haiku-4-5-20250514"
```

### Project Configuration

| Key | Values | Description |
|-----|--------|-------------|
| `max-agents` | 1-100 | Maximum concurrent agents (default: 3) |
| `autostart` | true/false | Start orchestration when daemon starts |
| `issue-backend` | tk/gh/github/linear | Issue tracking system |
| `permissions-checker` | manual/llm | Permission authorization method |

Example:
```bash
fab project config set myproject max-agents 5
fab project config set myproject permissions-checker llm
fab project config set myproject issue-backend gh
```

### Worktrees

Worktrees are stored in `~/.fab/projects/<project>/worktrees/wt-NNN/`.

## Permission System

fab includes a TOML-based permission rule engine for controlling what agents can do. Rules are evaluated in order; first match wins.

### Manual Mode (default)

Agents request permission through the TUI for sensitive operations. You review and approve each action.

### LLM Mode (autonomous)

An LLM evaluates each permission request for safety and task consistency:

1. Agent requests permission for a tool invocation
2. The LLM evaluates security considerations:
   - Could the operation cause data loss?
   - Could it expose sensitive information?
   - Is it consistent with the agent's stated task?
   - Are there signs of prompt injection?
3. Returns: **safe** (allow), **unsafe** (deny), or **unsure** (deny, fail-safe)

Enable per-project:
```bash
fab project config set myproject permissions-checker llm
```

### Permission Rules

Copy `permissions.toml.default` to `~/.config/fab/permissions.toml` to customize rules:

```toml
# Allow all file reads
[[rules]]
tool = "Read"
action = "allow"

# Allow writes within worktree only
[[rules]]
tool = "Write"
action = "allow"
pattern = "/:*"

# Allow safe git commands
[[rules]]
tool = "Bash"
action = "allow"
patterns = ["git status:*", "git diff:*", "git log:*"]
```

Pattern syntax:
- `":*"` matches everything
- `"prefix:*"` matches values starting with "prefix"
- `"/:*"` for worktree-scoped paths
- `"//:*"` for absolute paths

## Plan Mode

Planning agents explore codebases and design implementation approaches without counting against the project's max-agents limit.

```bash
# Start a planning session
fab plan "Add user authentication with OAuth"

# Plan within a specific project's worktree
fab plan --project myapp "Implement dark mode"

# Chat with the planning agent
fab plan chat abc123
```

Planning agents:
- Can explore the codebase and ask clarifying questions
- Write plans to `.fab/plans/<agent-id>.md`
- Are visible and interactive in the TUI
- Don't consume project agent slots

## Documentation

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed design documentation.

## License

MIT
