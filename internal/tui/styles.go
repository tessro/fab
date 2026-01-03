package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	primaryColor   = lipgloss.Color("#7C3AED") // Purple
	secondaryColor = lipgloss.Color("#10B981") // Green
	mutedColor     = lipgloss.Color("#6B7280") // Gray
	errorColor     = lipgloss.Color("#EF4444") // Red

	// Header style
	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(primaryColor).
			Padding(0, 1)

	// Status bar style
	statusStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(0, 1)

	// Main content area
	contentStyle = lipgloss.NewStyle().
			Padding(1, 2)
)
