// Package tui provides the Bubbletea-based terminal user interface for fab.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tessro/fab/internal/daemon"
)

// Focus indicates which panel is currently focused.
type Focus int

const (
	FocusAgentList Focus = iota
	FocusPTYView
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
	ptyView   PTYView

	// Daemon client for IPC
	client   *daemon.Client
	attached bool
}

// New creates a new TUI model.
func New() Model {
	return Model{
		header:    NewHeader(),
		agentList: NewAgentList(),
		ptyView:   NewPTYView(),
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
	if m.client != nil {
		// Attach to stream and start receiving events
		return tea.Batch(
			m.attachToStream(),
			m.fetchAgentList(),
		)
	}
	return nil
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

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "tab":
			// Toggle focus between agent list and PTY view
			if m.focus == FocusAgentList {
				m.focus = FocusPTYView
				m.ptyView.SetFocused(true)
			} else {
				m.focus = FocusAgentList
				m.ptyView.SetFocused(false)
			}

		case "enter":
			// Select current agent for PTY view
			if m.focus == FocusAgentList {
				if agent := m.agentList.Selected(); agent != nil {
					m.ptyView.SetAgent(agent.ID, agent.Project)
				}
			}

		case "j", "down":
			if m.focus == FocusAgentList {
				m.agentList.MoveDown()
			} else {
				m.ptyView.ScrollDown(1)
			}

		case "k", "up":
			if m.focus == FocusAgentList {
				m.agentList.MoveUp()
			} else {
				m.ptyView.ScrollUp(1)
			}

		case "g", "home":
			if m.focus == FocusAgentList {
				m.agentList.MoveToTop()
			} else {
				m.ptyView.ScrollToTop()
			}

		case "G", "end":
			if m.focus == FocusAgentList {
				m.agentList.MoveToBottom()
			} else {
				m.ptyView.ScrollToBottom()
			}

		case "ctrl+u", "pgup":
			if m.focus == FocusPTYView {
				m.ptyView.PageUp()
			}

		case "ctrl+d", "pgdown":
			if m.focus == FocusPTYView {
				m.ptyView.PageDown()
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
		}
	}

	return m, tea.Batch(cmds...)
}

// handleStreamEvent processes a stream event from the daemon.
func (m *Model) handleStreamEvent(event *daemon.StreamEvent) {
	switch event.Type {
	case "output":
		// Only append output if this is the currently viewed agent
		if event.AgentID == m.ptyView.AgentID() {
			m.ptyView.AppendOutput(event.Data)
		}

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
		// Clear PTY view if viewing deleted agent
		if event.AgentID == m.ptyView.AgentID() {
			m.ptyView.ClearAgent()
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
	if m.focus == FocusAgentList {
		helpText = "j/k: navigate  enter: view output  tab: switch pane  q: quit"
	} else {
		helpText = "j/k/pgup/pgdn: scroll  tab: switch pane  q: quit"
	}
	status := statusStyle.Width(m.width).Render(helpText)

	// Side-by-side layout: agent list (left) | PTY view (right)
	agentList := m.agentList.View()
	ptyView := m.ptyView.View()

	content := lipgloss.JoinHorizontal(lipgloss.Top, agentList, ptyView)

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

	// Split width: 40% agent list, 60% PTY view
	listWidth := m.width * 40 / 100
	ptyWidth := m.width - listWidth

	m.agentList.SetSize(listWidth, contentHeight)
	m.ptyView.SetSize(ptyWidth, contentHeight)
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
