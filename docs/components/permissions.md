# Permissions

## Purpose

The permissions system controls which Claude Code tool invocations are automatically allowed, denied, or require user approval through the TUI. It integrates with Claude Code's hook mechanism to intercept tool calls before they execute.

**Non-goals:**
- Does not replace Claude Code's built-in permission system (works alongside it)
- Does not handle authentication or user identity
- Does not provide per-session permission overrides

## Interface

### Hook Commands

| Command | Description |
|---------|-------------|
| `fab hook PreToolUse` | Evaluates permission rules before tool execution |
| `fab hook PermissionRequest` | Legacy hook (identical to PreToolUse) |
| `fab hook Stop` | Notifies daemon that agent is idle |

### Hook Decision Flow

When a tool is invoked:

1. Claude Code calls `fab hook PreToolUse`
2. fab reads the tool invocation from stdin
3. fab evaluates permission rules (project rules first, then global)
4. If a rule matches:
   - `allow` → respond with `permissionDecision: allow`
   - `deny` → respond with `permissionDecision: deny`
   - `pass` → continue to next rule
5. If no rule matches → send permission request to TUI for user approval

If the fab daemon is not running when a permission request needs user approval, the request is denied for safety.

### AskUserQuestion Handling

The `AskUserQuestion` tool has special handling:

1. fab intercepts the tool call via `PreToolUse`
2. Sends the questions to the fab daemon
3. TUI displays questions and collects user answers
4. fab returns answers in `updatedInput` to Claude Code

## Configuration

Permissions are configured in TOML files. fab evaluates rules in order: project-specific rules first, then global rules. The first matching rule wins.

### File Locations

| Location | Purpose |
|----------|---------|
| `~/.config/fab/permissions.toml` | Global rules (apply to all projects) |
| `~/.fab/projects/<name>/permissions.toml` | Project-specific rules (evaluated first) |

When `FAB_DIR` is set, paths become `$FAB_DIR/config/permissions.toml` and `$FAB_DIR/projects/<name>/permissions.toml`.

### Getting Started

```bash
cp permissions.toml.default ~/.config/fab/permissions.toml
```

### Rule Syntax

Each rule matches a tool and specifies an action:

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

Patterns match against the primary field of each tool.

| Syntax | Behavior |
|--------|----------|
| (empty) or `:*` | Matches everything |
| `prefix:*` | Prefix match (value starts with `prefix`) |
| `exact string` | Exact match only |

Use `patterns` (plural) to match multiple patterns:

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

### Primary Fields by Tool

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

The script receives the tool name as the first argument and tool input JSON on stdin. Output determines the action: `allow`, `deny`, or `pass` (continue to next rule). Scripts timeout after 5 seconds.

### Manager Configuration

The manager agent has its own allowlist for Bash commands:

```toml
[manager]
allowed_patterns = ["fab:*"]
```

### Claude Code Settings

Configure hooks in your Claude Code `settings.json`:

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [{"type": "command", "command": "fab hook PreToolUse"}]
      }
    ],
    "Stop": [
      {
        "matcher": "*",
        "hooks": [{"type": "command", "command": "fab hook Stop"}]
      }
    ]
  }
}
```

## Verification

Test your configuration:

```bash
# Start the daemon
fab start <project>

# Check hook output in debug mode
FAB_LOG_LEVEL=debug fab hook PreToolUse < test-input.json
```

## Examples

### Allow git read operations

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
[[rules]]
tool = "Bash"
action = "allow"
patterns = ["make:*", "cargo build:*", "cargo test:*"]
```

## Gotchas

- **Rule order matters**: First matching rule wins. Put specific deny rules before broad allow rules.
- **Daemon required for TUI prompts**: If no rule matches and the daemon isn't running, the request is denied.
- **Pattern escaping**: The `:*` suffix is literal. To match a colon in your pattern, place it before the `:*` suffix.
- **Home directory expansion**: The `~` prefix only works at the start of patterns. `~user` syntax is not supported.
- **Worktree scoping**: The `/` prefix uses the working directory at hook invocation time, which is typically the worktree root.

## Decisions

**Hook integration over wrapper**: fab uses Claude Code's hook mechanism rather than wrapping the Claude Code process. This allows fab to intercept tool calls while letting Claude Code manage its own lifecycle.

**Project rules first**: Project-specific rules are evaluated before global rules, allowing projects to override global defaults without modifying the global config.

**Deny on daemon unavailable**: When the daemon isn't running and a permission request needs user approval, the request is denied rather than allowed. This fail-safe behavior prevents unintended tool execution.

## Paths

- `permissions.toml.default` - Default permissions template
- `internal/rules/evaluator.go` - Rule loading and evaluation logic
- `internal/rules/matcher.go` - Pattern matching implementation
- `internal/rules/rules.go` - Rule and config type definitions
- `internal/cli/hook.go` - Claude Code hook handlers
- `internal/paths/paths.go` - Path resolution for config files
