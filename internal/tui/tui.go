// Package tui provides the Bubbletea-based terminal user interface for fab.
package tui

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/usage"
)

// Focus indicates which panel is currently focused.
type Focus int

const (
	FocusAgentList Focus = iota
	FocusChatView
	FocusInputLine
	FocusActionQueue
)

// StreamEventMsg wraps a daemon stream event for Bubble Tea.
type StreamEventMsg struct {
	Event *daemon.StreamEvent
	Err   error
}

// AgentListMsg contains updated agent list from daemon.
type AgentListMsg struct {
	Agents []daemon.AgentStatus
	Err    error
}

// AgentInputMsg is the result of sending input to an agent.
type AgentInputMsg struct {
	Err error
}

// AgentChatHistoryMsg contains chat history fetched for an agent.
type AgentChatHistoryMsg struct {
	AgentID string
	Entries []daemon.ChatEntryDTO
	Err     error
}

// StagedActionsMsg contains pending actions that need user approval.
type StagedActionsMsg struct {
	Actions []daemon.StagedAction
	Err     error
}

// StatsMsg contains aggregated session statistics.
type StatsMsg struct {
	Stats *daemon.StatsResponse
	Err   error
}

// ActionResultMsg is the result of approving/rejecting an action.
type ActionResultMsg struct {
	Err error
}

// PermissionResultMsg is the result of responding to a permission request.
type PermissionResultMsg struct {
	Err error
}

// AbortResultMsg is the result of aborting an agent.
type AbortResultMsg struct {
	Err error
}

// tickMsg is sent on regular intervals to drive spinner animation.
type tickMsg time.Time

// clearErrorMsg is sent to clear the error display after a timeout.
type clearErrorMsg struct{}

// ConnectionState represents the current IPC connection status.
type ConnectionState int

const (
	// ConnectionConnected means the TUI is connected to the daemon.
	ConnectionConnected ConnectionState = iota
	// ConnectionDisconnected means the connection was lost.
	ConnectionDisconnected
	// ConnectionReconnecting means a reconnection attempt is in progress.
	ConnectionReconnecting
)

// reconnectMsg signals the result of a reconnection attempt.
type reconnectMsg struct {
	Success   bool
	Err       error
	EventChan <-chan daemon.EventResult
}

// UsageUpdateMsg contains updated usage statistics.
type UsageUpdateMsg struct {
	Percent   int
	Remaining time.Duration
	Err       error
}

// Model is the main Bubbletea model for the fab TUI.
type Model struct {
	// Window dimensions
	width  int
	height int

	// UI state
	ready bool
	err   error

	// Mode state (centralized focus and mode management)
	modeState ModeState

	// Components
	header      Header
	agentList   AgentList
	chatView    ChatView
	inputLine   InputLine
	helpBar     HelpBar
	actionQueue ActionQueue

	// Daemon client for IPC
	client   *daemon.Client
	attached bool

	// Event channel from dedicated streaming connection
	eventChan <-chan daemon.EventResult

	// Connection state tracking
	connState      ConnectionState
	reconnectDelay time.Duration
	reconnectCount int
	maxReconnects  int

	// Staged actions pending approval (for selected agent)
	stagedActions []daemon.StagedAction

	// Pending permission requests (for selected agent)
	pendingPermissions []daemon.PermissionRequest

	// Spinner animation frame counter
	spinnerFrame int

	// Stats refresh counter (every 300 ticks = 30s at 100ms/tick)
	statsRefreshTick int

	// Key bindings
	keys KeyBindings

	// Usage tracking
	lastUsageFetch time.Time
	usageLimits    usage.Limits
}

// New creates a new TUI model.
func New() Model {
	return Model{
		header:         NewHeader(),
		agentList:      NewAgentList(),
		chatView:       NewChatView(),
		inputLine:      NewInputLine(),
		helpBar:        NewHelpBar(),
		actionQueue:    NewActionQueue(),
		modeState:      NewModeState(),
		keys:           DefaultKeyBindings(),
		connState:      ConnectionConnected,
		reconnectDelay: 500 * time.Millisecond,
		maxReconnects:  10,
		usageLimits:    usage.DefaultProLimits(),
	}
}

