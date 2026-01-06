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
)

// Focus indicates which panel is currently focused.
type Focus int

const (
	FocusAgentList Focus = iota
	FocusChatView
	FocusInputLine
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

// ActionResultMsg is the result of approving/rejecting an action.
type ActionResultMsg struct {
	Err error
}

// PermissionResultMsg is the result of responding to a permission request.
type PermissionResultMsg struct {
	Err error
}

// tickMsg is sent on regular intervals to drive spinner animation.
type tickMsg time.Time

// Model is the main Bubbletea model for the fab TUI.
type Model struct {
	// Window dimensions
	width  int
	height int

	// UI state
	ready bool
	err   error
	focus Focus

	// Components
	header    Header
	agentList AgentList
	chatView  ChatView
	inputLine InputLine
	helpBar   HelpBar

	// Daemon client for IPC
	client   *daemon.Client
	attached bool

	// Event channel from dedicated streaming connection
	eventChan <-chan daemon.EventResult

	// Staged actions pending approval (for selected agent)
	stagedActions []daemon.StagedAction

	// Pending permission requests (for selected agent)
	pendingPermissions []daemon.PermissionRequest

	// Spinner animation frame counter
	spinnerFrame int

	// Key bindings
	keys KeyBindings
}

// New creates a new TUI model.
func New() Model {
	return Model{
		header:    NewHeader(),
		agentList: NewAgentList(),
		chatView:  NewChatView(),
		inputLine: NewInputLine(),
		helpBar:   NewHelpBar(),
		focus:     FocusAgentList,
		keys:      DefaultKeyBindings(),
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
		// Handle input line when focused
		if m.focus == FocusInputLine {
			switch {
			case key.Matches(msg, m.keys.Cancel):
				// Blur input and return to agent list
				m.inputLine.SetFocused(false)
				m.focus = FocusAgentList
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
						// Blur input and return to agent list
						m.inputLine.SetFocused(false)
						m.focus = FocusAgentList
						m.chatView.SetInputView(m.inputLine.View(), 1)
					}
				}
			case key.Matches(msg, m.keys.Tab):
				// Cycle to agent list
				m.inputLine.SetFocused(false)
				m.focus = FocusAgentList
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
			switch m.focus {
			case FocusAgentList:
				m.focus = FocusChatView
				m.chatView.SetFocused(true)
			case FocusChatView:
				m.chatView.SetFocused(false)
				m.inputLine.SetFocused(true)
				m.focus = FocusInputLine
				m.chatView.SetInputView(m.inputLine.View(), 1)
			case FocusInputLine:
				m.inputLine.SetFocused(false)
				m.focus = FocusAgentList
				m.chatView.SetInputView(m.inputLine.View(), 1)
			}

		case key.Matches(msg, m.keys.FocusChat):
			// Focus input line (vim-style)
			if m.chatView.AgentID() != "" {
				m.chatView.SetFocused(false)
				m.inputLine.SetFocused(true)
				m.focus = FocusInputLine
				m.chatView.SetInputView(m.inputLine.View(), 1)
			}

		case key.Matches(msg, m.keys.Approve):
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

		case key.Matches(msg, m.keys.Reject):
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

		case key.Matches(msg, m.keys.Select):
			// Select current agent for chat view
			if m.focus == FocusAgentList {
				if agent := m.agentList.Selected(); agent != nil {
					m.chatView.SetAgent(agent.ID, agent.Project)
					// Update pending permission/action for newly selected agent
					m.chatView.SetPendingPermission(m.pendingPermissionForAgent(agent.ID))
					m.chatView.SetPendingAction(m.pendingActionForAgent(agent.ID))
					// Fetch existing chat history for this agent
					cmds = append(cmds, m.fetchAgentChatHistory(agent.ID))
				}
			}

		case key.Matches(msg, m.keys.Down):
			switch m.focus {
			case FocusAgentList:
				m.agentList.MoveDown()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				m.chatView.ScrollDown(1)
			}

		case key.Matches(msg, m.keys.Up):
			switch m.focus {
			case FocusAgentList:
				m.agentList.MoveUp()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				m.chatView.ScrollUp(1)
			}

		case key.Matches(msg, m.keys.Top):
			switch m.focus {
			case FocusAgentList:
				m.agentList.MoveToTop()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				m.chatView.ScrollToTop()
			}

		case key.Matches(msg, m.keys.Bottom):
			switch m.focus {
			case FocusAgentList:
				m.agentList.MoveToBottom()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				m.chatView.ScrollToBottom()
			}

		case key.Matches(msg, m.keys.PageUp):
			if m.focus == FocusChatView {
				m.chatView.PageUp()
			}

		case key.Matches(msg, m.keys.PageDown):
			if m.focus == FocusChatView {
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
		cmds = append(cmds, m.waitForEvent())

	case StreamEventMsg:
		if msg.Err != nil {
			slog.Debug("stream error, stopping event loop", "err", msg.Err)
			m.err = msg.Err
		} else if msg.Event != nil {
			slog.Debug("stream event received", "type", msg.Event.Type)
			m.handleStreamEvent(msg.Event)
			// Continue listening for events
			cmds = append(cmds, m.waitForEvent())
		}

	case AgentListMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else {
			m.agentList.SetAgents(msg.Agents)
			m.header.SetAgentCounts(len(msg.Agents), countRunning(msg.Agents))
			// Attach to event stream after initial agent list fetch
			if !m.attached {
				cmds = append(cmds, m.attachToStream())
			}
			// Fetch staged actions for approval display
			cmds = append(cmds, m.fetchStagedActions())
		}

	case AgentInputMsg:
		if msg.Err != nil {
			m.err = msg.Err
		}

	case AgentChatHistoryMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else if msg.AgentID == m.chatView.AgentID() {
			// Only apply if still viewing this agent
			m.chatView.SetEntries(msg.Entries)
		}

	case StagedActionsMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else {
			// Store all staged actions - filtering happens in pendingActionForAgent
			m.stagedActions = msg.Actions
			// Update chat view with pending action for current agent
			m.chatView.SetPendingAction(m.pendingActionForAgent(m.chatView.AgentID()))
			// Update attention indicators
			m.updateNeedsAttention()
		}

	case ActionResultMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else {
			// Refresh staged actions after approve/reject
			cmds = append(cmds, m.fetchStagedActions())
		}

	case PermissionResultMsg:
		if msg.Err != nil {
			m.err = msg.Err
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

	case tickMsg:
		// Advance spinner frame and schedule next tick
		m.spinnerFrame++
		m.agentList.SetSpinnerFrame(m.spinnerFrame)
		cmds = append(cmds, m.tickCmd())
	}

	return m, tea.Batch(cmds...)
}

// handleStreamEvent processes a stream event from the daemon.
func (m *Model) handleStreamEvent(event *daemon.StreamEvent) {
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
		// Deprecated: kept for backwards compatibility with raw PTY output
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

	case "deleted":
		// An agent was deleted - remove from list
		agents := m.agentList.Agents()
		for i := range agents {
			if agents[i].ID == event.AgentID {
				agents = append(agents[:i], agents[i+1:]...)
				break
			}
		}
		m.agentList.SetAgents(agents)
		m.header.SetAgentCounts(len(agents), countRunning(agents))
		// Clear chat view if viewing deleted agent
		if event.AgentID == m.chatView.AgentID() {
			m.chatView.ClearAgent()
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
	}
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

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Header
	header := m.header.View()

	// Update help bar context
	pendingPermission := m.pendingPermissionForAgent(m.chatView.AgentID())
	pendingAction := m.pendingActionForAgent(m.chatView.AgentID())
	m.helpBar.SetContext(m.focus, pendingPermission != nil, pendingAction != nil)
	status := m.helpBar.View()

	// Side-by-side layout: agent list (left) | chat view (right)
	agentList := m.agentList.View()
	chatView := m.chatView.View()

	content := lipgloss.JoinHorizontal(lipgloss.Top, agentList, chatView)

	return fmt.Sprintf("%s\n%s\n%s", header, content, status)
}

// updateLayout recalculates component dimensions for side-by-side layout.
func (m *Model) updateLayout() {
	headerHeight := lipgloss.Height(m.header.View())
	statusHeight := 1 // Single line status bar
	contentHeight := m.height - headerHeight - statusHeight - 1
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Split width: 40% agent list, 60% chat view
	listWidth := m.width * 40 / 100
	chatWidth := m.width - listWidth

	m.agentList.SetSize(listWidth, contentHeight)
	m.chatView.SetSize(chatWidth, contentHeight)
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
