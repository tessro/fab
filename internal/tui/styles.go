package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	primaryColor   = lipgloss.Color("#7C3AED") // Purple
	secondaryColor = lipgloss.Color("#10B981") // Green
	mutedColor     = lipgloss.Color("#6B7280") // Gray
	errorColor     = lipgloss.Color("#EF4444") // Red

	// Header styles
	headerContainerStyle = lipgloss.NewStyle().
				Background(primaryColor)

	headerBrandStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(primaryColor).
				Padding(0, 1)

	headerStatsStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E0E0E0")).
				Background(primaryColor).
				Padding(0, 1)

	// Status bar style
	statusStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(0, 1)

	// Main content area
	contentStyle = lipgloss.NewStyle().
			Padding(1, 2)

	// Agent list styles
	agentListContainerStyle = lipgloss.NewStyle()

	agentListEmptyStyle = lipgloss.NewStyle().
				Foreground(mutedColor).
				Padding(1, 2)

	agentRowStyle = lipgloss.NewStyle().
			Padding(0, 1)

	agentRowSelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#3B3B3B")).
				Padding(0, 1)

	agentIDStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true)

	agentProjectStyle = lipgloss.NewStyle().
				Foreground(primaryColor)

	agentTaskStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A0A0A0"))

	agentDurationStyle = lipgloss.NewStyle().
				Foreground(mutedColor)
)