// NewWithClient creates a new TUI model with a pre-connected daemon client.
func NewWithClient(client *daemon.Client) Model {
	m := New()
	m.client = client
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.inputLine.input.Cursor.BlinkCmd(),
		m.tickCmd(), // Start spinner animation
		m.fetchUsage(),
	}
	if m.client != nil {
		// Fetch agent list first, then attach to stream
		// (must be sequential to avoid concurrent decoder access)
		cmds = append(cmds, m.fetchAgentList())
	}
	return tea.Batch(cmds...)
}

// tickCmd returns a command that sends a tick message after a delay.
func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// clearErrorCmd returns a command that clears the error after a delay.
func clearErrorCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearErrorMsg{}
	})
}

// setError sets an error to display and returns a command to clear it after a timeout.
func (m *Model) setError(err error) tea.Cmd {
	m.err = err
	m.helpBar.SetError(err.Error())
	return clearErrorCmd()
}

// StreamStartMsg is sent when the event stream is started successfully.
type StreamStartMsg struct {
	EventChan <-chan daemon.EventResult
}

// attachToStream connects to the daemon event stream using a dedicated connection.
func (m Model) attachToStream() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		eventChan, err := m.client.StreamEvents(nil)
		if err != nil {
			return StreamEventMsg{Err: err}
		}
		return StreamStartMsg{EventChan: eventChan}
	}
}

// attemptReconnect tries to reconnect to the daemon after a delay.
func (m Model) attemptReconnect() tea.Cmd {
	delay := m.reconnectDelay
	return func() tea.Msg {
		// Wait before attempting reconnection
		time.Sleep(delay)

		// Try to reconnect the main connection first
		if !m.client.IsConnected() {
			if err := m.client.Connect(); err != nil {
				return reconnectMsg{Success: false, Err: err}
			}
		}

		// Try to establish the event stream
		eventChan, err := m.client.StreamEvents(nil)
		if err != nil {
			return reconnectMsg{Success: false, Err: err}
		}

		return reconnectMsg{Success: true, EventChan: eventChan}
	}
}

// waitForEvent waits for the next event from the channel.
func (m Model) waitForEvent() tea.Cmd {
	if m.eventChan == nil {
		return nil
	}
	ch := m.eventChan
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			// Channel closed
			return StreamEventMsg{Err: fmt.Errorf("event stream closed")}
		}
		return StreamEventMsg{Event: result.Event, Err: result.Err}
	}
}

// fetchAgentList retrieves the current agent list.
func (m Model) fetchAgentList() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		resp, err := m.client.AgentList("")
		if err != nil {
			return AgentListMsg{Err: err}
		}
		return AgentListMsg{Agents: resp.Agents}
	}
}

// sendAgentMessage sends a user message to an agent via stream-json.
func (m Model) sendAgentMessage(agentID, content string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.AgentSendMessage(agentID, content)
		return AgentInputMsg{Err: err}
	}
}

// fetchAgentChatHistory retrieves chat history for an agent.
func (m Model) fetchAgentChatHistory(agentID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return AgentChatHistoryMsg{AgentID: agentID, Entries: nil}
		}
		resp, err := m.client.AgentChatHistory(agentID, 0) // 0 = all entries
		if err != nil {
			return AgentChatHistoryMsg{AgentID: agentID, Err: err}
		}
		return AgentChatHistoryMsg{AgentID: agentID, Entries: resp.Entries}
	}
}

// fetchStagedActions retrieves pending actions for user approval.
func (m Model) fetchStagedActions() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		resp, err := m.client.ListStagedActions("")
		if err != nil {
			return StagedActionsMsg{Err: err}
		}
		return StagedActionsMsg{Actions: resp.Actions}
	}
}

