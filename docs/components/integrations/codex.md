# Codex CLI Integration

## Purpose

The Codex backend enables fab to use OpenAI's Codex CLI as an agent backend. It translates fab's agent protocol into Codex's event-based JSONL format, manages sessions using Codex's thread ID resume flow, and converts Codex output back to fab's canonical `StreamMessage` format.

**Non-goals:**
- Does not implement custom approval hooks (Codex uses built-in modes only)
- Does not provide bidirectional stdin communication during a session (Codex requires spawning a new process for follow-ups)

## Interface

### Backend Interface

The `CodexBackend` implements the `Backend` interface:

| Method | Description |
|--------|-------------|
| `Name()` | Returns `"codex"` |
| `BuildCommand(cfg)` | Creates exec.Cmd for `codex exec` or `codex exec resume` |
| `ParseStreamMessage(line)` | Converts Codex events to `StreamMessage` |
| `FormatInputMessage(content, sessionID)` | Formats submissions for stdin |
| `HookSettings(fabPath)` | Returns `nil` (Codex uses built-in approval modes) |

### Command Invocation

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

### JSONL Output Format

Codex outputs events as newline-delimited JSON:

| Event Type | Description |
|------------|-------------|
| `thread.started` | Session initialization with `thread_id` |
| `turn.started` | New turn beginning |
| `turn.completed` | Turn complete with usage stats |
| `item.started` | Tool use beginning |
| `item.completed` | Tool use or message complete |
| `error` | Error event |
| `warning` | Warning event |

The `item.completed` events contain different item types:

| Item Type | Fields | Description |
|-----------|--------|-------------|
| `reasoning` | `text` | Agent reasoning/thinking |
| `command_execution` | `command`, `aggregated_output`, `exit_code`, `status` | Shell command execution |
| `agent_message` | `text` | Agent text response |

### Approval & Sandbox Modes

Codex uses built-in approval modes rather than external hooks. fab cannot intercept Codex tool executions for custom approval.

**Approval modes** (via `--ask-for-approval` or `-a`):

| Mode | Behavior |
|------|----------|
| `untrusted` | Only run "trusted" commands without approval |
| `on-failure` | Run all, only ask if command fails |
| `on-request` | Model decides when to ask |
| `never` | Never ask for approval |

**Sandbox modes** (via `--sandbox` or `-s`):

| Mode | Behavior |
|------|----------|
| `read-only` | Read-only access |
| `workspace-write` | Write within workspace only |
| `danger-full-access` | Full system access |

## Configuration

Configure Codex as the agent backend in `~/.config/fab/config.toml`:

### Per-Project Configuration

```toml
[[projects]]
name = "my-project"
remote-url = "git@github.com:user/my-project.git"
agent-backend = "codex"      # Fallback for planner and coding if not set
planner-backend = "codex"    # Backend for planning agents
coding-backend = "codex"     # Backend for coding agents
```

### Global Default

```toml
[defaults]
agent-backend = "codex"
```

### Backend Resolution Order

1. `planner-backend` / `coding-backend` (if explicitly set)
2. `agent-backend` (project-level fallback)
3. `[defaults].agent-backend` (global default)
4. `"claude"` (hardcoded default)

## Verification

Run the Codex backend unit tests:

```bash
$ go test ./internal/backend/... -run Codex -v
```

Test the stream message parsing specifically:

```bash
$ go test ./internal/backend/... -run TestCodexBackend_ParseStreamMessage -v
```

## Examples

### Session Lifecycle

1. **Start session**: fab spawns `codex exec --json --full-auto -c ... "prompt"`
2. **Capture thread ID**: fab parses the `thread.started` event to extract `thread_id`
3. **Resume session**: For follow-up messages, fab spawns a new process using `codex exec resume --json --full-auto -c ... <thread-id> "follow-up"`

### Sample JSONL Output

```jsonl
{"type":"thread.started","thread_id":"019bac20-11a2-7061-9708-dda3b7642ac3"}
{"type":"turn.started"}
{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"**Creating a new file...**"}}
{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"...","status":"in_progress"}}
{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"...","exit_code":0,"status":"completed"}}
{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Created `hello.txt`."}}
{"type":"turn.completed","usage":{"input_tokens":8202,"cached_input_tokens":6400,"output_tokens":55}}
```

### Mixed Backend Configuration

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

## Gotchas

- **No stdin follow-ups**: Unlike Claude Code, Codex does not accept follow-up messages via stdin during a session. Each follow-up requires spawning a new process with `exec resume`.
- **Thread ID required**: The `thread_id` from `thread.started` must be captured and stored for session resumption.
- **No custom hooks**: Codex's approval system is built-in only. fab cannot implement custom approval logic like it does for Claude Code.
- **FormatInputMessage unused**: The method exists for interface compliance but fab's current implementation doesn't use it (follow-ups use `exec resume`).

## Decisions

**Event-based protocol**: Codex uses flat event messages rather than Claude Code's nested message structure. This requires event-type-specific conversion logic in `ParseStreamMessage`.

**Process-per-turn**: Codex requires a new process for each follow-up rather than stdin communication. This adds latency but simplifies state management since each process is independent.

**No hook interception**: Codex approval modes are built-in and cannot be overridden. fab passes `--full-auto` which uses `workspace-write` sandbox and `on-request` approval.

## Paths

- `internal/backend/codex.go` - CodexBackend implementation
- `internal/backend/backend.go` - Backend interface definition
