# Issue Backends

## Purpose

The Issue Backend system provides a pluggable abstraction for issue tracking, allowing fab to work with multiple issue tracking systems (tk, GitHub Issues, Linear) through a unified interface.

**Non-goals:**
- This system does not implement issue UI rendering (that's the CLI/TUI layer)
- It does not handle authentication directly (backends expect tokens/keys to be provided)

## Interface

The system defines several interfaces in `internal/issue/backend.go`:

| Interface | Description |
|-----------|-------------|
| `IssueReader` | Read-only access: `Get`, `List`, `Ready` |
| `IssueWriter` | Write access: `Create`, `CreateSubIssue`, `Update`, `Close`, `Commit` |
| `Backend` | Combines `IssueReader` and `IssueWriter` |
| `IssueCollaborator` | Collaboration features: `AddComment`, `UpsertPlanSection`, `CreateSubIssue` |
| `CollaborativeBackend` | Combines `Backend` and `IssueCollaborator` |

### Core Types

```go
type Issue struct {
    ID           string
    Title        string
    Description  string
    Status       Status      // open, closed, blocked
    Priority     int         // 0=low, 1=medium, 2=high
    Type         string      // task, bug, feature, chore
    Dependencies []string    // IDs of blocking issues
    Labels       []string
}
```

## Configuration

Issue backends are configured per-project in `~/.config/fab/config.toml`:

### Project Configuration Keys

| Key | Type | Description |
|-----|------|-------------|
| `issue-backend` | string | Backend type: `tk`, `github`, `gh`, or `linear` |
| `allowed-authors` | []string | Usernames allowed to create issues (GitHub or Linear) |
| `linear-team` | string | Linear team ID (required for Linear backend) |
| `linear-project` | string | Linear project ID (optional, scopes issues) |

### Provider API Keys

API keys are configured globally under `[providers.<name>]`:

| Provider | Config Key | Environment Variable |
|----------|------------|---------------------|
| GitHub | `[providers.github]` with `api-key` | `GITHUB_TOKEN` or `GH_TOKEN` |
| Linear | `[providers.linear]` with `api-key` | `LINEAR_API_KEY` |

## Paths

- `internal/issue/backend.go` - Interface definitions
- `internal/issue/issue.go` - Core types (`Issue`, `CreateParams`, `UpdateParams`)
- `internal/issue/resolver.go` - Project resolution from cwd/env
- `internal/issue/plan.go` - Plan section upsert helper
- `internal/issue/tk/` - File-based tk backend (stores issues in `.tickets/`)
- `internal/issue/gh/` - GitHub Issues backend (GraphQL API)
- `internal/issue/linear/` - Linear backend (GraphQL API)

## Verification

Run the unit tests for the issue package:

```bash
$ go test ./internal/issue/... -v -count=1 2>&1 | head -30
=== RUN
```

Run the tk backend parser tests:

```bash
$ go test ./internal/issue/tk -run TestParse -v
=== RUN   TestParseIssue
```

## Examples

### tk Backend Configuration

The tk backend stores issues as markdown files in `.tickets/`:

```toml
[[projects]]
name = "myproject"
remote-url = "git@github.com:user/repo.git"
issue-backend = "tk"
```

Issues are stored as `.tickets/<id>.md` with YAML frontmatter:

```markdown
---
id: fa-123
title: Fix the bug
status: open
priority: 1
type: bug
deps: [fa-100]
---

Description of the issue here.
```

### GitHub Backend Configuration

```toml
# Global provider config
[providers.github]
api-key = "ghp_xxxxx"  # or use GITHUB_TOKEN env

# Project config
[[projects]]
name = "myproject"
remote-url = "git@github.com:user/repo.git"
issue-backend = "github"  # or "gh"
allowed-authors = ["owner", "contributor"]
```

### Linear Backend Configuration

```toml
# Global provider config
[providers.linear]
api-key = "lin_api_xxxxx"  # or use LINEAR_API_KEY env

# Project config
[[projects]]
name = "myproject"
remote-url = "git@github.com:user/repo.git"
issue-backend = "linear"
linear-team = "TEAM-UUID"
linear-project = "PROJECT-UUID"  # optional
allowed-authors = ["user@example.com"]  # optional
```

## Gotchas

- **tk backend**: Changes require `Commit()` to persist (git add/commit/push)
- **GitHub/Linear backends**: Changes are immediate via API, `Commit()` is a no-op
- **Priority mapping**: fab uses 0=low, 1=medium, 2=high; Linear uses inverted scale (1=urgent, 4=low)
- **Dependencies**: tk uses explicit `deps` field; GitHub uses `blockedBy` API; Linear uses parent-child
- **Collaboration features**: `fab issue comment` and `fab issue plan` require a backend that implements `IssueCollaborator`. Backends that don't support these features return `ErrNotSupported`.
- **ErrNotSupported**: Backends may return this for unsupported operations

## Decisions

**Interface split**: The read/write/collaborator split allows backends to implement only what they support. A backend can satisfy `Backend` without implementing `IssueCollaborator` if it doesn't support comments or plans.

**tk as default**: The tk backend is the primary backend, storing issues in-repo as markdown files. This enables offline work and keeps issues versioned with code.

**GraphQL for GitHub/Linear**: Both external backends use GraphQL APIs for richer query capabilities and to support features like blockedBy relationships and sub-issues.

**Priority normalization**: Each backend maps its native priority system to fab's 0-2 scale, ensuring consistent behavior across backends while preserving backend-specific semantics.
