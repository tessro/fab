# Codex CLI Integration Research

This document details the Codex CLI JSONL output format, stdin protocol, and recommendations for the `CodexBackend` implementation.

## Overview

Codex CLI (v0.80.0) is OpenAI's coding agent CLI. It supports non-interactive operation via `codex exec` with JSONL output streaming.

Key differences from Claude Code:
- Codex uses a simpler event-based JSONL format (not nested message structures)
- Session continuity is achieved via `codex exec resume <session-id>` (not stdin messages)
- Approval handling uses built-in modes rather than external hooks
- No stdin protocol for sending follow-up messages during a session

## JSONL Output Format

Enable JSONL output with `codex exec --json`. Each line is a discrete event.

### Event Types

Based on testing and schema analysis, the main event types are:

| Event Type | Description |
|------------|-------------|
| `thread.started` | Session initialization with `thread_id` |
| `turn.started` | New turn beginning |
| `turn.completed` | Turn complete with usage stats |
| `item.started` | Tool use beginning (command execution, etc.) |
| `item.completed` | Tool use or message complete |
| `error` | Error event |
| `warning` | Warning event |

### Sample Output

```jsonl
{"type":"thread.started","thread_id":"019bac20-11a2-7061-9708-dda3b7642ac3"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"**Creating a new file using shell command**"}}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"/bin/zsh -lc \"printf '%s' 'Hello World' > hello.txt\"","aggregated_output":"","exit_code":null,"status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"/bin/zsh -lc \"printf '%s' 'Hello World' > hello.txt\"","aggregated_output":"","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Created `hello.txt` with `Hello World`."}}
{"type":"turn.completed","usage":{"input_tokens":8202,"cached_input_tokens":6400,"output_tokens":55}}
```

### Item Types

The `item.completed` events contain different item types:

| Item Type | Fields | Description |
|-----------|--------|-------------|
| `reasoning` | `text` | Agent reasoning/thinking |
| `command_execution` | `command`, `aggregated_output`, `exit_code`, `status` | Shell command execution |
| `agent_message` | `text` | Agent text response |

### Command Execution States

- `status: "in_progress"` - Command running
- `status: "completed"` - Command finished successfully (exit_code: 0)
- `status: "failed"` - Command failed (exit_code: non-zero)

### Usage Statistics

The `turn.completed` event includes token usage:

```json
{
  "type": "turn.completed",
  "usage": {
    "input_tokens": 8202,
    "cached_input_tokens": 6400,
    "output_tokens": 55
  }
}
```

## Session Management

### Starting a Session

```bash
codex exec --json --full-auto "your prompt here"
```

The `thread.started` event returns a `thread_id` (UUID format).

### Resuming a Session

Follow-up messages require using `codex exec resume`:

```bash
codex exec resume --json --full-auto "<thread-id>" "follow-up prompt"
```

This maintains conversation context from the original session.

### Stdin Protocol

**Important:** Unlike Claude Code, `codex exec` does NOT accept follow-up messages via stdin during a session. The only stdin use is:
- Reading the initial prompt: `echo "prompt" | codex exec -`

For multi-turn conversations, you must:
1. Let the first `codex exec` complete
2. Use `codex exec resume <thread-id> "new prompt"` for follow-ups

## Approval & Permission Handling

### Approval Modes

Set via `--ask-for-approval` or `-a`:

| Mode | Behavior |
|------|----------|
| `untrusted` | Only run "trusted" commands (ls, cat, etc.) without approval |
| `on-failure` | Run all, only ask if command fails |
| `on-request` | Model decides when to ask |
| `never` | Never ask for approval |

### Sandbox Modes

Set via `--sandbox` or `-s`:

| Mode | Behavior |
|------|----------|
| `read-only` | Read-only access |
| `workspace-write` | Write within workspace only |
| `danger-full-access` | Full system access |

### Full Auto Mode

`--full-auto` is equivalent to `--sandbox workspace-write --ask-for-approval on-request`.

### Bypass Mode

`--dangerously-bypass-approvals-and-sandbox` (or `--yolo`) bypasses all safety checks. Only use in externally sandboxed environments.

## Notification Hooks

Codex supports a `notify` configuration in `~/.codex/config.toml`:

```toml
notify = ["python3", "/path/to/notify.py"]
```

The notification script receives a JSON argument with event data:

```python
#!/usr/bin/env python3
import json, sys

notification = json.loads(sys.argv[1])
# notification["type"] == "agent-turn-complete"
# notification["thread-id"], notification["turn-id"]
# notification["last-assistant-message"]
```

**Note:** This is notification-only (one-way). It cannot approve/reject actions like Claude Code's hook system.

## Comparison with Claude Code

