# Permissions & Hooks

## Purpose

The permissions system controls which Claude Code tool invocations are automatically allowed, denied, or require user approval through the TUI. It integrates with Claude Code's hook mechanism to intercept tool calls before they execute.

**Non-goals:**
- Does not replace Claude Code's built-in permission system (works alongside it)
- Does not handle authentication or user identity
- Does not provide per-session permission overrides

## Configuration

Permissions are configured in TOML files. fab evaluates rules in order: project-specific rules first, then global rules. The first matching rule wins.

### File Locations

| Location | Purpose |
|----------|---------|
| `~/.config/fab/permissions.toml` | Global rules (apply to all projects) |
| `~/.fab/projects/<name>/permissions.toml` | Project-specific rules (evaluated first) |

When `FAB_DIR` is set (e.g., for testing), paths become:
- `$FAB_DIR/config/permissions.toml` (global)
- `$FAB_DIR/projects/<name>/permissions.toml` (project)

### Getting Started

Copy the default permissions file to your config directory:

```bash
cp permissions.toml.default ~/.config/fab/permissions.toml
```

## Rule Syntax

Each rule matches a tool and specifies an action. Rules are defined as `[[rules]]` entries:

```toml
[[rules]]
tool = "Bash"
action = "allow"
pattern = "git status:*"
```

### Actions

| Action | Effect |
|--------|--------|
| `allow` | Permit the tool invocation without prompting |
| `deny` | Block the tool invocation |
| `pass` | Skip to the next rule (explicit no-op) |

### Pattern Matching