// fetchStats retrieves aggregated session statistics.
func (m Model) fetchStats() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		resp, err := m.client.Stats("")
		if err != nil {
			return StatsMsg{Err: err}
		}
		return StatsMsg{Stats: resp}
	}
}

// approveAction approves a staged action.
func (m Model) approveAction(actionID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.ApproveAction(actionID)
		return ActionResultMsg{Err: err}
	}
}

// rejectAction rejects a staged action.
func (m Model) rejectAction(actionID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.RejectAction(actionID, "")
		return ActionResultMsg{Err: err}
	}
}

// allowPermission approves a permission request.
func (m Model) allowPermission(requestID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.RespondPermission(requestID, "allow", "", false)
		return PermissionResultMsg{Err: err}
	}
}

// denyPermission denies a permission request.
func (m Model) denyPermission(requestID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.RespondPermission(requestID, "deny", "denied by user", false)
		return PermissionResultMsg{Err: err}
	}
}

// abortAgent aborts a running agent.
func (m Model) abortAgent(agentID string, force bool) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.AgentAbort(agentID, force)
		return AbortResultMsg{Err: err}
	}
}

// fetchUsage retrieves current usage statistics.
func (m Model) fetchUsage() tea.Cmd {
	limits := m.usageLimits
	return func() tea.Msg {
		window, err := usage.GetCurrentBillingWindowWithUsage()
		if err != nil {
			return UsageUpdateMsg{Err: err}
		}
		return UsageUpdateMsg{
			Percent:   window.Usage.PercentInt(limits),
			Remaining: window.TimeRemaining(),
		}
	}
}

// pendingPermissionForAgent returns the first pending permission request for the given agent.
func (m *Model) pendingPermissionForAgent(agentID string) *daemon.PermissionRequest {
	if agentID == "" {
		return nil
	}
	for i := range m.pendingPermissions {
		if m.pendingPermissions[i].AgentID == agentID {
			return &m.pendingPermissions[i]
		}
	}
	return nil
}

