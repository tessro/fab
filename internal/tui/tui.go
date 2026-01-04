// Package tui provides the Bubbletea-based terminal user interface for fab.
package tui

import (
	"fmt"
	"time"

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

	// Daemon client for IPC
	client   *daemon.Client
	attached bool
}

// New creates a new TUI model.
func New() Model {
	return Model{
		header:    NewHeader(),
		agentList: NewAgentList(),
		chatView:  NewChatView(),
		inputLine: NewInputLine(),
		focus:     FocusAgentList,
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
	cmds := []tea.Cmd{m.inputLine.input.Cursor.BlinkCmd()}
	if m.client != nil {
		// Fetch agent list first, then attach to stream
		// (must be sequential to avoid concurrent decoder access)
		cmds = append(cmds, m.fetchAgentList())
	}
	return tea.Batch(cmds...)
}

// attachToStream connects to the daemon event stream.
func (m Model) attachToStream() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		if err := m.client.Attach(nil); err != nil {
			return StreamEventMsg{Err: err}
		}
		// Start receiving events
		event, err := m.client.RecvEvent()
		return StreamEventMsg{Event: event, Err: err}
	}
}

// waitForEvent waits for the next stream event.
func (m Model) waitForEvent() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil || !m.attached {
			return nil
		}
		event, err := m.client.RecvEvent()
		return StreamEventMsg{Event: event, Err: err}
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
// Currently returns empty entries since chat history streaming handles real-time updates.
func (m Model) fetchAgentChatHistory(agentID string) tea.Cmd {
	return func() tea.Msg {
		// Chat history is streamed in real-time via chat_entry events.
		// When an agent is selected, any new messages will appear via the stream.
		// TODO: Add agent.chat_history endpoint to fetch existing history on connect.
		return AgentChatHistoryMsg{AgentID: agentID, Entries: nil}
	}
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle input line when focused
		if m.focus == FocusInputLine {
			switch msg.String() {
			case "esc":
				// Blur input and return to agent list
				m.inputLine.SetFocused(false)
				m.focus = FocusAgentList
			case "enter":
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
					}
				}
			case "tab":
				// Cycle to agent list
				m.inputLine.SetFocused(false)
				m.focus = FocusAgentList
			default:
				// Pass all other keys to input
				cmd := m.inputLine.Update(msg)
				cmds = append(cmds, cmd)
			}
			return m, tea.Batch(cmds...)
		}

		switch msg.String() {
		case "q", "ctrl+c":
			// Close client to unblock any pending RecvEvent() calls
			if m.client != nil {
				m.client.Close()
			}
			return m, tea.Quit

		case "tab":
			// Cycle focus: agent list -> chat view -> input line -> agent list
			switch m.focus {
			case FocusAgentList:
				m.focus = FocusChatView
				m.chatView.SetFocused(true)
			case FocusChatView:
				m.chatView.SetFocused(false)
				m.inputLine.SetFocused(true)
				m.focus = FocusInputLine
			case FocusInputLine:
				m.inputLine.SetFocused(false)
				m.focus = FocusAgentList
			}

		case "i":
			// Focus input line (vim-style)
			if m.chatView.AgentID() != "" {
				m.chatView.SetFocused(false)
				m.inputLine.SetFocused(true)
				m.focus = FocusInputLine
			}

		case "enter":
			// Select current agent for chat view
			if m.focus == FocusAgentList {
				if agent := m.agentList.Selected(); agent != nil {
					m.chatView.SetAgent(agent.ID, agent.Project)
					// Fetch existing chat history for this agent
					cmds = append(cmds, m.fetchAgentChatHistory(agent.ID))
				}
			}

		case "j", "down":
			if m.focus == FocusAgentList {
				m.agentList.MoveDown()
			} else if m.focus == FocusChatView {
				m.chatView.ScrollDown(1)
			}

		case "k", "up":
			if m.focus == FocusAgentList {
				m.agentList.MoveUp()
			} else if m.focus == FocusChatView {
				m.chatView.ScrollUp(1)
			}

		case "g", "home":
			if m.focus == FocusAgentList {
				m.agentList.MoveToTop()
			} else if m.focus == FocusChatView {
				m.chatView.ScrollToTop()
			}

		case "G", "end":
			if m.focus == FocusAgentList {
				m.agentList.MoveToBottom()
			} else if m.focus == FocusChatView {
				m.chatView.ScrollToBottom()
			}

		case "ctrl+u", "pgup":
			if m.focus == FocusChatView {
				m.chatView.PageUp()
			}

		case "ctrl+d", "pgdown":
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

	case StreamEventMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else if msg.Event != nil {
			m.attached = true
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
	}

	return m, tea.Batch(cmds...)
}

// handleStreamEvent processes a stream event from the daemon.
func (m *Model) handleStreamEvent(event *daemon.StreamEvent) {
	switch event.Type {
	case "chat_entry":
		// Handle chat entry events from stream-json parsing
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
		// A new agent was created - refresh the list
		// For now, just add a placeholder; a full refresh would be better
		agents := m.agentList.Agents()
		agents = append(agents, daemon.AgentStatus{
			ID:      event.AgentID,
			Project: event.Project,
			State:   "starting",
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

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Header
	header := m.header.View()

	// Status bar with context-sensitive help
	var helpText string
	switch m.focus {
	case FocusAgentList:
		helpText = "j/k: navigate  enter: view chat  i: input  tab: switch pane  q: quit"
	case FocusChatView:
		helpText = "j/k/pgup/pgdn: scroll  i: input  tab: switch pane  q: quit"
	case FocusInputLine:
		helpText = "enter: send  esc: cancel  tab: switch pane"
	}
	status := statusStyle.Width(m.width).Render(helpText)

	// Side-by-side layout: agent list (left) | chat view (right)
	agentList := m.agentList.View()
	chatView := m.chatView.View()

	content := lipgloss.JoinHorizontal(lipgloss.Top, agentList, chatView)

	// Input line (only show when agent is selected)
	var inputLine string
	if m.chatView.AgentID() != "" {
		inputLine = m.inputLine.View()
	}

	return fmt.Sprintf("%s\n%s\n%s\n%s", header, content, inputLine, status)
}

// updateLayout recalculates component dimensions for side-by-side layout.
func (m *Model) updateLayout() {
	headerHeight := lipgloss.Height(m.header.View())
	statusHeight := 1    // Single line status bar
	inputLineHeight := 3 // Input line with border
	contentHeight := m.height - headerHeight - statusHeight - inputLineHeight - 1
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Split width: 40% agent list, 60% chat view
	listWidth := m.width * 40 / 100
	chatWidth := m.width - listWidth

	m.agentList.SetSize(listWidth, contentHeight)
	m.chatView.SetSize(chatWidth, contentHeight)
	m.inputLine.SetSize(m.width, inputLineHeight)
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
