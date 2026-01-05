---
id: fa-4ga
status: closed
deps: []
links: []
created: 2026-01-03T15:58:39.13036574-08:00
type: task
priority: 2
---
# Validate agent PTY is running claude process

Ensure agents are actually running PTYs with the `claude` process in them.

**Requirements:**
- On agent spawn, verify the PTY successfully started `claude`
- Detect if the `claude` process exits unexpectedly (non-zero exit, crash, etc.)
- Handle edge cases: command not found, permission denied, etc.
- Update agent state appropriately on process failure
- Potentially add health check that periodically verifies process is alive

**Implementation notes:**
- Can check process status via PTY file descriptor or `os.Process` state
- Should distinguish between clean exit (user quit) vs crash
- Consider adding an `Error` or `Failed` agent state


