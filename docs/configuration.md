# Configuration Reference

Configuration is stored in `~/.config/fab/config.toml`. Use `fab project config` to manage per-project settings.

## Global Keys

| Key | Default | Description |
|-----|---------|-------------|
| `log_level` | `"info"` | Logging verbosity: `"debug"`, `"info"`, `"warn"`, `"error"` |
| `providers.<name>.api-key` | — | API key for provider (anthropic, openai, linear, github) |
| `llm_auth.provider` | `"anthropic"` | LLM auth provider: `"anthropic"` or `"openai"` |
| `llm_auth.model` | `"claude-haiku-4-5"` | Model for permission authorization |
| `defaults.agent-backend` | `"claude"` | Default agent CLI: `"claude"` or `"codex"` |
| `defaults.merge-strategy` | `"direct"` | Default merge: `"direct"` or `"pull-request"` |
| `webhook.enabled` | `false` | Enable webhook server |
| `webhook.bind-addr` | `":8080"` | Webhook server bind address |
| `webhook.secret` | — | Webhook signature secret |
| `webhook.path-prefix` | `"/webhooks"` | Webhook URL path prefix |

## Per-Project Configuration

Projects are defined using `[[projects]]` array entries.

```toml
[[projects]]
name = "my-project"
remote-url = "git@github.com:user/repo.git"
max-agents = 2
autostart = true
issue-backend = "github"
```

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

## Environment Variables

| Variable | Description |
|----------|-------------|
| `FAB_DIR` | Base directory (default: `~/.fab`); derives socket, PID, project paths |
| `FAB_SOCKET_PATH` | Override daemon socket path |
| `FAB_PID_PATH` | Override PID file path |
| `FAB_AGENT_HOST_SOCKET_PATH` | Override agent host socket path |
| `FAB_PROJECT` | Project name for agent commands (set automatically) |

**Precedence:** Specific env vars > `FAB_DIR`-derived paths > defaults.

## Examples

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
```

## Verification

Validate your configuration:

```bash
$ fab project list
NAME       AUTOSTART  MAX-AGENTS
frontend   false      3
backend    true       3
```
