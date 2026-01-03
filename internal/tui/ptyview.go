package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
)

// PTYView displays scrollable PTY output for a selected agent.
type PTYView struct {
	width    int
	height   int
	viewport viewport.Model
	agentID  string
	project  string
	lines    []string
	focused  bool
	ready    bool
}

// NewPTYView creates a new PTY view component.
func NewPTYView() PTYView {
	return PTYView{
		lines: make([]string, 0),
	}
}

// SetSize updates the component dimensions.
func (v *PTYView) SetSize(width, height int) {
	v.width = width
	v.height = height

	headerHeight := 1 // Agent ID header
	contentHeight := height - headerHeight
	if contentHeight < 1 {
		contentHeight = 1
	}

	if !v.ready {
		v.viewport = viewport.New(width-2, contentHeight) // -2 for border padding
		v.viewport.Style = ptyViewportStyle
		v.ready = true
	} else {
		v.viewport.Width = width - 2
		v.viewport.Height = contentHeight
	}

	v.updateContent()
}

// SetAgent sets the currently viewed agent.
func (v *PTYView) SetAgent(agentID, project string) {
	if v.agentID != agentID {
		v.agentID = agentID
		v.project = project
		v.lines = make([]string, 0)
		v.updateContent()
	}
}

// ClearAgent clears the current agent view.
func (v *PTYView) ClearAgent() {
	v.agentID = ""
	v.project = ""
	v.lines = make([]string, 0)
	v.updateContent()
}

// AgentID returns the current agent ID.
func (v *PTYView) AgentID() string {
	return v.agentID
}

// AppendOutput adds output data to the view.
func (v *PTYView) AppendOutput(data string) {
	if data == "" {
		return
	}

	// Split incoming data by newlines
	newLines := strings.Split(data, "\n")
	v.lines = append(v.lines, newLines...)

	// Cap at max lines to prevent unbounded growth
	const maxLines = 10000
	if len(v.lines) > maxLines {
		v.lines = v.lines[len(v.lines)-maxLines:]
	}

	v.updateContent()

	// Auto-scroll to bottom if near the end
	if v.viewport.AtBottom() || v.viewport.YOffset >= v.viewport.TotalLineCount()-v.viewport.Height-5 {
		v.viewport.GotoBottom()
	}
}

// SetFocused sets the focus state.
func (v *PTYView) SetFocused(focused bool) {
	v.focused = focused
}

// IsFocused returns whether the view is focused.
func (v *PTYView) IsFocused() bool {
	return v.focused
}

// ScrollUp scrolls the viewport up.
func (v *PTYView) ScrollUp(lines int) {
	v.viewport.LineUp(lines)
}

// ScrollDown scrolls the viewport down.
func (v *PTYView) ScrollDown(lines int) {
	v.viewport.LineDown(lines)
}

// ScrollToTop scrolls to the top.
func (v *PTYView) ScrollToTop() {
	v.viewport.GotoTop()
}

// ScrollToBottom scrolls to the bottom.
func (v *PTYView) ScrollToBottom() {
	v.viewport.GotoBottom()
}

// PageUp scrolls up by one page.
func (v *PTYView) PageUp() {
	v.viewport.ViewUp()
}

// PageDown scrolls down by one page.
func (v *PTYView) PageDown() {
	v.viewport.ViewDown()
}

// updateContent refreshes the viewport content.
func (v *PTYView) updateContent() {
	if !v.ready {
		return
	}
	content := strings.Join(v.lines, "\n")
	v.viewport.SetContent(content)
}

// View renders the PTY view.
func (v PTYView) View() string {
	if v.agentID == "" {
		empty := ptyEmptyStyle.Width(v.width).Height(v.height).Render("Select an agent to view output")
		return empty
	}

	// Header showing agent info
	headerText := lipgloss.JoinHorizontal(lipgloss.Center,
		ptyHeaderAgentStyle.Render(v.agentID),
		" ",
		ptyHeaderProjectStyle.Render(v.project),
	)

	var header string
	if v.focused {
		header = ptyHeaderFocusedStyle.Width(v.width).Render(headerText)
	} else {
		header = ptyHeaderStyle.Width(v.width).Render(headerText)
	}

	// Viewport content
	var content string
	if len(v.lines) == 0 {
		content = ptyEmptyStyle.Width(v.width).Height(v.height-1).Render("Waiting for output...")
	} else {
		content = v.viewport.View()
	}

	return lipgloss.JoinVertical(lipgloss.Left, header, content)
}
