// Package tui provides the Bubbletea-based terminal user interface for fab.
package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Model is the main Bubbletea model for the fab TUI.
type Model struct {
	// Window dimensions
	width  int
	height int

	// UI state
	ready bool
	err   error

	// Components
	header    Header
	agentList AgentList

	// TODO: daemon client for IPC
	// TODO: focused agent for output view
}

// New creates a new TUI model.
func New() Model {
	return Model{
		header:    NewHeader(),
		agentList: NewAgentList(),
	}
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "j", "down":
			m.agentList.MoveDown()
		case "k", "up":
			m.agentList.MoveUp()
		case "g", "home":
			m.agentList.MoveToTop()
		case "G", "end":
			m.agentList.MoveToBottom()
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.header.SetWidth(m.width)
		m.updateAgentListSize()
		m.ready = true
	}

	return m, nil
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Header
	header := m.header.View()

	// Status bar
	status := statusStyle.Width(m.width).Render("j/k: navigate  q: quit")

	// Agent list
	agentList := m.agentList.View()

	return fmt.Sprintf("%s\n%s\n%s", header, agentList, status)
}

// updateAgentListSize recalculates the agent list dimensions.
func (m *Model) updateAgentListSize() {
	headerHeight := lipgloss.Height(m.header.View())
	statusHeight := 1 // Single line status bar
	listHeight := m.height - headerHeight - statusHeight - 1

	m.agentList.SetSize(m.width, listHeight)
}

// Run starts the TUI.
func Run() error {
	p := tea.NewProgram(
		New(),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}