// updateNeedsAttention rebuilds the map of agents that need user attention.
func (m *Model) updateNeedsAttention() {
	attention := make(map[string]bool)
	for _, perm := range m.pendingPermissions {
		attention[perm.AgentID] = true
	}
	for _, action := range m.stagedActions {
		attention[action.AgentID] = true
	}
	m.agentList.SetNeedsAttention(attention)
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle input mode
		if m.modeState.IsInputting() {
			switch {
			case key.Matches(msg, m.keys.Cancel):
				// Exit input mode, return to agent list
				_ = m.modeState.ExitInputMode()
				m.inputLine.SetFocused(false)
				m.chatView.SetInputView(m.inputLine.View(), 1)
			case key.Matches(msg, m.keys.Submit):
				// Submit input to agent
				if m.client != nil && m.chatView.AgentID() != "" {
					input := m.inputLine.Value()
					if input != "" {
						// Show user message immediately in chat
						m.chatView.AppendEntry(daemon.ChatEntryDTO{
							Role:      "user",
							Content:   input,
							Timestamp: time.Now().Format(time.RFC3339),
						})
						// Send to agent
						cmds = append(cmds, m.sendAgentMessage(m.chatView.AgentID(), input))
						m.inputLine.Clear()
						// Exit input mode, return to agent list
						_ = m.modeState.ExitInputMode()
						m.inputLine.SetFocused(false)
						m.chatView.SetInputView(m.inputLine.View(), 1)
					}
				}
			case key.Matches(msg, m.keys.Tab):
				// Exit input mode, return to agent list
				_ = m.modeState.ExitInputMode()
				m.inputLine.SetFocused(false)
				m.chatView.SetInputView(m.inputLine.View(), 1)
			default:
				// Pass all other keys to input
				cmd := m.inputLine.Update(msg)
				cmds = append(cmds, cmd)
				m.chatView.SetInputView(m.inputLine.View(), 1)
			}
			return m, tea.Batch(cmds...)
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			// Close client to unblock any pending RecvEvent() calls
			if m.client != nil {
				m.client.Close()
			}
			return m, tea.Quit

		case key.Matches(msg, m.keys.Tab):
			// Cycle focus: agent list -> chat view -> input line -> agent list
			newFocus, _ := m.modeState.CycleFocus()
			m.syncFocusToComponents(newFocus)

		case key.Matches(msg, m.keys.FocusChat):
			// Focus input line (vim-style) - enters input mode
			if m.chatView.AgentID() != "" && m.modeState.IsNormal() {
				_ = m.modeState.EnterInputMode()
				m.chatView.SetFocused(false)
				m.inputLine.SetFocused(true)
				m.chatView.SetInputView(m.inputLine.View(), 1)
			}

		case key.Matches(msg, m.keys.FocusActions):
			// Focus action queue
			if m.modeState.IsNormal() {
				_ = m.modeState.SetFocus(FocusActionQueue)
				m.syncFocusToComponents(FocusActionQueue)
			}

		case key.Matches(msg, m.keys.Approve):
			// Handle abort confirmation
			if m.modeState.IsAbortConfirming() {
				agentID, _ := m.modeState.ConfirmAbort()
				slog.Debug("confirming abort", "agent_id", agentID)
				cmds = append(cmds, m.abortAgent(agentID, false))
				m.chatView.SetAbortConfirming(false, "")
			} else if m.modeState.Focus == FocusActionQueue {
				// Approve action selected in action queue
				if action := m.actionQueue.Selected(); action != nil {
					slog.Debug("approving action from queue",
						"action_id", action.ID,
						"action_agent", action.AgentID,
					)
					cmds = append(cmds, m.approveAction(action.ID))
				}
			} else {
				// Approve pending permission or action for selected agent
				agentID := m.chatView.AgentID()
				// Permissions take priority over actions
				if perm := m.pendingPermissionForAgent(agentID); perm != nil {
					slog.Debug("approving permission",
						"permission_id", perm.ID,
						"tool", perm.ToolName,
					)
					cmds = append(cmds, m.allowPermission(perm.ID))
				} else if action := m.pendingActionForAgent(agentID); action != nil {
					slog.Debug("approving action",
						"action_id", action.ID,
						"action_agent", action.AgentID,
					)
					cmds = append(cmds, m.approveAction(action.ID))
				}
			}

		case key.Matches(msg, m.keys.Reject):
			// Handle abort cancellation
			if m.modeState.IsAbortConfirming() {
				_ = m.modeState.CancelAbort()
				m.chatView.SetAbortConfirming(false, "")
			} else if m.modeState.Focus == FocusActionQueue {
				// Reject action selected in action queue
				if action := m.actionQueue.Selected(); action != nil {
					slog.Debug("rejecting action from queue",
						"action_id", action.ID,
						"action_agent", action.AgentID,
					)
					cmds = append(cmds, m.rejectAction(action.ID))
				}
			} else {
				// Reject pending permission or action for selected agent
				agentID := m.chatView.AgentID()
				// Permissions take priority over actions
				if perm := m.pendingPermissionForAgent(agentID); perm != nil {
					slog.Debug("denying permission",
						"permission_id", perm.ID,
						"tool", perm.ToolName,
					)
					cmds = append(cmds, m.denyPermission(perm.ID))
				} else if action := m.pendingActionForAgent(agentID); action != nil {
					cmds = append(cmds, m.rejectAction(action.ID))
				}
			}

		case key.Matches(msg, m.keys.Abort):
			// Start abort confirmation for selected agent
			agentID := m.chatView.AgentID()
			if agentID != "" && m.modeState.IsNormal() {
				if err := m.modeState.EnterAbortConfirm(agentID); err == nil {
					m.chatView.SetAbortConfirming(true, agentID)
				}
			}

		case key.Matches(msg, m.keys.Reconnect):
			// Manual reconnection when disconnected
			if m.connState == ConnectionDisconnected && m.client != nil {
				slog.Debug("manual reconnection triggered")
				m.connState = ConnectionReconnecting
				m.reconnectCount = 0
				m.reconnectDelay = 500 * time.Millisecond
				m.header.SetConnectionState(m.connState)
				cmds = append(cmds, m.attemptReconnect())
			}

		case key.Matches(msg, m.keys.Down):
			switch m.modeState.Focus {
			case FocusAgentList:
				m.agentList.MoveDown()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				m.chatView.ScrollDown(1)
			case FocusActionQueue:
				m.actionQueue.MoveDown()
			}

		case key.Matches(msg, m.keys.Up):
			switch m.modeState.Focus {
			case FocusAgentList:
				m.agentList.MoveUp()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				m.chatView.ScrollUp(1)
			case FocusActionQueue:
				m.actionQueue.MoveUp()
			}

		case key.Matches(msg, m.keys.Top):
			switch m.modeState.Focus {
			case FocusAgentList:
				m.agentList.MoveToTop()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				m.chatView.ScrollToTop()
			case FocusActionQueue:
				m.actionQueue.MoveToTop()
			}

		case key.Matches(msg, m.keys.Bottom):
			switch m.modeState.Focus {
			case FocusAgentList:
				m.agentList.MoveToBottom()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				m.chatView.ScrollToBottom()
			case FocusActionQueue:
				m.actionQueue.MoveToBottom()
			}

		case key.Matches(msg, m.keys.PageUp):
			if m.modeState.Focus == FocusChatView {
				m.chatView.PageUp()
			}

		case key.Matches(msg, m.keys.PageDown):
			if m.modeState.Focus == FocusChatView {
				m.chatView.PageDown()
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.header.SetWidth(m.width)
		m.updateLayout()
		m.ready = true

	case StreamStartMsg:
		// Event stream connected successfully
		m.eventChan = msg.EventChan
		m.attached = true
		m.connState = ConnectionConnected
		m.reconnectCount = 0
		m.reconnectDelay = 500 * time.Millisecond
		m.header.SetConnectionState(m.connState)
		cmds = append(cmds, m.waitForEvent())

	case StreamEventMsg:
		if msg.Err != nil {
			slog.Debug("stream error, attempting reconnection", "err", msg.Err)
			m.connState = ConnectionDisconnected
			m.header.SetConnectionState(m.connState)
			m.eventChan = nil
			// Attempt automatic reconnection if under limit
			if m.reconnectCount < m.maxReconnects {
				m.connState = ConnectionReconnecting
				m.header.SetConnectionState(m.connState)
				cmds = append(cmds, m.attemptReconnect())
			} else {
				cmds = append(cmds, m.setError(fmt.Errorf("connection lost (press 'r' to reconnect)")))
			}
		} else if msg.Event != nil {
			slog.Debug("stream event received", "type", msg.Event.Type)
			if cmd := m.handleStreamEvent(msg.Event); cmd != nil {
				cmds = append(cmds, cmd)
			}
			// Continue listening for events
			cmds = append(cmds, m.waitForEvent())
		}

	case reconnectMsg:
		if msg.Success {
			slog.Debug("reconnection successful")
			m.eventChan = msg.EventChan
			m.attached = true
			m.connState = ConnectionConnected
			m.reconnectCount = 0
			m.reconnectDelay = 500 * time.Millisecond
			m.header.SetConnectionState(m.connState)
			// Fetch fresh agent list after reconnection
			cmds = append(cmds, m.fetchAgentList())
			cmds = append(cmds, m.waitForEvent())
		} else {
			slog.Debug("reconnection failed", "err", msg.Err, "attempt", m.reconnectCount+1)
			m.reconnectCount++
			// Exponential backoff: 500ms, 1s, 2s, 4s, 8s (capped)
			m.reconnectDelay = min(m.reconnectDelay*2, 8*time.Second)
			if m.reconnectCount < m.maxReconnects {
				cmds = append(cmds, m.attemptReconnect())
			} else {
				m.connState = ConnectionDisconnected
				m.header.SetConnectionState(m.connState)
				cmds = append(cmds, m.setError(fmt.Errorf("connection lost after %d attempts (press 'r' to reconnect)", m.reconnectCount)))
			}
		}

	case AgentListMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		} else {
			m.agentList.SetAgents(msg.Agents)
			m.header.SetAgentCounts(len(msg.Agents), countRunning(msg.Agents))
			// Auto-select first agent if none is currently selected
			if m.chatView.AgentID() == "" && len(msg.Agents) > 0 {
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			// Attach to event stream after initial agent list fetch
			if !m.attached {
				cmds = append(cmds, m.attachToStream())
			}
			// Fetch staged actions for approval display
			cmds = append(cmds, m.fetchStagedActions())
			// Fetch stats for header display
			cmds = append(cmds, m.fetchStats())
		}

	case AgentInputMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		}

	case AgentChatHistoryMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		} else if msg.AgentID == m.chatView.AgentID() {
			// Only apply if still viewing this agent
			m.chatView.SetEntries(msg.Entries)
		}

	case StagedActionsMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		} else {
			// Store all staged actions - filtering happens in pendingActionForAgent
			m.stagedActions = msg.Actions
			// Update action queue component
			m.actionQueue.SetActions(msg.Actions)
			// Update chat view with pending action for current agent
			m.chatView.SetPendingAction(m.pendingActionForAgent(m.chatView.AgentID()))
			// Update attention indicators
			m.updateNeedsAttention()
		}

	case StatsMsg:
		if msg.Err == nil && msg.Stats != nil {
			// Only use stats for commit count - usage comes from UsageUpdateMsg
			m.header.SetCommitCount(msg.Stats.CommitCount)
		}

	case ActionResultMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		} else {
			// Refresh staged actions after approve/reject
			cmds = append(cmds, m.fetchStagedActions())
		}

	case PermissionResultMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		} else {
			// Remove the permission from our pending list
			permID := m.chatView.PendingPermissionID()
			if permID != "" {
				for i := range m.pendingPermissions {
					if m.pendingPermissions[i].ID == permID {
						m.pendingPermissions = append(m.pendingPermissions[:i], m.pendingPermissions[i+1:]...)
						break
					}
				}
			}
		}
		// Clear the chat view's pending permission
		m.chatView.SetPendingPermission(nil)
		// Update attention indicators
		m.updateNeedsAttention()

	case AbortResultMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		}
		// Clear abort confirmation state (in case of error)
		if m.modeState.IsAbortConfirming() {
			_ = m.modeState.CancelAbort()
			m.chatView.SetAbortConfirming(false, "")
		}

	case tickMsg:
		// Advance spinner frame and schedule next tick
		m.spinnerFrame++
		m.agentList.SetSpinnerFrame(m.spinnerFrame)
		cmds = append(cmds, m.tickCmd())

		// Periodically refresh usage stats (every 30 seconds)
		if time.Since(m.lastUsageFetch) > 30*time.Second {
			m.lastUsageFetch = time.Now()
			cmds = append(cmds, m.fetchUsage())
		}

		// Refresh daemon stats every 30 seconds for commit count
		m.statsRefreshTick++
		if m.statsRefreshTick >= 300 && m.connState == ConnectionConnected {
			m.statsRefreshTick = 0
			cmds = append(cmds, m.fetchStats())
		}

	case UsageUpdateMsg:
		if msg.Err != nil {
			// Silently ignore usage fetch errors - not critical
			slog.Debug("usage fetch error", "err", msg.Err)
		} else {
			m.header.SetUsage(msg.Percent, msg.Remaining)
			m.lastUsageFetch = time.Now()
		}

	case clearErrorMsg:
		// Clear error display
		m.err = nil
		m.helpBar.ClearError()
	}

	return m, tea.Batch(cmds...)
}