| Feature | Claude Code | Codex CLI |
|---------|-------------|-----------|
| Output format | Nested message structure | Flat event-based |
| Stdin input | Continuous JSONL messages | Initial prompt only |
| Multi-turn | Via stdin during session | Via `exec resume` |
| Approval hooks | External command hooks | Built-in modes only |
| Session ID | In stream messages | `thread_id` in `thread.started` |

## CodexBackend Implementation Recommendations

### 1. Fix Event Parsing

The current implementation expects a wrapper structure (`codexEvent`) that doesn't match the actual format. Events are flat with a `type` field at the top level.

Correct parsing:

```go
type codexEvent struct {
    Type     string          `json:"type"`      // "thread.started", "item.completed", etc.
    ThreadID string          `json:"thread_id"` // For thread.started
    Item     *codexItem      `json:"item"`      // For item.* events
    Usage    *codexUsage     `json:"usage"`     // For turn.completed
    Message  string          `json:"message"`   // For error/warning
}

type codexItem struct {
    ID               string `json:"id"`
    Type             string `json:"type"`      // "reasoning", "command_execution", "agent_message"
    Text             string `json:"text"`      // For reasoning, agent_message
    Command          string `json:"command"`   // For command_execution
    AggregatedOutput string `json:"aggregated_output"`
    ExitCode         int    `json:"exit_code"`
    Status           string `json:"status"`    // "in_progress", "completed", "failed"
}
```

### 2. Session Continuity

For multi-turn support, consider:
1. Capture `thread_id` from `thread.started` event
2. For follow-up messages, spawn new `codex exec resume <thread-id> "<message>"` process
3. Parse output the same way

Alternative: Use the experimental `codex app-server` which supports JSON-RPC over stdio for stateful sessions.

### 3. No Hook Support

Codex doesn't support external approval hooks. Options:
- Use `--full-auto` for automated operation with sandboxing
- Use `--ask-for-approval never` in trusted environments
- Accept that fab cannot intercept Codex tool executions for custom approval

### 4. FormatInputMessage

Since Codex doesn't accept stdin messages during a session, `FormatInputMessage` should either:
- Return an error indicating this isn't supported
- Format the message for a future `exec resume` call

## App-Server Protocol (Experimental)

Codex includes an experimental `codex app-server` that supports JSON-RPC over stdio:

```bash
codex app-server
```

This provides:
- `initialize` - Start server
- `thread/start` - Start new thread
- `thread/resume` - Resume existing thread
- `turn/start` - Submit user input
- `turn/interrupt` - Cancel current turn

The app-server emits events via JSON-RPC notifications matching the `EventMsg` schema.

### Consideration

If fab needs true bidirectional communication with Codex, the app-server approach may be preferable to spawning multiple `codex exec` processes.

## Verification

Validate the sample JSONL file and check event types:

```bash
$ jq -e '.type' internal/backend/testdata/codex-sample-output.jsonl > /dev/null && echo "All events have type field"
All events have type field
$ jq -r '.type' internal/backend/testdata/codex-sample-output.jsonl | sort -u
item.completed
item.started
thread.started
turn.completed
turn.started
```

## Examples

### Starting a New Session

```bash
$ codex exec --json --full-auto "Create hello.txt with 'Hello World'"
{"type":"thread.started","thread_id":"019bac20-11a2-7061-9708-dda3b7642ac3"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"**Creating a new file...**"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"...","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Created `hello.txt`."}}
{"type":"turn.completed","usage":{"input_tokens":8202,"output_tokens":55}}
```

### Resuming a Session

Use `thread_id` from the initial session:

```bash
$ codex exec resume --json --full-auto "019bac20-11a2-7061-9708-dda3b7642ac3" "Convert to uppercase"
{"type":"thread.started","thread_id":"019bac20-4c70-78d2-a452-e54fb16a161a"}
...
{"type":"turn.completed","usage":{"input_tokens":12471,"output_tokens":97}}
```

### Handling Failed Commands

Failed commands have `status: "failed"` and non-zero `exit_code`:

```bash
$ codex exec --json --full-auto "Read nonexistent.txt"
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"/bin/zsh -lc 'cat nonexistent.txt'","aggregated_output":"cat: nonexistent.txt: No such file or directory\n","exit_code":1,"status":"failed"}}
```

Full sample output is available in `internal/backend/testdata/codex-sample-output.jsonl`.

## References

- [Codex CLI Reference](https://developers.openai.com/codex/cli/reference/)
- [Non-interactive Mode](https://developers.openai.com/codex/noninteractive)
- [Configuration Reference](https://developers.openai.com/codex/config-reference/)
- [Advanced Configuration](https://developers.openai.com/codex/config-advanced/)
