package tui

import (
	"fmt"
	"time"

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

	// Usage tracking
	usagePercent  int           // 0-100+ percentage of limit
	timeRemaining time.Duration // time remaining in billing window
	hasUsage      bool          // whether usage data is available
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
	case ConnectionDisconnected:
		connStatus = headerConnDisconnectedStyle.Render(" ● disconnected")
	case ConnectionReconnecting:
		connStatus = headerConnReconnectingStyle.Render(" ◌ reconnecting...")
	}

	// Right side: stats (agent counts + usage)
	var statsParts []string

	// Agent running count (only show if connected)
	if h.agentCount > 0 && h.connState == ConnectionConnected {
		statsParts = append(statsParts, fmt.Sprintf("%d/%d running", h.runningCount, h.agentCount))
	}

	// Usage percentage and time remaining
	if h.hasUsage {
		remaining := formatDuration(h.timeRemaining)
		statsParts = append(statsParts, fmt.Sprintf("%d%% (%s)", h.usagePercent, remaining))
	}

	var stats string
	if len(statsParts) > 0 {
		stats = headerStatsStyle.Render(fmt.Sprintf("  %s", join(statsParts, "  •  ")))
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

// join concatenates strings with a separator.
func join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	result := parts[0]
	for i := 1; i < len(parts); i++ {
		result += sep + parts[i]
	}
	return result
}
