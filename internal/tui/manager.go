package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tessro/fab/internal/daemon"
)

// ManagerModel is the TUI model for manager mode.
type ManagerModel struct {
	width  int
	height int

	ready bool
	err   error

	// Components
	chatView  ChatView
	inputLine InputLine

	// Daemon client
	client    *daemon.Client
	attached  bool
	eventChan <-chan daemon.EventResult

	// Project this manager is for
	project string

	// Key bindings
	keys KeyBindings
}

// ManagerChatHistoryMsg contains chat history for the manager.
type ManagerChatHistoryMsg struct {
	Entries []daemon.ChatEntryDTO
	Err     error
}

// ManagerInputMsg is the result of sending input to the manager.
type ManagerInputMsg struct {
	Err error
}

// ManagerClearHistoryMsg is the result of clearing the manager chat history.
type ManagerClearHistoryMsg struct {
	Err error
}

// NewManagerModel creates a new manager TUI model.
func NewManagerModel(client *daemon.Client, project string) ManagerModel {
	chatView := NewChatView()
	chatView.SetAgent("manager", project)

	return ManagerModel{
		chatView:  chatView,
		inputLine: NewInputLine(),
		client:    client,
		project:   project,
		keys:      DefaultKeyBindings(),
	}
}

// Init implements tea.Model.
func (m ManagerModel) Init() tea.Cmd {
	cmds := []tea.Cmd{
		m.inputLine.input.Cursor.BlinkCmd(),
		m.fetchManagerChatHistory(),
	}
	if m.client != nil {
		cmds = append(cmds, m.attachToStream())
	}
	return tea.Batch(cmds...)
}

// attachToStream connects to the daemon event stream.
func (m ManagerModel) attachToStream() tea.Cmd {
	return attachToStreamCmd(m.client)
}

// waitForEvent waits for the next event from the channel.
func (m ManagerModel) waitForEvent() tea.Cmd {
	return waitForEventCmd(m.eventChan)
}

// fetchManagerChatHistory retrieves chat history for the manager.
func (m ManagerModel) fetchManagerChatHistory() tea.Cmd {
	project := m.project
	return func() tea.Msg {
		if m.client == nil {
			return ManagerChatHistoryMsg{Entries: nil}
		}
		resp, err := m.client.ManagerChatHistory(project, 0)
		if err != nil {
			return ManagerChatHistoryMsg{Err: err}
		}
		return ManagerChatHistoryMsg{Entries: resp.Entries}
	}
}

// sendManagerMessage sends a message to the manager.
func (m ManagerModel) sendManagerMessage(content string) tea.Cmd {
	project := m.project
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.ManagerSendMessage(project, content)
		return ManagerInputMsg{Err: err}
	}
}

// clearManagerHistory clears the manager chat history.
func (m ManagerModel) clearManagerHistory() tea.Cmd {
	project := m.project
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.ManagerClearHistory(project)
		return ManagerClearHistoryMsg{Err: err}
	}
}

// Update implements tea.Model.
func (m ManagerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
			// Send message to manager
			if m.client != nil {
				input := m.inputLine.Value()
				if input != "" {
					// Check for slash commands
					if strings.TrimSpace(input) == "/clear" {
						cmds = append(cmds, m.clearManagerHistory())
						m.inputLine.Clear()
					} else {
						// Show user message immediately
						m.chatView.AppendEntry(daemon.ChatEntryDTO{
							Role:      "user",
							Content:   input,
							Timestamp: time.Now().Format(time.RFC3339),
						})
						cmds = append(cmds, m.sendManagerMessage(input))
						m.inputLine.Clear()
					}
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

	case StreamStartMsg:
		m.eventChan = msg.EventChan
		m.attached = true
		cmds = append(cmds, m.waitForEvent())

	case StreamEventMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else if msg.Event != nil {
			m.handleStreamEvent(msg.Event)
			cmds = append(cmds, m.waitForEvent())
		}

	case ManagerChatHistoryMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else {
			m.chatView.SetEntries(msg.Entries)
		}

	case ManagerInputMsg:
		if msg.Err != nil {
			m.err = msg.Err
		}

	case ManagerClearHistoryMsg:
		if msg.Err != nil {
			m.err = msg.Err
		} else {
			// Clear the chat view
			m.chatView.SetEntries(nil)
		}
	}

	return m, tea.Batch(cmds...)
}

// handleStreamEvent processes stream events.
func (m *ManagerModel) handleStreamEvent(event *daemon.StreamEvent) {
	switch event.Type {
	case "manager_chat_entry":
		if event.ChatEntry != nil {
			m.chatView.AppendEntry(*event.ChatEntry)
		}
	case "manager_state":
		// Could update a status indicator
	}
}

// View implements tea.Model.
func (m ManagerModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("39")).
		Padding(0, 1)

	header := headerStyle.Render("🚌 fab manager")

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

	status := statusStyle.Render("Enter: send | /clear: reset context | Esc: clear input | Ctrl+C: quit")
	if m.err != nil {
		status = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(m.err.Error())
	}

	// Chat view (fills remaining space)
	chatHeight := m.height - 5 // header + input + status + margins
	if chatHeight < 1 {
		chatHeight = 1
	}
	m.chatView.SetSize(m.width, chatHeight)
	chat := m.chatView.View()

	return fmt.Sprintf("%s\n%s\n%s\n%s", header, chat, input, status)
}

// updateLayout recalculates component dimensions.
func (m *ManagerModel) updateLayout() {
	inputHeight := 1
	m.inputLine.SetSize(m.width-6, inputHeight) // Account for border and prompt

	chatHeight := m.height - 5
	if chatHeight < 1 {
		chatHeight = 1
	}
	m.chatView.SetSize(m.width, chatHeight)
}

// RunManagerMode starts the TUI in manager mode for a project.
func RunManagerMode(client *daemon.Client, project string) error {
	p := tea.NewProgram(
		NewManagerModel(client, project),
		tea.WithAltScreen(),
	)
	_, err := p.Run()
	return err
}
