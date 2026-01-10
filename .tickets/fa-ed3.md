---
id: fa-ed3
status: closed
deps: []
links: []
created: 2026-01-10T07:35:22.493228-08:00
type: feature
priority: 1
---
# fab plan command for spawning planning agents

Add 'fab plan [--project <name>] [prompt]' command that spawns an agent in plan mode. Requirements: Agent should be visible in TUI. Agent should be able to ask questions. Agent should run in a worktree for the project if a project is specified. When agent runs ExitPlanMode, the plan should be written to .fab/plans/<agentId>.md and agent should be shut down. Planning agents should be creatable from TUI. Planning agents are not subject to max-agents limit.
