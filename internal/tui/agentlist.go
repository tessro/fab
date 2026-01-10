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

// SetSelected sets the selection index.
func (l *AgentList) SetSelected(index int) {
	if index >= 0 && index < len(l.agents) {
		l.selected = index
	}
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
		// Column header row
		columnHeader := l.renderColumnHeader(innerWidth)
		rows = append(rows, columnHeader)
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

// ManagerAgentID is the special agent ID for the manager agent.
const ManagerAgentID = "manager"

// PlannerAgentIDPrefix is the prefix for planner agents in the agent list.
const PlannerAgentIDPrefix = "plan:"

// isManagerAgent returns true if the agent is the special manager agent.
func isManagerAgent(agentID string) bool {
	return agentID == ManagerAgentID
}

// isPlannerAgent returns true if the agent is a planner agent.
func isPlannerAgent(agentID string) bool {
	return len(agentID) > len(PlannerAgentIDPrefix) && agentID[:len(PlannerAgentIDPrefix)] == PlannerAgentIDPrefix
}

// plannerAgentID creates a TUI agent ID for a planner.
func plannerAgentID(plannerID string) string {
	return PlannerAgentIDPrefix + plannerID
}

// extractPlannerID extracts the real planner ID from a TUI agent ID.
func extractPlannerID(agentID string) string {
	if isPlannerAgent(agentID) {
		return agentID[len(PlannerAgentIDPrefix):]
	}
	return agentID
}

// renderAgent renders a single agent row.
func (l AgentList) renderAgent(index int, agent daemon.AgentStatus, width int) string {
	isSelected := index == l.selected

	// Get row style based on selection - this will be applied once at the end
	rowStyle := agentRowStyle
	if isSelected {
		rowStyle = agentRowSelectedStyle
	}

	// Create a background-only style for inheriting (no padding)
	// This ensures text elements get the correct background color without adding extra padding
	bgStyle := lipgloss.NewStyle()
	if isSelected {
		bgStyle = bgStyle.Background(lipgloss.Color("#3B3B3B"))
	}

	// State indicator with color
	stateIcon := l.stateIcon(agent.ID, agent.State)
	stateStyle := l.stateStyle(agent.ID, agent.State).Inherit(bgStyle)
	stateStr := stateStyle.Render(stateIcon)

	// Agent ID - use special style for manager and planner
	idStyle := agentIDStyle
	displayID := agent.ID
	if isManagerAgent(agent.ID) {
		idStyle = agentManagerIDStyle
	} else if isPlannerAgent(agent.ID) {
		idStyle = agentPlannerIDStyle
		displayID = extractPlannerID(agent.ID) // Show just the short ID, not the prefix
	}
	idStr := idStyle.Inherit(bgStyle).Render(displayID)

	// Project name
	projectStr := agentProjectStyle.Inherit(bgStyle).Render(agent.Project)

	// Task (if any)
	taskStr := ""
	if agent.Task != "" {
		taskStr = agentTaskStyle.Inherit(bgStyle).Render(agent.Task)
	}

	// Duration since started
	duration := time.Since(agent.StartedAt).Truncate(time.Second)
	durationStr := agentDurationStyle.Inherit(bgStyle).Render(formatDuration(duration))

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
		descStr := agentDescriptionStyle.Inherit(bgStyle).Render(desc)
		left = lipgloss.JoinHorizontal(lipgloss.Center, left, " ", descStr)
		leftWidth = lipgloss.Width(left)
	}

	// Right-align duration
	// Ensure spacer width never makes total content exceed available width
	spacerWidth := contentWidth - leftWidth - rightWidth
	if spacerWidth < 1 {
		spacerWidth = 1
	}

	// Build row content - use bgStyle for spacer to get correct background
	spacer := bgStyle.Render(strings.Repeat(" ", spacerWidth))
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
			left = bgStyle.Render(strings.Repeat(" ", maxLeftWidth-1))
		}
		spacer = bgStyle.Render(strings.Repeat(" ", spacerWidth))
		row = left + spacer + durationStr
	}

	// Apply row styling with full width - padding is applied only here
	return rowStyle.Width(width).Render(row)
}

// renderColumnHeader renders the column header row.
func (l AgentList) renderColumnHeader(width int) string {
	// Column header labels styled with muted color
	headerStyle := lipgloss.NewStyle().Foreground(mutedColor)

	// Build header: " " (state placeholder) | AGENT | PROJECT
	stateHeader := headerStyle.Render(" ") // Single space placeholder for state icon
	agentHeader := headerStyle.Render("AGENT")
	projectHeader := headerStyle.Render("PROJECT")
	timeHeader := headerStyle.Render("TIME")

	// Compose left part
	left := lipgloss.JoinHorizontal(lipgloss.Center,
		stateHeader, " ",
		agentHeader, " ",
		projectHeader,
	)

	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(timeHeader)
	// Available content width is total width minus padding (1 on each side = 2)
	contentWidth := width - 2

	// Right-align time header
	spacerWidth := contentWidth - leftWidth - rightWidth
	if spacerWidth < 1 {
		spacerWidth = 1
	}

	spacer := strings.Repeat(" ", spacerWidth)
	row := left + spacer + timeHeader

	// Apply row styling with underline to separate from data rows
	return agentRowStyle.Width(width).Render(row)
}

// stateIcon returns an icon for the agent state.
func (l AgentList) stateIcon(agentID, state string) string {
	// Check if agent needs user attention (pending permission/action)
	if l.needsAttention[agentID] {
		return "!"
	}

	switch state {
	case "starting":
		// Animated spinner for starting state
		return spinnerFrames[l.spinnerFrame%len(spinnerFrames)]
	case "running":
		// Manager and planner agents don't spin when running - they're idle waiting for input
		if isManagerAgent(agentID) || isPlannerAgent(agentID) {
			return "○"
		}
		// Animated spinner for active agents
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
