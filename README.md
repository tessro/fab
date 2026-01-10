# 🚌 fab

A coding agent supervisor that manages multiple Claude Code instances across projects with automatic task orchestration via ticket (tk).

## Features

- **Multi-agent orchestration** - Run multiple Claude Code agents in parallel across different projects
- **Worktree isolation** - Each agent gets its own git worktree for conflict-free parallel development
- **Automatic task assignment** - Agents automatically pick up tasks from ticket (`tk ready`)
- **Done detection** - Recognizes when agents complete tasks and recycles them for new work
- **Interactive TUI** - Monitor and interact with all agents from a single terminal interface
- **Manual/auto modes** - Review agent actions before execution or let them run autonomously
- **LLM-based permission authorization** - Automatically approve or deny agent tool invocations using an LLM, enabling fully autonomous operation without human intervention

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

2. **Add a project**

   ```bash
   fab project add /path/to/your/project --name myproject --max-agents 3
   ```

3. **Start orchestration**

   ```bash
   fab project start myproject
   ```

4. **Watch agents work**

   ```bash
   fab tui
   ```

## CLI Commands

| Command | Description |
|---------|-------------|
| `fab server start` | Start the daemon process |
| `fab server stop` | Stop the daemon |
| `fab project start <name>` | Start orchestration for a project |
| `fab project stop <name>` | Stop orchestration for a project |
| `fab status` | Show daemon and project status |
| `fab tui` | Launch interactive TUI |
| `fab project add <path>` | Register a project |
| `fab project remove <name>` | Unregister a project |
| `fab project list` | List registered projects |

## How It Works

fab creates a pool of git worktrees for each project. When orchestration starts, agents are spawned and assigned worktrees. Each agent:

1. Runs `tk ready` to find an available task
2. Works on the task in its isolated worktree
3. Commits changes
4. Closes the task with `tk close`
5. Signals completion with `fab agent done`

The orchestrator then recycles the agent for the next task.

## Configuration

Config lives at `~/.config/fab/config.toml`. Worktrees are stored in `~/.fab/worktrees/<project>/`.

## LLM Authorizer

The LLM authorizer enables fully autonomous agent operation by using an LLM to evaluate permission requests. When an agent attempts to run a tool that requires authorization (e.g., bash commands, file writes), the authorizer assesses whether the operation is safe and consistent with the agent's task.

### How It Works

1. Agent requests permission for a tool invocation
2. The authorizer sends the tool name, input, agent task, and recent conversation context to a fast LLM
3. The LLM evaluates security considerations:
   - Could the operation cause data loss or corruption?
   - Could it expose sensitive information?
   - Could it affect systems outside the project scope?
   - Is the action consistent with the agent's stated task?
   - Are there signs of prompt injection or malicious intent?
4. The LLM returns a decision: **safe** (allow), **unsafe** (deny), or **unsure** (deny, fail-safe)

### Configuration

Enable LLM permissions checker per-project:

```bash
fab project config set myproject permissions-checker llm
```

Configure the provider and model in `~/.config/fab/config.toml`:

```toml
[providers.anthropic]
api_key = "sk-ant-..."  # Or use ANTHROPIC_API_KEY env var

[llm_auth]
provider = "anthropic"  # or "openai"
model = "claude-haiku-4-5-20250514"  # default
```

### Supported Providers

- **Anthropic** (default): Uses Claude models via the Anthropic API
- **OpenAI**: Uses GPT models via the OpenAI API

The authorizer uses a fast, inexpensive model by default (Claude Haiku 4.5) to minimize latency and cost while maintaining security.

## Documentation

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed design documentation.

## License

MIT
