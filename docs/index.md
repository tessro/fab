# fab Documentation

fab is a coding agent supervisor that manages multiple Claude Code or Codex instances across projects with automatic task orchestration. It spawns agents in isolated git worktrees, assigns them issues from your tracker, and coordinates their work back to your main branch.

## How It Works

1. fab creates a pool of git worktrees for each registered project
2. When orchestration starts, agents spawn and pick up issues from your tracker
3. Each agent works in isolation—implementing, testing, and committing changes
4. When finished, fab rebases and merges their work to main (or opens a PR)

## Guides

| Guide | Description |
|-------|-------------|
| [Codex CLI Integration](./codex-integration.md) | Using OpenAI's Codex CLI as an agent backend |

## Components

| Component | Description |
|-----------|-------------|
| [Configuration](./components/configuration.md) | Global and per-project settings including API keys, backends, and merge strategies |
| [Supervisor](./components/supervisor.md) | Central daemon that handles CLI requests, manages orchestrators, and coordinates permissions |
| [Orchestrator](./components/orchestrator.md) | Per-project lifecycle manager that spawns agents and coordinates merges |
| [Issue Backends](./components/issue-backends.md) | Pluggable issue tracking: tk (file-based), GitHub Issues, or Linear |
| [Permissions](./components/permissions.md) | Configure which agent actions are auto-approved, denied, or require manual review |

## Quick Reference

Start the daemon and add a project:
```bash
fab server start
fab project add /path/to/repo --name myproject
```

Start orchestration and watch agents work:
```bash
fab project start myproject
fab tui
```

See the [README](../README.md) for installation, full CLI reference, and configuration examples.
