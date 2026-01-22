# 🚌 fab

A coding agent supervisor that manages multiple Claude Code or Codex instances across projects with automatic task orchestration.

## Features

- 🤖 **Multi-agent orchestration** - Run multiple Claude Code or Codex agents in parallel across different projects
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
| `fab project config get <project> <key>` | Get a configuration value |
| `fab project config set <project> <key> <value>` | Set configuration |

### Agents

| Command | Description |
|---------|-------------|
| `fab agent list [--project name]` | List running agents |
| `fab agent abort <id> [--force]` | Stop an agent |
| `fab agent claim <ticket-id>` | Claim a ticket (used by agents) |
| `fab agent done` | Signal task completion (used by agents) |
| `fab agent describe <description>` | Set agent status (used by agents) |
| `fab agent plan <prompt>` | Start a planning agent |
| `fab agent plan --project <name> <prompt>` | Plan in a project worktree |
| `fab agent plan list` | List planning agents |
| `fab agent plan stop <id>` | Stop a planning agent |

### Issues

| Command | Description |
|---------|-------------|
| `fab issue list [--status open]` | List issues |
| `fab issue show <id>` | Show issue details |
| `fab issue ready` | List ready/unblocked issues |
| `fab issue create <title>` | Create a new issue |
| `fab issue update <id>` | Update an issue |
| `fab issue close <id>` | Close an issue |
| `fab issue commit` | Commit and push issue changes |
| `fab issue comment <id> --body "..."` | Add a comment to an issue |
| `fab issue plan <id> --body "..."` | Upsert a plan section in an issue |

### Plan Storage

| Command | Description |
|---------|-------------|
| `fab plan write` | Write plan from stdin (uses FAB_AGENT_ID) |
| `fab plan read <id>` | Read a stored plan |
| `fab plan list` | List stored plans |

### Manager

| Command | Description |
|---------|-------------|
| `fab manager start <project>` | Start the manager agent for a project |
| `fab manager stop <project>` | Stop the manager agent |
| `fab manager status <project>` | Show manager agent status |
| `fab manager clear <project>` | Clear the manager agent's context |

### Other

