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

	// Header separator style
	headerSeparatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#A0A0A0")).
				Background(primaryColor)

	// Status bar style
	statusStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(0, 1)

	// Pane title styles
	paneTitleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#2D2D2D")).
			Bold(true).
			Padding(0, 1)

	paneTitleFocusedStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(primaryColor).
				Bold(true).
				Padding(0, 1)

	// Pane border styles
	paneBorderStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(mutedColor)

	paneBorderFocusedStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(primaryColor)

	// Agent list styles
	agentListContainerStyle = lipgloss.NewStyle()

	agentListEmptyStyle = lipgloss.NewStyle().
				Foreground(mutedColor).
				Padding(0, 1)

	agentRowStyle = lipgloss.NewStyle().
			Padding(0, 1)

	agentRowSelectedStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#3B3B3B")).
				Padding(0, 1)

	agentIDStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true)

	// Special style for the manager agent
	agentManagerIDStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFD700")). // Gold
				Bold(true)

	// Special style for planner agents
	agentPlannerIDStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#00BFFF")). // Deep Sky Blue
				Bold(true)

	// Special style for the director agent
	agentDirectorIDStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FF8C00")). // Dark Orange
				Bold(true)

	agentProjectStyle = lipgloss.NewStyle().
				Foreground(primaryColor)

	agentTaskStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#A0A0A0"))

	agentDescriptionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888888")).
				Italic(true)

	agentDurationStyle = lipgloss.NewStyle().
				Foreground(mutedColor)

	// Backend styles - distinct color per backend
	agentBackendClaudeStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#60A5FA")) // Light blue for Claude

	agentBackendCodexStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#34D399")) // Emerald for Codex

	chatEmptyStyle = lipgloss.NewStyle().
			Foreground(mutedColor).
			Padding(1, 2)

	// Input line styles (no border - docked inside chat pane)
	inputLineStyle = lipgloss.NewStyle().
			Padding(0, 1)

	inputLineFocusedStyle = lipgloss.NewStyle().
				Padding(0, 1)

	// Input mode indicator style (shown on divider line)
	inputModeIndicatorStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFFFFF")).
				Background(primaryColor).
				Bold(true).
				Padding(0, 1)

	// Input divider style (horizontal line above input)
	inputDividerStyle = lipgloss.NewStyle().
				Foreground(mutedColor)

	inputDividerFocusedStyle = lipgloss.NewStyle().
					Foreground(primaryColor)

	// Chat view styles
	chatAssistantStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("12")) // blue
	chatUserStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("10")) // green
	chatToolStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	chatResultStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))  // gray
	chatTimeStyle      = lipgloss.NewStyle().Foreground(mutedColor)           // gray, muted

	chatViewBorderStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(mutedColor)

	chatViewFocusedBorderStyle = lipgloss.NewStyle().
					Border(lipgloss.RoundedBorder()).
					BorderForeground(primaryColor)

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

	// User question styles (AskUserQuestion from Claude)
	userQuestionStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#2B3B4B")). // Dark blue background
				Padding(0, 1)

	userQuestionHeaderStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#60A5FA")). // Light blue
				Bold(true)

	userQuestionOptionStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#E0E0E0"))

	userQuestionSelectedStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("#FFFFFF")).
					Background(lipgloss.Color("#4B5B6B")).
					Bold(true)

	userQuestionDescStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888888")).
				Italic(true)

	// Error display styles
	errorBarStyle = lipgloss.NewStyle().
			Foreground(errorColor).
			Padding(0, 1)

)
