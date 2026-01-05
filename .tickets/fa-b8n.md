---
id: fa-b8n
status: closed
deps: []
links: []
created: 2026-01-03T13:22:52.11651989-08:00
type: task
priority: 2
---
# Make worktrees optional per project

Add a per-project config option to disable worktree pooling.

When disabled:
- Agents run directly in the main project directory
- No .fab-worktrees/ created
- Useful for projects where worktree overhead isn't worth it
- Default: enabled (current behavior)

Config example:
```toml
[[projects]]
name = "myapp"
path = "/home/user/myapp"
max_agents = 3
use_worktrees = false  # new field, default true
```


