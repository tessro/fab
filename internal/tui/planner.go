package tui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tessro/fab/internal/daemon"
)

// PlannerModel is the TUI model for planner mode.
type PlannerModel struct {
	width  int
	height int

	ready bool
	err   error

	// Planner identification
	plannerID string

	// Components
	chatView  ChatView
	inputLine InputLine

	// Daemon client
	client    daemon.PlannerModeClient
	attached  bool
	eventChan <-chan daemon.EventResult

	// Plan completion state
	planComplete bool
	planFile     string

	// Key bindings
	keys KeyBindings
}

// plannerChatHistoryMsg contains chat history for the planner.
type plannerChatHistoryMsg struct {
	Entries []daemon.ChatEntryDTO
	Err     error
}

// plannerInputMsg is the result of sending input to the planner.
type plannerInputMsg struct {
	Err error
}

// NewPlannerModel creates a new planner TUI model.
func NewPlannerModel(client daemon.PlannerModeClient, plannerID string) PlannerModel {
	chatView := NewChatView()
	chatView.SetAgent(plannerID, "plan", "claude") // Default to Claude, updated when planner info is fetched

	return PlannerModel{
		plannerID: plannerID,
		chatView:  chatView,
		inputLine: NewInputLine(),
		client:    client,
		keys:      DefaultKeyBindings(),
	}
}

// Init implements tea.Model.
func (m PlannerModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.inputLine.input.Cursor.BlinkCmd(),
		m.fetchPlannerChatHistory(),
	}
	if m.client != nil {
		cmds = append(cmds, m.attachToStream())
	}
	return tea.Batch(cmds...)
}

// attachToStream connects to the daemon event stream.
func (m PlannerModel) attachToStream() tea.Cmd {
	return attachToStreamCmd(m.client)
}

// waitForEvent waits for the next event from the channel.
func (m PlannerModel) waitForEvent() tea.Cmd {
	return waitForEventCmd(m.eventChan)
}

// fetchPlannerChatHistory retrieves chat history for the planner.
func (m PlannerModel) fetchPlannerChatHistory() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return plannerChatHistoryMsg{Entries: nil}
		}
		resp, err := m.client.PlanChatHistory(m.plannerID, 0)
		if err != nil {
			return plannerChatHistoryMsg{Err: err}
		}
		return plannerChatHistoryMsg{Entries: resp.Entries}
	}
}

// sendPlannerMessage sends a message to the planner.
func (m PlannerModel) sendPlannerMessage(content string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.PlanSendMessage(m.plannerID, content)
		return plannerInputMsg{Err: err}
	}
}

// Update implements tea.Model.
func (m PlannerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Quit):
			if m.client != nil {
				m.client.Close()
			}
			return m, tea.Quit

		case key.Matches(msg, m.keys.Cancel):
			// Clear input
			m.inputLine.Clear()

		case key.Matches(msg, m.keys.Submit):
			// Send message to planner
			if m.client != nil {
				input := m.inputLine.Value()
				if input != "" {
					// Show user message immediately
					m.chatView.AppendEntry(daemon.ChatEntryDTO{
						Role:      "user",
						Content:   input,
						Timestamp: time.Now().Format(time.RFC3339),
					})
					cmds = append(cmds, m.sendPlannerMessage(input))
					m.inputLine.Clear()
				}
			}

		default:
			// Pass to input
			cmd := m.inputLine.Update(msg)
			cmds = append(cmds, cmd)
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.updateLayout()
		m.ready = true

	case streamStartMsg:
		m.eventChan = msg.EventChan
		m.attached = true
		cmds = append(cmds, m.waitForEvent())

	case streamEventMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else if msg.Event != nil {
			m.handleStreamEvent(msg.Event)
			cmds = append(cmds, m.waitForEvent())
		}

	case plannerChatHistoryMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else {
			m.chatView.SetEntries(msg.Entries)
		}

	case plannerInputMsg:
		if msg.Err != nil {
			m.err = msg.Err
		}
	}

	return m, tea.Batch(cmds...)
}

// handleStreamEvent processes stream events.
func (m *PlannerModel) handleStreamEvent(event *daemon.StreamEvent) {
	switch event.Type {
	case "planner_chat_entry":
		if event.ChatEntry != nil && event.AgentID == m.plannerID {
			m.chatView.AppendEntry(*event.ChatEntry)
		}
	case "planner_state":
		// Could update a status indicator
	case "planner_info":
		// Description changed - could update a status indicator if needed
	case "plan_complete":
		if event.AgentID == m.plannerID {
			m.planComplete = true
			m.planFile = event.Data
		}
	}
}

// View implements tea.Model.
func (m PlannerModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39")).
		Padding(0, 1)

	header := headerStyle.Render(fmt.Sprintf("🚌 fab plan (%s)", m.plannerID))

	// Plan completion banner
	var banner string
	if m.planComplete {
		bannerStyle := lipgloss.NewStyle().
			Background(lipgloss.Color("28")).
			Foreground(lipgloss.Color("255")).
			Padding(0, 1).
			Bold(true)
		banner = bannerStyle.Render(fmt.Sprintf("✓ Plan complete: %s", m.planFile)) + "\n"
	}

	// Input prompt
	inputStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(0, 1).
		Width(m.width - 4)

	promptStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("39"))

	input := inputStyle.Render(promptStyle.Render("> ") + m.inputLine.input.View())

	// Status bar
	statusStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	status := statusStyle.Render("Enter: send | Esc: clear | Ctrl+C: quit")
	if m.err != nil {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(m.err.Error())
	}

	// Chat view (fills remaining space)
	chatHeight := m.height - 5 // header + input + status + margins
	if banner != "" {
		chatHeight-- // Account for banner
	}
	if chatHeight < 1 {
		chatHeight = 1
	}
	m.chatView.SetSize(m.width, chatHeight)
	chat := m.chatView.View()

	if banner != "" {
		return fmt.Sprintf("%s\n%s%s\n%s\n%s", header, banner, chat, input, status)
	}
	return fmt.Sprintf("%s\n%s\n%s\n%s", header, chat, input, status)
}

// updateLayout recalculates component dimensions.
func (m *PlannerModel) updateLayout() {
	inputHeight := 1
	m.inputLine.SetSize(m.width-6, inputHeight) // Account for border and prompt

	chatHeight := m.height - 5
	if chatHeight < 1 {
		chatHeight = 1
	}
	m.chatView.SetSize(m.width, chatHeight)
}

// RunPlannerMode starts the TUI in planner mode.
func RunPlannerMode(client daemon.PlannerModeClient, plannerID string) error {
	p := tea.NewProgram(
		NewPlannerModel(client, plannerID),
		tea.WithAltScreen(),
	)
	_, err := p.Run()
	return err
}
