# Codex CLI Integration

This document describes how fab integrates with OpenAI's Codex CLI as an agent backend.

## Using Codex with fab

fab supports two agent backends: `claude` (default) and `codex`. You can configure the backend at two levels:

### Project Configuration

In `~/.config/fab/config.toml`, configure backends per-project using the `[[projects]]` array:

```toml
[[projects]]
name = "my-project"
remote-url = "git@github.com:user/my-project.git"
agent-backend = "codex"      # Fallback for planner and coding if not set
planner-backend = "codex"    # Backend for planning agents
coding-backend = "codex"     # Backend for coding agents
```

The backend resolution order is:
1. `planner-backend` / `coding-backend` (if explicitly set)
2. `agent-backend` (project-level fallback)
3. `[defaults].agent-backend` (global default, see below)
4. `"claude"` (hardcoded default)

### Global Defaults

Set a global default in the `[defaults]` section:

```toml
[defaults]
agent-backend = "codex"
```

### Example Configuration

```toml
[defaults]
agent-backend = "claude"

[[projects]]
name = "openai-project"
remote-url = "git@github.com:user/openai-project.git"
agent-backend = "codex"

[[projects]]
name = "anthropic-project"
remote-url = "git@github.com:user/anthropic-project.git"
# Uses global default: claude
```

## Command Invocation

fab invokes Codex using the following command pattern:

**New session:**
```bash
codex exec --json --full-auto -c 'model_reasoning_effort="xhigh"' "initial prompt"
```

**Resume session:**
```bash
codex exec resume --json --full-auto -c 'model_reasoning_effort="xhigh"' <thread-id> "follow-up prompt"
```

Key flags:
- `--json` - Enable JSONL output for programmatic parsing
- `--full-auto` - Equivalent to `--sandbox workspace-write --ask-for-approval on-request`
- `-c 'model_reasoning_effort="xhigh"'` - Configure high reasoning effort

## Session Handling

fab manages Codex sessions using the thread ID resume flow:

1. **Start session**: fab spawns `codex exec --json --full-auto -c ... "prompt"`
2. **Capture thread ID**: fab parses the `thread.started` event to extract `thread_id`
3. **Resume session**: For follow-up messages, fab spawns a new process using `codex exec resume --json --full-auto -c ... <thread-id> "follow-up"`

This differs from Claude Code, which accepts follow-up messages via stdin during a session. Codex requires spawning a new process with `exec resume` for each follow-up.

## JSONL Output Format

Codex outputs events as newline-delimited JSON. fab parses these events and converts them to its canonical `StreamMessage` format.

### Event Types

| Event Type | Description |
|------------|-------------|
| `thread.started` | Session initialization with `thread_id` |
| `turn.started` | New turn beginning |
| `turn.completed` | Turn complete with usage stats |
| `item.started` | Tool use beginning |
| `item.completed` | Tool use or message complete |
| `error` | Error event |
| `warning` | Warning event |

### Item Types

The `item.completed` events contain different item types:

| Item Type | Fields | Description |
|-----------|--------|-------------|
| `reasoning` | `text` | Agent reasoning/thinking |
| `command_execution` | `command`, `aggregated_output`, `exit_code`, `status` | Shell command execution |
| `agent_message` | `text` | Agent text response |

### Sample Output

```jsonl
{"type":"thread.started","thread_id":"019bac20-11a2-7061-9708-dda3b7642ac3"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"**Creating a new file...**"}}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"...","status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"...","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Created `hello.txt`."}}
{"type":"turn.completed","usage":{"input_tokens":8202,"cached_input_tokens":6400,"output_tokens":55}}
```

## Approval & Sandbox Modes

Codex uses built-in approval modes rather than external hooks. fab cannot intercept Codex tool executions for custom approval.

### Approval Modes

Set via `--ask-for-approval` or `-a`:

| Mode | Behavior |
|------|----------|
| `untrusted` | Only run "trusted" commands without approval |
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

## Comparison with Claude Code

| Feature | Claude Code | Codex CLI |
|---------|-------------|-----------|
| Output format | Nested message structure | Flat event-based |
| Multi-turn | Via stdin during session | Via `exec resume` |
| Approval hooks | External command hooks | Built-in modes only |
| Session ID | In stream messages | `thread_id` in `thread.started` |

---

## Implementation Notes

The following sections contain research notes and implementation details that may be useful for debugging or extending the Codex backend.

### FormatInputMessage

The `CodexBackend.FormatInputMessage` method exists in `internal/backend/codex.go` but is not used by fab's current implementation. Codex does not accept stdin messages during a session - follow-ups must use `exec resume`. The method is retained for interface compliance and potential future use with the experimental `codex app-server`.

### Notification Hooks

Codex supports a `notify` configuration in `~/.codex/config.toml`:

```toml
notify = ["python3", "/path/to/notify.py"]
```

This is notification-only (one-way) and cannot approve/reject actions like Claude Code's hook system.

### App-Server Protocol (Experimental)

Codex includes an experimental `codex app-server` that supports JSON-RPC over stdio:

```bash
codex app-server
```

This provides bidirectional communication (`thread/start`, `thread/resume`, `turn/start`, `turn/interrupt`) and may be useful if fab needs true bidirectional communication without spawning multiple processes.

### References

- [Codex CLI Reference](https://developers.openai.com/codex/cli/reference/)
- [Non-interactive Mode](https://developers.openai.com/codex/noninteractive)
- [Configuration Reference](https://developers.openai.com/codex/config-reference/)