Patterns match against the primary field of each tool (see [Primary Fields by Tool](#primary-fields-by-tool)).

| Syntax | Behavior |
|--------|----------|
| (empty) or `:*` | Matches everything |
| `prefix:*` | Prefix match (value starts with `prefix`) |
| `exact string` | Exact match only |

Use `patterns` (plural) to match multiple patterns (any match triggers the rule):

```toml
[[rules]]
tool = "Bash"
action = "allow"
patterns = ["git status:*", "git diff:*", "git log:*"]
```

### Path Pattern Prefixes

For file-related tools, special prefixes control path scoping:

| Prefix | Expansion | Example |
|--------|-----------|---------|
| `/path:*` | Worktree-scoped | `/src:*` → `/home/you/project/src:*` |
| `//path:*` | Absolute path | `//tmp:*` → `/tmp:*` |
| `~/path:*` | Home directory | `~/docs:*` → `/home/you/docs:*` |

Examples:

```toml
# Allow writes within the current working directory
[[rules]]
tool = "Write"
action = "allow"
pattern = "/:*"

# Allow writes to /tmp (absolute path)
[[rules]]
tool = "Write"
action = "allow"
pattern = "//tmp/:*"

# Allow reads from home directory
[[rules]]
tool = "Read"
action = "allow"
pattern = "~/:*"
```

### Primary Fields by Tool

Each tool has a primary field used for pattern matching:

| Tool | Primary Field |
|------|---------------|
| `Bash` | `command` |
| `Read`, `Write`, `Edit` | `file_path` |
| `Glob`, `Grep` | `pattern` |
| `WebFetch` | `url` |
| `WebSearch` | `query` |
| `Task` | `prompt` |
| `Skill` | `skill` |
| `NotebookEdit` | `notebook_path` |

### Script Matchers

For complex validation logic, use a script:

```toml
[[rules]]
tool = "Bash"
script = "~/scripts/validate-bash.sh"
```

The script receives:
- First argument: tool name
- Stdin: tool input JSON

Script output determines the action:
- `allow` → permit the invocation
- `deny` → block the invocation
- `pass` (or any other output) → continue to next rule

Scripts timeout after 5 seconds.

## Manager Configuration

The manager agent (runs `fab` commands for orchestration) has its own allowlist:

```toml
[manager]
allowed_patterns = ["fab:*"]
```

This controls which Bash commands the manager can run without prompting. The default is `["fab:*"]` if not specified.

## Claude Code Hooks

fab integrates with Claude Code through hooks. Configure these in your Claude Code `settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "fab hook PreToolUse"
          }
        ]
      }
    ],
    "PermissionRequest": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "fab hook PermissionRequest"
          }
        ]
      }
    ],
    "Stop": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "fab hook Stop"
          }
        ]
      }
    ]
  }
}
```

### Hook Types

| Hook | Purpose |
|------|---------|
| `PreToolUse` | Called before a tool is used. fab evaluates permission rules and returns `allow`, `deny`, or `ask`. |
| `PermissionRequest` | Legacy hook for permission requests. Handled identically to `PreToolUse`. Optional if `PreToolUse` is configured. |
| `Stop` | Called when Claude Code finishes responding. fab notifies the daemon that the agent is idle. |

> **Note:** For new setups, only `PreToolUse` and `Stop` are required. `PermissionRequest` is included for backward compatibility.

### Hook Decision Flow

When a tool is invoked:

1. Claude Code calls `fab hook PreToolUse`
2. fab reads the tool invocation from stdin
3. fab evaluates permission rules (project rules first, then global)
4. If a rule matches:
   - `allow` → respond with `permissionDecision: allow`
   - `deny` → respond with `permissionDecision: deny`
   - `pass` → continue to next rule
5. If no rule matches:
   - Connect to fab daemon
   - Send permission request to TUI for user approval
   - Return user's decision to Claude Code

If the fab daemon is not running when a permission request needs user approval, the request is denied for safety.

## AskUserQuestion Flow

The `AskUserQuestion` tool has special handling. Instead of simple allow/deny, fab:

1. Intercepts the tool call via `PreToolUse`
2. Sends the questions to the fab daemon
3. TUI displays the questions and collects user answers
4. fab returns the answers in `updatedInput` to Claude Code
5. Claude Code proceeds with the populated answers

This allows the TUI to present a proper question interface rather than relying on Claude Code's default terminal prompts.

## Verification

Test your configuration by running a tool that should be allowed/denied:

```bash
# Start the daemon
fab start <project>

# In another terminal, trigger a tool through Claude Code
# Watch the TUI for permission prompts on tools that don't match rules
```

Check hook output in debug mode:

```bash
FAB_LOG_LEVEL=debug fab hook PreToolUse < test-input.json
```

## Examples

### Allow all git read operations

```toml
[[rules]]
tool = "Bash"
action = "allow"
patterns = [
    "git status:*",
    "git diff:*",
    "git log:*",
    "git show:*",
    "git branch:*",
    "git blame:*",
]
```

### Deny dangerous commands

```toml
# Put deny rules before more permissive allow rules
[[rules]]
tool = "Bash"
action = "deny"
patterns = ["rm -rf :*", "sudo :*", "chmod 777:*"]
```

### Allow writes only within worktree

```toml
[[rules]]
tool = "Write"
action = "allow"
pattern = "/:*"

[[rules]]
tool = "Edit"
action = "allow"
pattern = "/:*"
```

### Allow specific documentation sites

```toml
[[rules]]
tool = "WebFetch"
action = "allow"
patterns = [
    "https://docs.rs:*",
    "https://pkg.go.dev:*",
    "https://github.com:*",
]
```

### Project-specific rules

Create `~/.fab/projects/myproject/permissions.toml`:

```toml
# Allow running project-specific build commands
[[rules]]
tool = "Bash"
action = "allow"
patterns = ["make:*", "cargo build:*", "cargo test:*"]
```

## Gotchas

- **Rule order matters**: First matching rule wins. Put specific deny rules before broad allow rules.
- **Daemon required for TUI prompts**: If no rule matches and the daemon isn't running, the request is denied.
- **Pattern escaping**: The `:*` suffix is literal. To match a colon in your pattern, place it before the `:*` suffix (e.g., `http\::*` does not work; use `https://example.com:*`).
- **Home directory expansion**: The `~` prefix only works at the start of patterns. `~user` syntax is not supported.
- **Worktree scoping**: The `/` prefix uses the working directory at hook invocation time, which is typically the worktree root.

## Paths

- `permissions.toml.default` - Default permissions template (copy to config)
- `internal/rules/evaluator.go` - Rule loading and evaluation logic
- `internal/rules/matcher.go` - Pattern matching implementation
- `internal/rules/rules.go` - Rule and config type definitions
- `internal/cli/hook.go` - Claude Code hook handlers
- `internal/paths/paths.go` - Path resolution for config files
