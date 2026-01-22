package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Header displays the fab TUI header with branding and status info.
type Header struct {
	width int

	// Agent stats
	agentCount   int
	runningCount int

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
