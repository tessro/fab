package tui

import (
	"fmt"

	"github.com/charmbracelet/lipgloss"
)

// Header displays the fab TUI header with branding and status info.
type Header struct {
	width int

	// Stats to display
	agentCount   int
	runningCount int

	// Connection state
	connState ConnectionState
}

// NewHeader creates a new header component.
func NewHeader() Header {
	return Header{
		connState: ConnectionConnected,
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
func (h *Header) SetConnectionState(state ConnectionState) {
	h.connState = state
}

// View renders the header.
func (h Header) View() string {
	// Left side: branding
	brand := headerBrandStyle.Render("🚌 fab")

	// Connection status indicator (shown when not connected)
	var connStatus string
	switch h.connState {
	case ConnectionDisconnected:
		connStatus = headerConnDisconnectedStyle.Render(" ● disconnected")
	case ConnectionReconnecting:
		connStatus = headerConnReconnectingStyle.Render(" ◌ reconnecting...")
	}

	// Right side: agent stats (only show if we have agents and connected)
	var stats string
	if h.agentCount > 0 && h.connState == ConnectionConnected {
		stats = headerStatsStyle.Render(
			fmt.Sprintf("%d/%d running", h.runningCount, h.agentCount),
		)
	}

	// Calculate spacing between brand+status and stats
	brandWidth := lipgloss.Width(brand)
	connStatusWidth := lipgloss.Width(connStatus)
	statsWidth := lipgloss.Width(stats)
	spacerWidth := h.width - brandWidth - connStatusWidth - statsWidth
	if spacerWidth < 0 {
		spacerWidth = 0
	}
	spacer := lipgloss.NewStyle().Width(spacerWidth).Render("")

	// Combine into full-width header
	content := lipgloss.JoinHorizontal(lipgloss.Top, brand, connStatus, spacer, stats)

	return headerContainerStyle.Width(h.width).Render(content)
}
