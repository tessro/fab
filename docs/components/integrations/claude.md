# Claude Code Integration

## Purpose

The Claude backend is fab's default agent backend, enabling integration with Anthropic's Claude Code CLI. It configures hook-based permission handling, manages bidirectional stdin/stdout communication for multi-turn conversations, and parses Claude Code's nested JSONL stream format.

**Non-goals:**
- Does not implement the Claude API directly (delegated to Claude Code CLI)
- Does not manage Claude Code installation or configuration (assumed to be pre-installed)

## Interface

### Backend Interface

The `ClaudeBackend` implements the `Backend` interface:

| Method | Description |
|--------|-------------|
| `Name()` | Returns `"claude"` |
| `BuildCommand(cfg)` | Creates exec.Cmd with stream-json mode and hooks |
| `ParseStreamMessage(line)` | Parses Claude Code JSONL directly |
| `FormatInputMessage(content, sessionID)` | Formats user messages for stdin |
| `HookSettings(fabPath)` | Returns hook configuration for PreToolUse, PermissionRequest, Stop |

### Command Invocation

```bash
claude --output-format stream-json --input-format stream-json --verbose \
  --permission-mode default --plugin-dir <plugin-dir> --settings '<json>'
```

Key flags:
- `--output-format stream-json` - Enable JSONL output for programmatic parsing
- `--input-format stream-json` - Accept JSONL input for multi-turn conversations
- `--verbose` - Required when using stream-json output
- `--permission-mode default` - Use default permission handling with hooks
- `--settings` - JSON object containing hook configuration

### Hook Configuration

fab configures three hooks via the `--settings` flag:

| Hook | Matcher | Timeout | Purpose |
|------|---------|---------|---------|
| `PreToolUse` | `*` | 5 min | Intercept all tool calls for permission checking |
| `PermissionRequest` | `*` | 5 min | Handle explicit permission requests |
| `Stop` | (default) | 10 sec | Notify fab when agent becomes idle |

Hook commands:
```bash
fab hook PreToolUse
fab hook PermissionRequest
fab hook Stop
```

### Multi-Turn Communication

Claude Code accepts follow-up messages via stdin during a session. Messages use JSONL format:

```json
{"type":"user","message":{"role":"user","content":"follow-up message"},"session_id":"default"}
```

This enables efficient multi-turn conversations without spawning new processes.

### Stream Message Format

Claude Code outputs nested JSONL messages with the following structure:

| Type | Description |
|------|-------------|
| `assistant` | Agent responses with content blocks |
| `user` | Tool results |
| `system` | System messages (init, warnings) |
| `result` | Final result or error |

Content blocks within messages include:
- `text` - Text content
- `tool_use` - Tool invocation with ID, name, and input
- `tool_result` - Tool execution result

## Configuration

Configure Claude Code as the agent backend in `~/.config/fab/config.toml`:

### Per-Project Configuration

```toml
[[projects]]
name = "my-project"
remote-url = "git@github.com:user/my-project.git"
agent-backend = "claude"     # Fallback for planner and coding if not set
planner-backend = "claude"   # Backend for planning agents
coding-backend = "claude"    # Backend for coding agents
```

### Global Default

```toml
[defaults]
agent-backend = "claude"
```

Claude is the hardcoded default if no backend is specified.

### Backend Resolution Order

1. `planner-backend` / `coding-backend` (if explicitly set)
2. `agent-backend` (project-level fallback)
3. `[defaults].agent-backend` (global default)
4. `"claude"` (hardcoded default)

## Verification

Run the Claude backend unit tests:

```bash
$ go test ./internal/backend/... -run Claude -v
```

Test the hook settings generation:

```bash
$ go test ./internal/backend/... -run TestClaudeBackend_HookSettings -v
```

## Examples

### Session Lifecycle

1. **Start session**: fab spawns `claude --output-format stream-json ...`
2. **Initial prompt**: fab writes user message to stdin
3. **Tool calls**: fab receives tool_use blocks, runs hooks, sends results
4. **Follow-ups**: fab writes additional messages to stdin for multi-turn
5. **Idle detection**: Stop hook notifies fab when agent finishes responding

### Permission Flow

1. Agent calls a tool (e.g., Bash with `rm -rf /`)
2. Claude Code invokes `fab hook PreToolUse` with tool details
3. fab checks permission policy and may prompt user via TUI
4. Hook returns approval/denial JSON to Claude Code
5. Claude Code proceeds or aborts based on response

### Sample Input Message

```json
{"type":"user","message":{"role":"user","content":"Create a hello.txt file"},"session_id":"default","parent_tool_use_id":null}
```

### Sample Output Messages

```jsonl
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"I'll create the file for you."}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"tool_use","id":"toolu_01","name":"Write","input":{"file_path":"hello.txt","content":"Hello, World!"}}]}}
{"type":"user","message":{"role":"user","content":[{"type":"tool_result","tool_use_id":"toolu_01","content":"File written successfully"}]}}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Done! I've created hello.txt with the content \"Hello, World!\""}]}}
```

## Gotchas

- **Hook timeout**: Permission requests timeout after 5 minutes (`PermissionTimeout`). Hooks block until the user responds or timeout occurs.
- **Verbose required**: The `--verbose` flag is required when using `--output-format stream-json` or Claude Code will error.
- **FAB_AGENT_ID env var**: Hooks identify the agent via the `FAB_AGENT_ID` environment variable set by fab.
- **Session ID**: Default session ID is `"default"`. This enables message routing in multi-session scenarios.

## Decisions

**Hook-based permissions**: fab intercepts all tool calls via hooks rather than relying on Claude Code's built-in permission system. This enables centralized permission management across agents and TUI-based approval workflows.

**Bidirectional stdin**: Unlike Codex, Claude Code maintains a persistent process that accepts follow-up messages via stdin. This reduces latency for multi-turn conversations and preserves session state.

**5-minute hook timeout**: Matches fab's permission timeout. Hooks may block waiting for user input, so the timeout must be long enough for human response.

**Default permission mode**: Using `--permission-mode default` ensures Claude Code's built-in prompts don't interfere with fab's hook-based permission system.

## Paths

- `internal/backend/claude.go` - ClaudeBackend implementation
- `internal/backend/claude_test.go` - Backend unit tests
- `internal/backend/backend.go` - Backend interface definition
