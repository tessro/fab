package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tessro/fab/internal/daemon"
)

// Spinner frames for animated state indicators
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// AgentList displays a navigable list of agents with status indicators.
type AgentList struct {
	width          int
	height         int
	agents         []daemon.AgentStatus
	selected       int
	spinnerFrame   int
	needsAttention map[string]bool // agents with pending permissions/actions
	focused        bool
}

// NewAgentList creates a new agent list component.
func NewAgentList() AgentList {
	return AgentList{
		selected:       0,
		needsAttention: make(map[string]bool),
	}
}

// SetSize updates the component dimensions.
func (l *AgentList) SetSize(width, height int) {
	l.width = width
	l.height = height
}

// SetAgents updates the agent list.
func (l *AgentList) SetAgents(agents []daemon.AgentStatus) {
	l.agents = agents
	// Adjust selection if list shrunk
	if l.selected >= len(agents) && len(agents) > 0 {
		l.selected = len(agents) - 1
	}
	if len(agents) == 0 {
		l.selected = 0
	}
}

// Agents returns the current agent list.
func (l *AgentList) Agents() []daemon.AgentStatus {
	return l.agents
}

// Selected returns the currently selected agent, or nil if none.
func (l *AgentList) Selected() *daemon.AgentStatus {
	if len(l.agents) == 0 || l.selected < 0 || l.selected >= len(l.agents) {
		return nil
	}
	return &l.agents[l.selected]
}

// SelectedIndex returns the current selection index.
func (l *AgentList) SelectedIndex() int {
	return l.selected
}

// MoveUp moves selection up one item.
func (l *AgentList) MoveUp() {
	if l.selected > 0 {
		l.selected--
	}
}

// MoveDown moves selection down one item.
func (l *AgentList) MoveDown() {
	if l.selected < len(l.agents)-1 {
		l.selected++
	}
}

// MoveToTop moves selection to the first item.
func (l *AgentList) MoveToTop() {
	l.selected = 0
}

// MoveToBottom moves selection to the last item.
func (l *AgentList) MoveToBottom() {
	if len(l.agents) > 0 {
		l.selected = len(l.agents) - 1
	}
}

// SetSpinnerFrame updates the current spinner animation frame.
func (l *AgentList) SetSpinnerFrame(frame int) {
	l.spinnerFrame = frame
}

// SetNeedsAttention updates which agents have pending approvals.
func (l *AgentList) SetNeedsAttention(agentIDs map[string]bool) {
	l.needsAttention = agentIDs
}

// SetFocused sets the focus state.
func (l *AgentList) SetFocused(focused bool) {
	l.focused = focused
}

// IsFocused returns whether the agent list is focused.
func (l *AgentList) IsFocused() bool {
	return l.focused
}

// View renders the agent list.
func (l AgentList) View() string {
	// Calculate inner dimensions (accounting for border)
	innerWidth := l.width - 2
	innerHeight := l.height - 2 - 1 // -2 for border, -1 for header
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Header
	titleStyle := paneTitleStyle
	if l.focused {
		titleStyle = paneTitleFocusedStyle
	}
	header := titleStyle.Width(innerWidth).Render("Agents")

	// Content
	var content string
	if len(l.agents) == 0 {
		content = agentListEmptyStyle.Width(innerWidth).Height(innerHeight).Render("No agents")
	} else {
		var rows []string
		for i, agent := range l.agents {
			row := l.renderAgent(i, agent, innerWidth)
			rows = append(rows, row)
		}
		content = agentListContainerStyle.Width(innerWidth).Height(innerHeight).Render(strings.Join(rows, "\n"))
	}

	// Combine header and content
	inner := lipgloss.JoinVertical(lipgloss.Left, header, content)

	// Apply border
	borderStyle := paneBorderStyle
	if l.focused {
		borderStyle = paneBorderFocusedStyle
	}

	return borderStyle.Width(l.width - 2).Height(l.height - 2).Render(inner)
}

// ManagerAgentID is the special agent ID for the supervisor/manager.
const ManagerAgentID = "manager"

// isManagerAgent returns true if the agent is the special manager agent.
func isManagerAgent(agentID string) bool {
	return agentID == ManagerAgentID
}

