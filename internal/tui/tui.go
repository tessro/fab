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

	// TODO: daemon client for IPC
	// TODO: agent list state
	// TODO: focused agent for output view
}

// New creates a new TUI model.
func New() Model {
	return Model{}
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
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
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
	header := headerStyle.Width(m.width).Render("🚌 fab")

	// Status bar
	status := statusStyle.Width(m.width).Render("Press q to quit")

	// Main content area
	contentHeight := m.height - lipgloss.Height(header) - lipgloss.Height(status)
	content := contentStyle.
		Width(m.width).
		Height(contentHeight).
		Render("No agents running")

	return fmt.Sprintf("%s\n%s\n%s", header, content, status)
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
