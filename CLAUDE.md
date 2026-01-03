# fab - Claude Code Instructions

## Branding

Use the bus emoji 🚌 to brand the project:
- README.md header
- CLI output (e.g., `🚌 fab daemon started`)
- TUI header
- Error messages where appropriate

## Project Context

fab is a coding agent supervisor - it manages multiple Claude Code instances across projects with automatic task orchestration via beads.

## Code Style

- Go 1.25
- Use standard Go project layout (cmd/, internal/)
- Prefer simplicity over abstraction
- Error messages should be actionable

## Key Conventions

- Config lives in `~/.config/fab/config.toml`
- Daemon socket at `~/.fab/fab.sock`
- Worktrees in `<project>/.fab-worktrees/`
- Issue prefix: `FAB-`