// renderAgent renders a single agent row.
func (l AgentList) renderAgent(index int, agent daemon.AgentStatus, width int) string {
	isSelected := index == l.selected

	// Get row style based on selection
	rowStyle := agentRowStyle
	if isSelected {
		rowStyle = agentRowSelectedStyle
	}

	// State indicator with color - inherit background from row style
	stateIcon := l.stateIcon(agent.ID, agent.State)
	stateStyle := l.stateStyle(agent.ID, agent.State).Inherit(rowStyle)
	stateStr := stateStyle.Render(stateIcon)

	// Agent ID - use special style for manager
	idStyle := agentIDStyle
	if isManagerAgent(agent.ID) {
		idStyle = agentManagerIDStyle
	}
	idStr := idStyle.Inherit(rowStyle).Render(agent.ID)

	// Project name - inherit background from row style
	projectStr := agentProjectStyle.Inherit(rowStyle).Render(agent.Project)

	// Task (if any) - inherit background from row style
	taskStr := ""
	if agent.Task != "" {
		taskStr = agentTaskStyle.Inherit(rowStyle).Render(agent.Task)
	}

	// Duration since started - inherit background from row style
	duration := time.Since(agent.StartedAt).Truncate(time.Second)
	durationStr := agentDurationStyle.Inherit(rowStyle).Render(formatDuration(duration))

	// Compose the left part (without description first)
	left := lipgloss.JoinHorizontal(lipgloss.Center,
		stateStr, " ",
		idStr, " ",
		projectStr,
	)
	if taskStr != "" {
		left = lipgloss.JoinHorizontal(lipgloss.Center, left, " ", taskStr)
	}

	// Calculate available width for description and add it if present
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(durationStr)
	// Available content width is total width minus padding (1 on each side = 2)
	contentWidth := width - 2
	// Reserve space for: left content, space before desc, min spacer (1), duration
	availableForDesc := contentWidth - leftWidth - rightWidth - 1 - 1 // -1 for space before desc, -1 for min spacer
	if agent.Description != "" && availableForDesc > 3 {
		desc := truncateDescription(agent.Description, availableForDesc)
		descStr := agentDescriptionStyle.Inherit(rowStyle).Render(desc)
		left = lipgloss.JoinHorizontal(lipgloss.Center, left, " ", descStr)
		leftWidth = lipgloss.Width(left)
	}

	// Right-align duration - the spacer needs the row background too
	// Ensure spacer width never makes total content exceed available width
	spacerWidth := contentWidth - leftWidth - rightWidth
	if spacerWidth < 1 {
		spacerWidth = 1
	}

	// Build row content, ensuring it fits within contentWidth
	spacer := rowStyle.Render(strings.Repeat(" ", spacerWidth))
	row := left + spacer + durationStr

	// If row is too wide (edge case), truncate and re-render
	rowWidth := lipgloss.Width(row)
	if rowWidth > contentWidth && contentWidth > rightWidth+1 {
		// Truncate left portion to make room
		maxLeftWidth := contentWidth - rightWidth - 1
		// Re-render with truncated content - just use the duration
		spacerWidth = contentWidth - leftWidth - rightWidth
		if spacerWidth < 1 || leftWidth > maxLeftWidth {
			spacerWidth = 1
			left = rowStyle.Render(strings.Repeat(" ", maxLeftWidth-1))
		}
		spacer = rowStyle.Render(strings.Repeat(" ", spacerWidth))
		row = left + spacer + durationStr
	}

	// Apply row styling with full width
	return rowStyle.Width(width).Render(row)
}

// stateIcon returns an icon for the agent state.
func (l AgentList) stateIcon(agentID, state string) string {
	// Check if agent needs user attention (pending permission/action)
	if l.needsAttention[agentID] {
		return "!"
	}

	switch state {
	case "starting", "running":
		// Animated spinner for active states
		return spinnerFrames[l.spinnerFrame%len(spinnerFrames)]
	case "idle":
		return "○"
	case "done":
		return "✓"
	case "error":
		return "✗"
	default:
		return "?"
	}
}

// stateStyle returns the style for a state indicator.
func (l AgentList) stateStyle(agentID, state string) lipgloss.Style {
	// Attention-grabbing style for agents needing approval
	if l.needsAttention[agentID] {
		return lipgloss.NewStyle().Foreground(warningColor).Bold(true)
	}

	switch state {
	case "starting":
		return lipgloss.NewStyle().Foreground(mutedColor)
	case "running":
		return lipgloss.NewStyle().Foreground(secondaryColor)
	case "idle":
		return lipgloss.NewStyle().Foreground(mutedColor)
	case "done":
		return lipgloss.NewStyle().Foreground(secondaryColor)
	case "error":
		return lipgloss.NewStyle().Foreground(errorColor)
	default:
		return lipgloss.NewStyle().Foreground(mutedColor)
	}
}

// formatDuration formats a duration in a human-friendly way.
// Output is kept concise to prevent line wrapping in the agent list.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := int(d.Hours())
	if hours < 24 {
		return fmt.Sprintf("%dh%dm", hours, int(d.Minutes())%60)
	}
	// For durations >= 24h, show days to keep it concise
	days := hours / 24
	remainingHours := hours % 24
	return fmt.Sprintf("%dd%dh", days, remainingHours)
}

// truncateDescription truncates a description to fit within maxLen characters.
func truncateDescription(desc string, maxLen int) string {
	// Replace newlines with spaces for single-line display
	desc = strings.ReplaceAll(desc, "\n", " ")
	desc = strings.TrimSpace(desc)

	if maxLen < 10 {
		maxLen = 10
	}
	if len(desc) > maxLen {
		return desc[:maxLen-3] + "..."
	}
	return desc
}