// handleStreamEvent processes a stream event from the daemon.
// Returns a command to execute if needed (e.g., fetching chat history for newly selected agent).
func (m *Model) handleStreamEvent(event *daemon.StreamEvent) tea.Cmd {
	switch event.Type {
	case "chat_entry":
		// Handle chat entry events from stream-json parsing
		slog.Debug("chat_entry event received",
			"event_agent", event.AgentID,
			"chatview_agent", m.chatView.AgentID(),
			"match", event.AgentID == m.chatView.AgentID(),
			"has_entry", event.ChatEntry != nil,
		)
		if event.ChatEntry != nil && event.AgentID == m.chatView.AgentID() {
			m.chatView.AppendEntry(*event.ChatEntry)
		}

	case "output":
		// Deprecated: kept for backwards compatibility with raw output
		// This is no longer used by the chat view

	case "state":
		// Update agent state in the list
		agents := m.agentList.Agents()
		for i := range agents {
			if agents[i].ID == event.AgentID {
				agents[i].State = event.State
				m.agentList.SetAgents(agents)
				break
			}
		}
		m.header.SetAgentCounts(len(agents), countRunning(agents))

	case "created":
		// A new agent was created - add to list with proper StartedAt
		agents := m.agentList.Agents()
		startedAt := time.Now() // fallback
		if event.StartedAt != "" {
			if t, err := time.Parse(time.RFC3339, event.StartedAt); err == nil {
				startedAt = t
			}
		}
		agents = append(agents, daemon.AgentStatus{
			ID:        event.AgentID,
			Project:   event.Project,
			State:     "starting",
			StartedAt: startedAt,
		})
		m.agentList.SetAgents(agents)
		m.header.SetAgentCounts(len(agents), countRunning(agents))
		// Auto-select the new agent if no agent is currently selected
		if m.chatView.AgentID() == "" {
			return m.selectCurrentAgent()
		}

	case "deleted":
		// An agent was deleted - remove from list
		wasSelected := event.AgentID == m.chatView.AgentID()
		agents := m.agentList.Agents()
		for i := range agents {
			if agents[i].ID == event.AgentID {
				agents = append(agents[:i], agents[i+1:]...)
				break
			}
		}
		m.agentList.SetAgents(agents)
		m.header.SetAgentCounts(len(agents), countRunning(agents))
		// If the deleted agent was selected, auto-select the next agent
		if wasSelected {
			m.chatView.ClearAgent()
			if len(agents) > 0 {
				return m.selectCurrentAgent()
			}
		}

	case "permission_request":
		// A new permission request arrived
		if event.PermissionRequest != nil {
			slog.Debug("permission_request event",
				"agent", event.AgentID,
				"tool", event.PermissionRequest.ToolName,
			)
			// Add to our list of pending permissions
			m.pendingPermissions = append(m.pendingPermissions, *event.PermissionRequest)
			// Update chat view if this is for the current agent
			if event.AgentID == m.chatView.AgentID() {
				m.chatView.SetPendingPermission(m.pendingPermissionForAgent(event.AgentID))
			}
			// Update attention indicators
			m.updateNeedsAttention()
		}

	case "action_queued":
		// A new staged action was queued
		if event.StagedAction != nil {
			slog.Debug("action_queued event",
				"agent", event.AgentID,
				"action_id", event.StagedAction.ID,
				"type", event.StagedAction.Type,
			)
			// Add to our list of staged actions
			m.stagedActions = append(m.stagedActions, *event.StagedAction)
			// Update action queue component
			m.actionQueue.SetActions(m.stagedActions)
			// Update chat view if this is for the current agent
			if event.AgentID == m.chatView.AgentID() {
				m.chatView.SetPendingAction(m.pendingActionForAgent(event.AgentID))
			}
			// Update attention indicators
			m.updateNeedsAttention()
		}
	}
	return nil
}

