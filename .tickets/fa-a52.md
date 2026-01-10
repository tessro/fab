---
id: fa-a52
status: open
deps: []
links: []
created: 2026-01-10T07:48:03.085009-08:00
type: feature
priority: 1
---
# replace worktree pool with worktree per agent

Replace the current worktree pool implementation with a worktree-per-agent approach. Each agent should have its own worktree keyed by agent ID (e.g. wt-f2c372). Worktree should be created when the agent starts and removed when the agent finishes.
