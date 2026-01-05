---
id: fa-dmd
status: closed
deps: []
links: []
created: 2026-01-03T15:02:13.43204687-08:00
type: task
priority: 2
---
# Add checklocks and annotate mutexes

Add the checklocks static analyzer to catch mutex misuse.

Tasks:
- Add github.com/sasha-s/go-deadlock or use go vet's checklocks
- Annotate structs with mutex fields using // +checklocks comments
- Run as part of CI lint step
- Coarse-grained locking is fine - don't over-complicate

Key areas to annotate:
- Agent struct (pty access, state)
- Registry (agent map)
- Project/Worktree pools
- Supervisor state