// countRunning counts agents in running or starting state.
func countRunning(agents []daemon.AgentStatus) int {
	count := 0
	for _, a := range agents {
		if a.State == "running" || a.State == "starting" {
			count++
		}
	}
	return count
}

// pendingActionForAgent returns the first pending action for the given agent.
func (m *Model) pendingActionForAgent(agentID string) *daemon.StagedAction {
	if agentID == "" {
		return nil
	}
	for i := range m.stagedActions {
		if m.stagedActions[i].AgentID == agentID {
			return &m.stagedActions[i]
		}
	}
	return nil
}

// selectCurrentAgent updates the chat view with the currently selected agent
// and returns a command to fetch its chat history.
func (m *Model) selectCurrentAgent() tea.Cmd {
	agent := m.agentList.Selected()
	if agent == nil {
		return nil
	}
	m.chatView.SetAgent(agent.ID, agent.Project)
	m.chatView.SetPendingPermission(m.pendingPermissionForAgent(agent.ID))
	m.chatView.SetPendingAction(m.pendingActionForAgent(agent.ID))
	return m.fetchAgentChatHistory(agent.ID)
}

// syncFocusToComponents updates component focus states to match the ModeState focus.
func (m *Model) syncFocusToComponents(focus Focus) {
	switch focus {
	case FocusAgentList:
		m.chatView.SetFocused(false)
		m.inputLine.SetFocused(false)
	case FocusChatView:
		m.chatView.SetFocused(true)
		m.inputLine.SetFocused(false)
	case FocusInputLine:
		m.chatView.SetFocused(false)
		m.inputLine.SetFocused(true)
		m.chatView.SetInputView(m.inputLine.View(), 1)
	case FocusActionQueue:
		m.chatView.SetFocused(false)
		m.inputLine.SetFocused(false)
	}
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Header
	header := m.header.View()

	// Update help bar mode state
	m.modeState.SetPendingApprovals(
		m.pendingPermissionForAgent(m.chatView.AgentID()) != nil,
		m.pendingActionForAgent(m.chatView.AgentID()) != nil,
	)
	m.helpBar.SetModeState(m.modeState)
	status := m.helpBar.View()

	// Three-pane layout: agent list (left) | chat view (center) | action queue (right)
	agentList := m.agentList.View()
	chatView := m.chatView.View()
	actionQueue := m.actionQueue.View()

	content := lipgloss.JoinHorizontal(lipgloss.Top, agentList, chatView, actionQueue)

	return fmt.Sprintf("%s\n%s\n%s", header, content, status)
}

// updateLayout recalculates component dimensions for three-pane layout.
func (m *Model) updateLayout() {
	headerHeight := lipgloss.Height(m.header.View())
	statusHeight := 1 // Single line status bar
	contentHeight := m.height - headerHeight - statusHeight - 1
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Split width: 25% agent list, 50% chat view, 25% action queue
	listWidth := m.width * 25 / 100
	queueWidth := m.width * 25 / 100
	chatWidth := m.width - listWidth - queueWidth

	m.agentList.SetSize(listWidth, contentHeight)
	m.chatView.SetSize(chatWidth, contentHeight)
	m.actionQueue.SetSize(queueWidth, contentHeight)
	m.helpBar.SetWidth(m.width)

	// Input line sized to fit inside chat pane (accounting for border)
	inputLineHeight := 1
	m.inputLine.SetSize(chatWidth-4, inputLineHeight)
	m.chatView.SetInputView(m.inputLine.View(), inputLineHeight)
}

// Run starts the TUI without a daemon connection.
func Run() error {
	p := tea.NewProgram(
		New(),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}

// RunWithClient starts the TUI with a pre-connected daemon client.
func RunWithClient(client *daemon.Client) error {
	p := tea.NewProgram(
		NewWithClient(client),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}
