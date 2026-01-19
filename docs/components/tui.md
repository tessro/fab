# TUI

## Purpose

The TUI (Terminal User Interface) provides an interactive Bubbletea-based interface for monitoring and managing fab agents. It displays real-time agent status, chat history, and recent work, while enabling users to send messages, approve permissions, answer questions, and control agent lifecycle.

**Non-goals:**
- Does not implement daemon communication (delegated to `daemon.TUIClient`)
- Does not manage agent processes directly (delegated to supervisor and orchestrator)
- Does not persist state across sessions (session-only in-memory state)

## Interface

### CLI Command

| Command | Description |
|---------|-------------|
| `fab tui` | Launch the TUI connected to the running daemon |

### Key Bindings

| Mode | Key | Action |
|------|-----|--------|
| Normal | `q`, `Ctrl+C` | Quit TUI |
| Normal | `Tab` | Cycle focus between agent list and chat view |
| Normal | `j`/`k`, `↑`/`↓` | Navigate agent list or scroll chat |
| Normal | `g`/`G` | Jump to top/bottom |
| Normal | `Ctrl+U`/`Ctrl+D` | Page up/down in chat |
| Normal | `Enter` | Enter input mode (send message to agent) |
| Normal | `y` | Approve pending permission or answer |
| Normal | `n` | Reject pending permission |
| Normal | `x` | Abort selected agent (with confirmation) |
| Normal | `p` | Start a new planner agent |
| Normal | `r` | Reconnect when disconnected |
| Input | `Enter` | Send message |
| Input | `Esc` | Cancel input mode |
| Input | `Tab` | Exit input mode |
| Input | `Shift+Enter` | Insert newline |
| Input | `↑`/`↓` | Navigate input history |

### UI Components

| Component | Description |
|-----------|-------------|
| `Header` | Displays branding, agent counts, commit count, usage meter, and connection status |
| `AgentList` | Navigable list of agents with state indicators, project, backend, and duration |
| `ChatView` | Scrollable conversation history with permission/question overlays |
| `InputLine` | Text input with history support for sending messages |
| `RecentWork` | Displays recent commits made by agents |
| `HelpBar` | Context-sensitive keyboard shortcut hints |

### Interaction Modes

The TUI uses a modal state machine to manage user interaction:

| Mode | Description |
|------|-------------|
| `ModeNormal` | Default navigation mode |
| `ModeInput` | User is typing a message |
| `ModeAbortConfirm` | Awaiting abort confirmation (y/n) |
| `ModeUserQuestion` | Selecting answer for Claude's question |
| `ModePlanProjectSelect` | Selecting project for new planner |
| `ModePlanPrompt` | Entering prompt for new planner |

## Configuration

The TUI reads global configuration from `~/.config/fab/config.toml`:

| Key | Description |
|-----|-------------|
| `log-level` | Controls TUI debug logging (logs to file, not terminal) |

Runtime options (passed programmatically):

| Option | Description |
|--------|-------------|
| `InitialAgentID` | Agent to select on startup (empty = first agent) |

## Verification

Run the TUI unit tests:

```bash
$ go test ./internal/tui/... -v
=== RUN   TestNewModeState
```

Run the mode state machine tests specifically:

```bash
$ go test ./internal/tui/... -run TestModeState -v
--- PASS: TestModeState_SetFocus
```

## Examples

### Monitoring agent activity

1. Start the TUI: `fab tui`
2. Use `j`/`k` to navigate the agent list
3. View chat history for the selected agent
4. Press `Enter` to send a message to the agent

### Approving a tool permission

When an agent requests permission to use a tool:

1. The agent row shows `!` indicator (attention needed)
2. The chat view displays the pending permission request
3. Press `y` to approve or `n` to reject

### Answering a user question

When Claude uses AskUserQuestion:

1. The chat view shows the question with selectable options
2. Use `j`/`k` to navigate options
3. Press `y` to submit the selected answer
4. Select "Other" and press `y` to enter a custom response

### Starting a planner

1. Press `p` in normal mode
2. Select a project using `j`/`k` and press `Enter`
3. Type your planning prompt
4. Press `Enter` to start the planner

## Gotchas

- **Connection loss**: The TUI auto-reconnects with exponential backoff (up to 10 attempts). Press `r` for manual reconnection when disconnected.
- **Permission timeout**: Permissions must be approved within 5 minutes (handled by supervisor). Unanswered permissions cause agent failure.
- **Chat history on reconnect**: After daemon restart, chat history may be lost. The TUI refetches history on reconnection.
- **Input mode isolation**: In input mode, navigation keys are captured by the text input. Press `Esc` or `Tab` to exit.
- **Spinner animation**: Running agents show animated spinners. Manager agents show a static indicator when idle.

## Decisions

**Bubbletea architecture**: The TUI uses the Elm-like Bubbletea framework with a central `Model` and message-based updates. This provides predictable state management and clean separation between UI and logic.

**Modal state machine**: A centralized `ModeState` manages all interaction modes. This prevents invalid state combinations (e.g., input mode during abort confirmation) and simplifies focus management.

**Event streaming**: The TUI maintains a dedicated streaming connection to the daemon for real-time updates. Events are processed asynchronously via a message channel to avoid blocking the UI.

**Entry merging on history fetch**: When fetching chat history, the TUI merges with any streaming entries that arrived during the fetch. This prevents race conditions where switching agents loses recent messages.

**Automatic reconnection**: Exponential backoff (500ms to 8s) handles transient connection issues without user intervention. The header displays connection state for visibility.

## Paths

- `internal/tui/tui.go` - Main model, initialization, and view composition
- `internal/tui/update.go` - Message handling and state updates
- `internal/tui/mode.go` - Modal state machine
- `internal/tui/keybindings.go` - Keyboard shortcut definitions
- `internal/tui/commands.go` - Bubbletea commands for daemon communication
- `internal/tui/messages.go` - Internal message types
- `internal/tui/helpers.go` - Model helper methods (focus sync, layout, state pruning)
- `internal/tui/agentlist.go` - Agent list component
- `internal/tui/chatview.go` - Chat view component with permission/question overlays
- `internal/tui/header.go` - Header component with status indicators
- `internal/tui/inputline.go` - Text input with history
- `internal/tui/helpbar.go` - Context-sensitive help bar
- `internal/tui/recentwork.go` - Recent commits display
- `internal/tui/styles.go` - Lipgloss styling definitions
- `internal/cli/tui.go` - CLI command that launches the TUI
