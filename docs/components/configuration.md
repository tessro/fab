# Configuration

## Purpose

The Configuration system manages fab's settings across two scopes: global (daemon-wide) and per-project. It handles API keys for external providers, LLM authorization settings, and project-specific orchestration parameters.

**Non-goals:**
- Does not handle permission rules (that's `permissions.toml`)
- Does not manage runtime state like agent worktrees (that's `internal/project`)
- Does not implement validation logic (that's `internal/config/validate.go`)

## Interface

### CLI Commands

| Command | Description |
|---------|-------------|
| `fab project list` | List all registered projects with their settings |
| `fab project add <url>` | Register a new project from a git URL |
| `fab project remove <name>` | Unregister a project |
| `fab project config show <name>` | Show all configuration for a project |
| `fab project config get <name> <key>` | Get a single configuration value |
| `fab project config set <name> <key> <value>` | Set a configuration value |

### Configuration Scopes

| Scope | File | Description |
|-------|------|-------------|
| Global | `~/.config/fab/config.toml` | API keys, logging, defaults |
| Per-project | `[[projects]]` in global config | Project-specific orchestration settings |

## Configuration

### Global Keys

| Key | Default | Description |
|-----|---------|-------------|
| `log-level` | `"info"` | Logging verbosity: `"debug"`, `"info"`, `"warn"`, `"error"` |
| `providers.<name>.api-key` | — | API key for provider (anthropic, openai, linear, github) |
| `llm-auth.provider` | `"anthropic"` | LLM auth provider: `"anthropic"` or `"openai"` |
| `llm-auth.model` | `"claude-haiku-4-5"` | Model for permission authorization |
| `defaults.agent-backend` | `"claude"` | Default agent CLI: `"claude"` or `"codex"` |
| `defaults.planner-backend` | — | Default planner CLI: `"claude"` or `"codex"` (falls back to agent-backend) |
| `defaults.coding-backend` | — | Default coding agent CLI: `"claude"` or `"codex"` (falls back to agent-backend) |
| `defaults.merge-strategy` | `"direct"` | Default merge: `"direct"` or `"pull-request"` |
| `defaults.issue-backend` | `"tk"` | Default issue backend: `"tk"`, `"github"`, `"gh"`, or `"linear"` |
| `defaults.permissions-checker` | `"manual"` | Default permission checker: `"manual"` or `"llm"` |
| `defaults.autostart` | `false` | Default autostart setting for new projects |
| `defaults.max-agents` | `3` | Default max concurrent agents per project (1-100) |

### Per-Project Keys

Projects are defined using `[[projects]]` array entries:

| Key | Default | Description |
|-----|---------|-------------|
| `name` | (required) | Project identifier |
| `remote-url` | (required) | Git remote URL |
| `max-agents` | `3` | Maximum concurrent agents |
| `autostart` | `false` | Start orchestration on daemon start |
| `issue-backend` | `"tk"` | Issue backend: `"tk"`, `"github"`, `"gh"`, `"linear"` |
| `linear-team` | — | Linear team ID (required for Linear backend) |
| `linear-project` | — | Linear project ID (optional) |
| `allowed-authors` | `[]` | GitHub usernames allowed to create issues |
| `permissions-checker` | `"manual"` | Permission checker: `"manual"` or `"llm"` |
| `agent-backend` | `"claude"` | Agent CLI: `"claude"` or `"codex"` |
| `planner-backend` | `"claude"` | Planner CLI: `"claude"` or `"codex"` |
| `coding-backend` | `"claude"` | Coding agent CLI: `"claude"` or `"codex"` |
| `merge-strategy` | `"direct"` | Merge strategy: `"direct"` or `"pull-request"` |

### Environment Variables

| Variable | Description |
|----------|-------------|
| `FAB_DIR` | Base directory (default: `~/.fab`); derives socket, PID, project paths |
| `FAB_SOCKET_PATH` | Override daemon socket path |
| `FAB_PID_PATH` | Override PID file path |
| `FAB_AGENT_HOST_SOCKET_PATH` | Override agent host socket path |
| `FAB_PROJECT` | Project name for agent commands (set automatically) |

**Precedence:** Specific env vars > `FAB_DIR`-derived paths > defaults.

## Verification

Validate your configuration:

```bash
$ fab project list
NAME       AUTOSTART  MAX-AGENTS
frontend   false      3
backend    true       3
```

View project configuration:

```bash
$ fab project config show myapp
max-agents: 3
autostart: false
issue-backend: tk
```

Run the config validation tests:

```bash
$ go test ./internal/config/... -v
=== RUN   TestValidateRemoteURL
```

## Examples

### Minimal configuration

```toml
[[projects]]
name = "myapp"
remote-url = "git@github.com:user/repo.git"
```

### Multi-project configuration

```toml
log_level = "info"

[providers.anthropic]
api-key = "sk-ant-..."

[[projects]]
name = "frontend"
remote-url = "git@github.com:org/frontend.git"
issue-backend = "github"

[[projects]]
name = "backend"
remote-url = "git@github.com:org/backend.git"
autostart = true
max-agents = 5
```

### Linear integration

```toml
[providers.linear]
api-key = "lin_api_..."

[[projects]]
name = "myapp"
remote-url = "git@github.com:org/myapp.git"
issue-backend = "linear"
linear-team = "TEAM-UUID"
linear-project = "PROJECT-UUID"
allowed-authors = ["user@example.com"]
```

## Gotchas

- **Key naming**: Config keys use hyphens (`remote-url`), not underscores. Legacy underscore format is supported for backwards compatibility but not recommended.
- **Backend fallback**: `planner-backend` and `coding-backend` fall back to `agent-backend` if not set, which falls back to `"claude"`.
- **Linear requires team**: The `linear-team` key is required when using `issue-backend = "linear"`. Without it, issue fetching will fail.
- **FAB_DIR override**: When `FAB_DIR` is set, the config path changes to `$FAB_DIR/config/config.toml`, not the usual `~/.config/fab/config.toml`.
- **Allowed authors**: For GitHub/Linear backends, `allowed-authors` restricts which users' issues are processed. An empty list uses the default (repo owner for GitHub).

## Decisions

**Single config file**: All configuration lives in one `config.toml` file. Global settings are at the top level, and projects are defined in `[[projects]]` arrays. This keeps configuration centralized and easy to version control.

**Hyphen-style keys**: Config keys use hyphens (e.g., `remote-url`) to match CLI flag conventions. Legacy underscore format is auto-migrated during load.

**Environment variable layering**: Environment variables provide testing isolation (`FAB_DIR`) and deployment flexibility (specific path overrides). The layering ensures test isolation while allowing production customization.

**Provider abstraction**: API keys are organized under `[providers.<name>]` rather than flat keys. This allows adding new providers without key collisions.

## Paths

- `internal/config/global.go` - GlobalConfig struct and accessors
- `internal/config/validate.go` - Configuration validation functions
- `internal/paths/paths.go` - Path resolution with env var support
- `internal/registry/registry.go` - Project registry and persistence
- `internal/project/project.go` - Project struct with per-project settings
