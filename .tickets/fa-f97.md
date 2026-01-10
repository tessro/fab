---
id: fa-f97
status: closed
deps: []
links: []
created: 2026-01-09T22:55:59.034984-08:00
type: feature
priority: 1
---
# detect manager starting/stopping while tui is running

The TUI needs to detect when the manager process starts or stops while the TUI is already running. TUI should detect manager startup/shutdown and update UI accordingly. Handle edge cases like crash, force kill, and restart.
