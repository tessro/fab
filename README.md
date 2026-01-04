# 🚌 fab

A coding agent supervisor that manages multiple Claude Code instances across projects with automatic task orchestration via [beads](https://github.com/tessro/beads).

## Features

- **Multi-agent orchestration** - Run multiple Claude Code agents in parallel across different projects
- **Worktree isolation** - Each agent gets its own git worktree for conflict-free parallel development
- **Automatic task assignment** - Agents automatically pick up tasks from beads (`bd ready`)
- **Done detection** - Recognizes when agents complete tasks and recycles them for new work
- **Interactive TUI** - Monitor and interact with all agents from a single terminal interface
- **Manual/auto modes** - Review agent actions before execution or let them run autonomously

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
   fab start myproject
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
| `fab start <project>` | Start orchestration for a project |
| `fab stop <project>` | Stop orchestration for a project |
| `fab status` | Show daemon and project status |
| `fab tui` | Launch interactive TUI |
| `fab project add <path>` | Register a project |
| `fab project remove <name>` | Unregister a project |
| `fab project list` | List registered projects |

## How It Works

fab creates a pool of git worktrees for each project. When orchestration starts, agents are spawned and assigned worktrees. Each agent:

1. Runs `bd ready` to find an available task
2. Works on the task in its isolated worktree
3. Commits and pushes changes
4. Closes the task with `bd close`
5. Signals completion with `fab agent done`

The orchestrator then recycles the agent for the next task.

## Configuration

Config lives at `~/.config/fab/config.toml`. Worktrees are stored in `~/.fab/worktrees/<project>/`.

## Documentation

See [ARCHITECTURE.md](ARCHITECTURE.md) for detailed design documentation.

## License

MIT
