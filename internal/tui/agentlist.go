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
	width        int
	height       int
	agents       []daemon.AgentStatus
	selected     int
	spinnerFrame int
}

// NewAgentList creates a new agent list component.
func NewAgentList() AgentList {
	return AgentList{
		selected: 0,
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

// View renders the agent list.
func (l AgentList) View() string {
	if len(l.agents) == 0 {
		return agentListEmptyStyle.Width(l.width).Height(l.height).Render("No agents")
	}

	var rows []string
	for i, agent := range l.agents {
		row := l.renderAgent(i, agent)
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	return agentListContainerStyle.Width(l.width).Height(l.height).Render(content)
}

// renderAgent renders a single agent row.
func (l AgentList) renderAgent(index int, agent daemon.AgentStatus) string {
	isSelected := index == l.selected

	// State indicator with color
	stateIcon := l.stateIcon(agent.State)
	stateStyle := l.stateStyle(agent.State)
	stateStr := stateStyle.Render(stateIcon)

	// Agent ID
	idStr := agentIDStyle.Render(agent.ID)

	// Project name
	projectStr := agentProjectStyle.Render(agent.Project)

	// Task (if any)
	taskStr := ""
	if agent.Task != "" {
		taskStr = agentTaskStyle.Render(agent.Task)
	}

	// Duration since started
	duration := time.Since(agent.StartedAt).Truncate(time.Second)
	durationStr := agentDurationStyle.Render(formatDuration(duration))

	// Compose the row
	left := lipgloss.JoinHorizontal(lipgloss.Center,
		stateStr, " ",
		idStr, " ",
		projectStr,
	)
	if taskStr != "" {
		left = lipgloss.JoinHorizontal(lipgloss.Center, left, " ", taskStr)
	}

	// Right-align duration
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(durationStr)
	spacerWidth := l.width - leftWidth - rightWidth - 4 // padding
	if spacerWidth < 1 {
		spacerWidth = 1
	}
	spacer := strings.Repeat(" ", spacerWidth)

	row := left + spacer + durationStr

	// Apply selection styling
	if isSelected {
		return agentRowSelectedStyle.Width(l.width).Render(row)
	}
	return agentRowStyle.Width(l.width).Render(row)
}

// stateIcon returns an icon for the agent state.
func (l AgentList) stateIcon(state string) string {
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
func (l AgentList) stateStyle(state string) lipgloss.Style {
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
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
