---
id: fa-5al
status: closed
deps: []
links: []
created: 2026-01-04T19:00:38.775234-08:00
type: task
priority: 2
---
# Add command to clean up merged fab/* branches

Agent worktrees now create fab/<agentID> branches that are pushed to origin on task completion. These branches accumulate over time and need periodic cleanup.

Add a `fab branch cleanup` command (or similar) that:
- Lists fab/* branches that have been merged to main
- Deletes them from origin (with --dry-run option)
- Optionally deletes local refs too

Consider: auto-cleanup after successful merge, or as part of daemon maintenance loop.


