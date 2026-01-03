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
}

// NewHeader creates a new header component.
func NewHeader() Header {
	return Header{}
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

// View renders the header.
func (h Header) View() string {
	// Left side: branding
	brand := headerBrandStyle.Render("🚌 fab")

	// Right side: agent stats (only show if we have agents)
	var stats string
	if h.agentCount > 0 {
		stats = headerStatsStyle.Render(
			fmt.Sprintf("%d/%d running", h.runningCount, h.agentCount),
		)
	}

	// Calculate spacing between brand and stats
	brandWidth := lipgloss.Width(brand)
	statsWidth := lipgloss.Width(stats)
	spacerWidth := h.width - brandWidth - statsWidth
	if spacerWidth < 0 {
		spacerWidth = 0
	}
	spacer := lipgloss.NewStyle().Width(spacerWidth).Render("")

	// Combine into full-width header
	content := lipgloss.JoinHorizontal(lipgloss.Top, brand, spacer, stats)

	return headerContainerStyle.Width(h.width).Render(content)
}
