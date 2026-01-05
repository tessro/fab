---
id: fa-1qn
status: open
deps: []
links: []
created: 2026-01-03T13:22:52.471297619-08:00
type: task
priority: 2
---
# Manager agent for interactive user conversation

Add a dedicated "manager" agent that:
- Is always available for interactive conversation with the user
- Knows about all registered projects and their status
- Has access to beads across all projects (bd list, bd show, etc.)
- Can invoke fab CLI commands (fab status, fab start, fab stop, etc.)
- Helps user coordinate work across the fleet of agents
- Not auto-orchestrated - purely user-driven

Implementation:
- Separate from worker agents (doesn't consume worktree slot)
- Claude Code instance with fab-aware hooks/tools
- Accessible via TUI (dedicated pane or switchable view)
- Could be `fab manager` CLI command or integrated into `fab attach`

Use cases:
- "What's the status of all agents?"
- "Start working on the API project"
- "Show me blocked issues across all projects"
- "Assign FAB-42 to an agent in the fab project"


