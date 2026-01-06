package tui

import "github.com/charmbracelet/lipgloss"

var (
	// Colors
	primaryColor   = lipgloss.Color("#7C3AED") // Purple
	secondaryColor = lipgloss.Color("#10B981") // Green
	mutedColor     = lipgloss.Color("#6B7280") // Gray
	errorColor     = lipgloss.Color("#EF4444") // Red
	warningColor   = lipgloss.Color("#F59E0B") // Amber/Yellow

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

	// Connection status styles
	headerConnDisconnectedStyle = lipgloss.NewStyle().
					Foreground(errorColor).
					Background(primaryColor)

	headerConnReconnectingStyle = lipgloss.NewStyle().
					Foreground(warningColor).
					Background(primaryColor)

	// Status bar style
	statusStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(0, 1)

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

	// Chat view header styles
	chatHeaderStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2D2D2D")).
			Padding(0, 1)

	chatHeaderFocusedStyle = lipgloss.NewStyle().
				Background(primaryColor).
				Padding(0, 1)

	chatHeaderAgentStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Bold(true)

	chatHeaderProjectStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#A0A0A0"))

	chatEmptyStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(1, 2)

	// Input line styles (inline, no border since it's inside the chat pane)
	inputLineStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2D2D2D")).
			Padding(0, 1)

	inputLineFocusedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#3B3B3B")).
				Padding(0, 1)

	// Chat view styles
	chatAssistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	chatUserStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	chatToolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	chatResultStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray

	chatViewBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(mutedColor)

	chatViewFocusedBorderStyle = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(primaryColor)

	// Pending action styles
	pendingActionStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#3B3B3B")).
				Padding(0, 1)

	pendingActionLabelStyle = lipgloss.NewStyle().
				Foreground(primaryColor).
				Bold(true)

	// Permission request styles
	pendingPermissionStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#4B3B2B")).
				Padding(0, 1)

	pendingPermissionLabelStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#FFA500")). // Orange for attention
					Bold(true)

	pendingPermissionToolStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#FFFFFF")).
					Bold(true)

	// Abort confirmation styles
	abortConfirmStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#4B2B2B")). // Dark red background
				Padding(0, 1)

	abortConfirmLabelStyle = lipgloss.NewStyle().
				Foreground(errorColor).
				Bold(true)

	abortConfirmHintStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#A0A0A0"))

	// Error display styles
	errorBarStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Padding(0, 1)
)
