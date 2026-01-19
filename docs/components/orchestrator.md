# Orchestrator

## Purpose

The orchestrator manages the agent lifecycle for a project. It detects ready issues from configured backends (GitHub, Linear, or tk), spawns agents with isolated worktrees, tracks ticket claims to prevent duplicate work, and coordinates merge/push operations when agents complete their tasks.

**Non-goals**: The orchestrator does not implement issue tracking itself (delegated to backends), does not directly execute code (delegated to agents), and does not manage cross-project coordination (each project has its own orchestrator).

## Interface

### CLI Commands

| Command | Description |
|---------|-------------|
| `fab agent list` | List all running agents |
| `fab agent claim <ticket-id>` | Claim a ticket (run inside agent worktree) |
| `fab agent done` | Signal task completion and trigger merge |
| `fab agent describe <desc>` | Set agent status description |
| `fab agent abort <id>` | Stop an agent gracefully or forcefully |
| `fab claims` | List active ticket claims |

### Key Types

The orchestrator coordinates several components:

- **Orchestrator**: Main loop that polls for ready issues and spawns agents
- **ClaimRegistry**: In-memory map preventing duplicate ticket claims
- **CommitLog**: Bounded log of successfully merged agent work
- **Agent**: Claude Code subprocess working in an isolated worktree
- **Worktree**: Git worktree at `~/.fab/projects/<project>/worktrees/wt-{agentID}`

## Configuration

Project-level configuration in `~/.config/fab/config.toml`:

```toml
[[projects]]
name = "myapp"
max-agents = 3              # Concurrent agents (default: 3)
issue-backend = "tk"        # tk, github, or linear
merge-strategy = "direct"   # direct or pull-request
coding-backend = "claude"   # Agent CLI backend
```

Internal orchestrator config (set programmatically):

| Option | Default | Description |
|--------|---------|-------------|
| `PollInterval` | 10s | Time between ready issue checks |
| `InterventionSilence` | 60s | Pause automation after user input |
| `KickstartPrompt` | (builtin) | Initial instructions sent to agents |

## Verification

Run the orchestrator unit tests:

```bash
$ go test ./internal/orchestrator/... -v
=== RUN   TestClaimRegistry_Claim
```

Check that the claims registry prevents duplicate claims:

```bash
$ go test ./internal/orchestrator/... -run TestClaimRegistry -v
--- PASS: TestClaimRegistry_Claim
```

## Examples

### Typical Agent Lifecycle

1. **Orchestrator detects ready issue**: Polls issue backend every 10s
2. **Agent spawns**: Creates worktree `wt-{agentID}` and branch `fab/{agentID}`
3. **Agent claims issue**: `fab agent claim 123` registers claim in registry
4. **Agent works**: Implements changes, runs tests, commits
5. **Agent completes**: `fab agent done` triggers rebase and merge to main
6. **Cleanup**: Worktree deleted, claims released, next agent spawns

### Merge Conflict Recovery

When `fab agent done` encounters a merge conflict:

1. Orchestrator aborts the rebase
2. Worktree is rebased to latest `origin/main`
3. Agent stays running to resolve conflicts
4. Agent commits resolution and retries `fab agent done`

### Pull Request Strategy

With `merge-strategy = "pull-request"`:

1. Agent branch pushed to origin: `git push -u origin fab/{agentID}`
2. PR created via `gh pr create`
3. Agent stops but worktree persists for PR feedback
4. Manual merge after review

## Gotchas

- **Claims are in-memory**: Restarting the daemon clears all claims. Agents should re-claim if restarted.
- **Worktree limit**: `max-agents` limits concurrent worktrees. `ErrNoWorktreeAvailable` when exceeded.
- **Intervention pauses automation**: User input pauses the kickstart prompt for `InterventionSilence` duration. Set to 0 to disable.
- **Rebase required**: Agents must rebase onto `origin/main` before merge. Conflicts block completion.

## Decisions

**Worktree isolation**: Each agent gets its own worktree to enable parallel development without interference. The worktree path includes the agent ID for traceability.

**In-memory claims**: Claims are stored in memory rather than persisted because they're transient coordination state. If the daemon restarts, agents can re-claim their issues.

**Merge serialization**: All merges go through a single mutex (`mergeMu`) to prevent race conditions when multiple agents complete simultaneously.

**Fast-forward only**: Direct merge uses `--ff-only` to maintain linear history and fail fast on conflicts.

## Paths

- `internal/orchestrator/orchestrator.go` - Main orchestrator and lifecycle loop
- `internal/orchestrator/claims.go` - Ticket claim registry
- `internal/orchestrator/commits.go` - Commit log tracking
- `internal/agent/agent.go` - Agent state machine
- `internal/project/project.go` - Worktree management
