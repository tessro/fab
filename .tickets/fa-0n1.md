---
id: fa-0n1
status: closed
deps: []
links: []
created: 2026-01-03T15:02:13.919595726-08:00
type: task
priority: 2
---
# CLI fab project remove command

Add 'fab project remove <name>' to unregister projects.

Cleanup steps:
- Check no agents are running for this project
- Remove worktrees from ~/.fab/worktrees/<project>/
- Remove project from config file
- Confirm with user before destructive operations (--force to skip)

Usage:
  fab project remove myapp
  fab project remove myapp --force


