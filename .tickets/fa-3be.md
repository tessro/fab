---
id: fa-3be
status: open
deps: []
links: []
created: 2026-01-10T07:49:00.351073-08:00
type: bug
priority: 1
---
# tui: prune dead agents when reconnecting

When the TUI reconnects to the server (e.g. after server restart), old/dead agents persist in the UI. The TUI should prune agents that no longer exist on the server when reconnecting.