| Command | Description |
|---------|-------------|
| `fab status [-a]` | Show daemon and project status |
| `fab tui` | Launch interactive TUI |
| `fab attach [projects...]` | Stream live agent output to stdout |
| `fab branch cleanup` | Clean up merged fab/* branches |
| `fab claims` | List claimed tickets |
| `fab version` | Print version information |

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

### Base Directory

By default, fab stores all data in `~/.fab/` (socket, PID file, logs, projects) and configuration in `~/.config/fab/config.toml`.

You can override this with the `--fab-dir` flag or `FAB_DIR` environment variable:

```bash
# Run fab in an isolated directory
fab --fab-dir /tmp/fab-test server start

# Or via environment variable
export FAB_DIR=/tmp/fab-test
fab server start
```

When `FAB_DIR` is set, all paths resolve under that directory:
- Socket: `$FAB_DIR/fab.sock`
- PID file: `$FAB_DIR/fab.pid`
- Log file: `$FAB_DIR/fab.log`
- Config: `$FAB_DIR/config/config.toml`
- Projects: `$FAB_DIR/projects/`

### Global Configuration

Config lives at `~/.config/fab/config.toml` (or `$FAB_DIR/config/config.toml` if FAB_DIR is set):

```toml
# Logging level: debug, info, warn, error
log-level = "info"

# API Provider Configuration
[providers.anthropic]
api-key = "sk-ant-..."  # Or use ANTHROPIC_API_KEY env var

[providers.openai]
api-key = "sk-..."      # Or use OPENAI_API_KEY env var

[providers.linear]
api-key = "lin_api_..."  # Linear API key for issue backend

[providers.github]
api-key = "ghp_..."      # GitHub token (or use GITHUB_TOKEN env var)

# LLM Authorization Settings
[llm-auth]
provider = "anthropic"  # or "openai"
model = "claude-haiku-4-5"

# Default settings for new projects
[defaults]
agent-backend = "claude"            # "claude" or "codex"
planner-backend = "claude"          # "claude" or "codex" (falls back to agent-backend)
coding-backend = "claude"           # "claude" or "codex" (falls back to agent-backend)
merge-strategy = "direct"           # "direct" or "pull-request"
issue-backend = "tk"                # "tk", "github", "gh", or "linear"
permissions-checker = "manual"      # "manual" or "llm"
autostart = false                   # true/false
max-agents = 3                      # 1-100

# Project definitions (use [[projects]] for each project)
[[projects]]
name = "myapp"
remote-url = "git@github.com:user/myapp.git"
max-agents = 3
autostart = false
issue-backend = "tk"           # "tk", "github", "gh", or "linear"
permissions-checker = "manual" # "manual" or "llm"
agent-backend = "claude"       # "claude" or "codex"
planner-backend = "claude"     # Backend for planning agents
coding-backend = "claude"      # Backend for coding agents
merge-strategy = "direct"      # "direct" or "pull-request"
allowed-authors = ["user1", "user2"]  # GitHub users allowed to create issues
linear-team = "TEAM-123"       # Linear team ID (for "linear" backend)
linear-project = "PROJECT-456" # Linear project ID (optional)
```

### Project Configuration

Configure projects via the CLI or directly in `config.toml`:

| Key | Values | Description |
|-----|--------|-------------|
| `max-agents` | 1-100 | Maximum concurrent agents (default: 3) |
| `autostart` | true/false | Start orchestration when daemon starts |
| `issue-backend` | tk/gh/github/linear | Issue tracking system |
| `permissions-checker` | manual/llm | Permission authorization method |
| `agent-backend` | claude/codex | Default CLI backend for agents |
| `planner-backend` | claude/codex | CLI backend for planning agents |
| `coding-backend` | claude/codex | CLI backend for coding agents |
| `merge-strategy` | direct/pull-request | How completed work is merged |
| `allowed-authors` | comma-separated | GitHub usernames allowed to create issues |
| `linear-team` | string | Linear team ID (required for linear backend) |
| `linear-project` | string | Linear project ID (optional) |

Example:
```bash
fab project config set myproject max-agents 5
fab project config set myproject permissions-checker llm
fab project config set myproject issue-backend gh
fab project config set myproject merge-strategy pull-request
```

### Environment Variables

| Variable | Description |
|----------|-------------|
| `FAB_DIR` | Base directory for all fab data (see [Base Directory](#base-directory)) |
| `FAB_SOCKET_PATH` | Override daemon socket path |
| `FAB_PID_PATH` | Override daemon PID file path |
| `FAB_AGENT_HOST_SOCKET_PATH` | Override agent host socket path |
| `FAB_PROJECT` | Set project context for agent commands |
| `FAB_AGENT_ID` | Agent identifier (set automatically by fab, used by agent commands) |

### Worktrees

Worktrees are stored in `~/.fab/projects/<project>/worktrees/wt-NNN/` (or `$FAB_DIR/projects/<project>/worktrees/wt-NNN/` if FAB_DIR is set).

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
fab agent plan "Add user authentication with OAuth"

# Plan within a specific project's worktree
fab agent plan --project myapp "Implement dark mode"

# List running planning agents
fab agent plan list

# Interact with planning agents in the TUI
fab tui
```

Planning agents:
- Can explore the codebase and ask clarifying questions
- Write plans via `fab plan write` (reads from stdin, uses agent ID)
- Are visible and interactive in the TUI
- Don't consume project agent slots

### Plan Storage

Plans are stored in `~/.fab/plans/` (or `$FAB_DIR/plans/` if FAB_DIR is set).

```bash
# Read a stored plan
fab plan read abc123

# List all stored plans
fab plan list
```

## Documentation

See the [Architecture documentation](docs/components/architecture.md) for detailed design documentation.

### Building the Site

The project website includes generated HTML documentation from the `docs/` directory. Generated files are not committed to git.

```bash
# Generate HTML docs from docs/ into site/public/docs/
make docs

# Build the full site (currently just generates docs)
make site

# Clean generated docs
make clean-docs
```

## License

MIT
