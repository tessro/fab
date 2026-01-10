package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Header displays the fab TUI header with branding and status info.
type Header struct {
	width int

	// Agent stats
	agentCount   int
	runningCount int

	// Session stats
	commitCount int

	// Usage stats
	usagePercent  int
	timeRemaining time.Duration
	hasUsage      bool

	// Connection state
	connState connectionState
}

// NewHeader creates a new header component.
func NewHeader() Header {
	return Header{
		connState: connectionConnected,
	}
}

// SetWidth updates the header width.
func (h *Header) SetWidth(width int) {
	h.width = width
}

// SetAgentCounts updates the agent statistics.
func (h *Header) SetAgentCounts(total, running int) {
	h.agentCount = total
	h.runningCount = running
}

// SetConnectionState updates the connection state display.
func (h *Header) SetConnectionState(state connectionState) {
	h.connState = state
}

// SetCommitCount updates the session commit count.
func (h *Header) SetCommitCount(count int) {
	h.commitCount = count
}

// SetUsage updates the usage display.
func (h *Header) SetUsage(percent int, remaining time.Duration) {
	h.usagePercent = percent
	h.timeRemaining = remaining
	h.hasUsage = true
}

// View renders the header.
func (h Header) View() string {
	// Left side: branding
	brand := headerBrandStyle.Render("🚌 fab")

	// Connection status indicator (shown when not connected)
	var connStatus string
	switch h.connState {
	case connectionDisconnected:
		connStatus = headerConnDisconnectedStyle.Render(" ● disconnected")
	case connectionReconnecting:
		connStatus = headerConnReconnectingStyle.Render(" ◌ reconnecting...")
	}

	// Agent stats section (only show if we have agents and connected)
	var agentStats string
	if h.agentCount > 0 && h.connState == connectionConnected {
		agentStats = headerStatsStyle.Render(
			fmt.Sprintf("%d/%d running", h.runningCount, h.agentCount),
		)
	}

	// Session stats section (commits)
	var sessionStats string
	if h.commitCount > 0 && h.connState == connectionConnected {
		sessionStats = headerStatsStyle.Render(
			fmt.Sprintf("%d commits", h.commitCount),
		)
	}

	// Usage meter section (only show if we have usage data)
	var usageMeter string
	if h.hasUsage && h.connState == connectionConnected {
		usageMeter = h.renderUsageMeter()
	}

	// Build the sections with separators
	var sections []string
	sections = append(sections, brand)
	if connStatus != "" {
		sections = append(sections, connStatus)
	}

	// Collect right-side stats
	var rightStats []string
	if agentStats != "" {
		rightStats = append(rightStats, agentStats)
	}
	if sessionStats != "" {
		rightStats = append(rightStats, sessionStats)
	}
	if usageMeter != "" {
		rightStats = append(rightStats, usageMeter)
	}

	// Calculate widths
	leftWidth := lipgloss.Width(strings.Join(sections, ""))
	rightContent := strings.Join(rightStats, headerSeparatorStyle.Render(" │ "))
	rightWidth := lipgloss.Width(rightContent)

	spacerWidth := h.width - leftWidth - rightWidth
	if spacerWidth < 0 {
		spacerWidth = 0
	}
	spacer := lipgloss.NewStyle().Width(spacerWidth).Render("")

	// Combine into full-width header
	content := lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(sections, ""), spacer, rightContent)

	return headerContainerStyle.Width(h.width).Render(content)
}

// renderUsageMeter renders the usage progress bar.
func (h Header) renderUsageMeter() string {
	// Progress bar: 10 segments
	const barLen = 10
	filled := h.usagePercent * barLen / 100
	if filled > barLen {
		filled = barLen
	}
	if filled < 0 {
		filled = 0
	}

	bar := strings.Repeat("█", filled) + strings.Repeat("░", barLen-filled)

	// Color based on usage level
	var barStyle lipgloss.Style
	switch {
	case h.usagePercent >= 90:
		barStyle = headerUsageHighStyle
	case h.usagePercent >= 70:
		barStyle = headerUsageMediumStyle
	default:
		barStyle = headerUsageLowStyle
	}

	// Format: "Usage: ████░░░░░░ 45% (2h 15m)"
	percentStr := fmt.Sprintf("%d%%", h.usagePercent)
	timeStr := ""
	if h.timeRemaining > 0 {
		timeStr = fmt.Sprintf(" (%s)", formatDuration(h.timeRemaining))
	}

	return headerStatsStyle.Render("Usage: ") +
		barStyle.Render(bar) +
		headerStatsStyle.Render(" "+percentStr+timeStr)
}
