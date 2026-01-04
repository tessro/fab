package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/tessro/fab/internal/daemon"
)

// ChatView displays chat entries for a selected agent in a conversational format.
type ChatView struct {
	entries  []daemon.ChatEntryDTO
	width    int
	height   int
	scroll   int // current scroll offset (legacy, viewport handles this)
	focused  bool
	agentID  string
	project  string
	viewport viewport.Model
	ready    bool
}

// NewChatView creates a new chat view component.
func NewChatView() ChatView {
	return ChatView{
		entries: make([]daemon.ChatEntryDTO, 0),
	}
}

// SetSize updates the component dimensions.
func (v *ChatView) SetSize(width, height int) {
	v.width = width
	v.height = height

	// Account for border (2 chars top/bottom, 2 chars left/right)
	contentWidth := width - 2
	contentHeight := height - 2
	if contentWidth < 1 {
		contentWidth = 1
	}
	if contentHeight < 1 {
		contentHeight = 1
	}

	if !v.ready {
		v.viewport = viewport.New(contentWidth, contentHeight)
		v.ready = true
	} else {
		v.viewport.Width = contentWidth
		v.viewport.Height = contentHeight
	}

	v.updateContent()
}

// SetFocused sets the focus state.
func (v *ChatView) SetFocused(focused bool) {
	v.focused = focused
}

// IsFocused returns whether the view is focused.
func (v *ChatView) IsFocused() bool {
	return v.focused
}

// SetAgent sets the currently viewed agent.
func (v *ChatView) SetAgent(agentID, project string) {
	if v.agentID != agentID {
		v.agentID = agentID
		v.project = project
		v.entries = make([]daemon.ChatEntryDTO, 0)
		v.updateContent()
	}
}

// ClearAgent clears the current agent view.
func (v *ChatView) ClearAgent() {
	v.agentID = ""
	v.project = ""
	v.entries = make([]daemon.ChatEntryDTO, 0)
	v.updateContent()
}

// AgentID returns the current agent ID.
func (v *ChatView) AgentID() string {
	return v.agentID
}

// AppendEntry adds a chat entry to the view.
func (v *ChatView) AppendEntry(entry daemon.ChatEntryDTO) {
	v.entries = append(v.entries, entry)

	// Cap at max entries to prevent unbounded growth
	const maxEntries = 1000
	if len(v.entries) > maxEntries {
		v.entries = v.entries[len(v.entries)-maxEntries:]
	}

	v.updateContent()

	// Auto-scroll to bottom if near the end
	if v.viewport.AtBottom() || v.viewport.YOffset >= v.viewport.TotalLineCount()-v.viewport.Height-5 {
		v.viewport.GotoBottom()
	}
}

// SetEntries replaces all entries in the view.
func (v *ChatView) SetEntries(entries []daemon.ChatEntryDTO) {
	v.entries = entries
	v.updateContent()
	v.viewport.GotoBottom()
}

// ScrollUp scrolls the viewport up.
func (v *ChatView) ScrollUp(n int) {
	v.viewport.LineUp(n)
}

// ScrollDown scrolls the viewport down.
func (v *ChatView) ScrollDown(n int) {
	v.viewport.LineDown(n)
}

// ScrollToTop scrolls to the top.
func (v *ChatView) ScrollToTop() {
	v.viewport.GotoTop()
}

// ScrollToBottom scrolls to the bottom.
func (v *ChatView) ScrollToBottom() {
	v.viewport.GotoBottom()
}

// PageUp scrolls up by one page.
func (v *ChatView) PageUp() {
	v.viewport.ViewUp()
}

// PageDown scrolls down by one page.
func (v *ChatView) PageDown() {
	v.viewport.ViewDown()
}

// updateContent refreshes the viewport content from entries.
func (v *ChatView) updateContent() {
	if !v.ready {
		return
	}

	var lines []string
	for _, entry := range v.entries {
		rendered := v.renderEntry(entry)
		lines = append(lines, rendered)
	}

	content := strings.Join(lines, "\n\n")
	v.viewport.SetContent(content)
}

// renderEntry renders a single chat entry to a string.
func (v *ChatView) renderEntry(entry daemon.ChatEntryDTO) string {
	switch entry.Role {
	case "assistant":
		return chatAssistantStyle.Render("Claude: ") + entry.Content

	case "user":
		return chatUserStyle.Render("You: ") + entry.Content

	case "tool":
		var parts []string

		// Tool invocation line
		toolLine := "  " + chatToolStyle.Render("["+entry.ToolName+"]") + " " + truncateToolInput(entry.ToolInput)
		parts = append(parts, toolLine)

		// Tool result (if present)
		if entry.ToolResult != "" {
			resultLine := "  " + chatResultStyle.Render("->") + " " + truncateResult(entry.ToolResult, v.width-6)
			parts = append(parts, resultLine)
		}

		return strings.Join(parts, "\n")

	default:
		return entry.Content
	}
}

// truncateToolInput truncates tool input for display.
func truncateToolInput(input string) string {
	// Remove newlines for single-line display
	input = strings.ReplaceAll(input, "\n", " ")
	input = strings.TrimSpace(input)

	const maxLen = 80
	if len(input) > maxLen {
		return input[:maxLen-3] + "..."
	}
	return input
}

// truncateResult truncates tool result for display.
func truncateResult(result string, maxWidth int) string {
	lines := strings.Split(result, "\n")

	// Show first few lines
	const maxLines = 5
	if len(lines) > maxLines {
		lines = lines[:maxLines]
		lines = append(lines, "...")
	}

	// Truncate each line
	for i, line := range lines {
		if len(line) > maxWidth {
			lines[i] = line[:maxWidth-3] + "..."
		}
	}

	// Join with indentation for continuation lines
	var parts []string
	for i, line := range lines {
		if i == 0 {
			parts = append(parts, line)
		} else {
			parts = append(parts, "     "+line)
		}
	}

	return strings.Join(parts, "\n")
}

// View renders the chat view.
func (v ChatView) View() string {
	if v.agentID == "" {
		empty := ptyEmptyStyle.Width(v.width).Height(v.height).Render("Select an agent to view chat")
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
		header = ptyHeaderFocusedStyle.Width(v.width - 2).Render(headerText)
	} else {
		header = ptyHeaderStyle.Width(v.width - 2).Render(headerText)
	}

	// Viewport content
	var content string
	if len(v.entries) == 0 {
		content = ptyEmptyStyle.Width(v.width - 2).Height(v.height - 3).Render("Waiting for messages...")
	} else {
		content = v.viewport.View()
	}

	// Combine header and content
	inner := lipgloss.JoinVertical(lipgloss.Left, header, content)

	// Apply border
	var borderStyle lipgloss.Style
	if v.focused {
		borderStyle = chatViewFocusedBorderStyle
	} else {
		borderStyle = chatViewBorderStyle
	}

	return borderStyle.Width(v.width - 2).Height(v.height - 2).Render(inner)
}
