package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"

	"github.com/tessro/fab/internal/daemon"
)

// ChatView displays chat entries for a selected agent in a conversational format.
type ChatView struct {
	entries           []daemon.ChatEntryDTO
	width             int
	height            int
	focused           bool
	agentID           string
	project           string
	viewport          viewport.Model
	ready             bool
	pendingAction     *daemon.StagedAction      // pending action awaiting approval
	pendingPermission *daemon.PermissionRequest // pending permission request
	inputView         string                    // rendered input line view
	inputHeight       int                       // height of input line (for layout)
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
	v.updateViewportSize()
}

// updateViewportSize recalculates viewport dimensions based on current state.
func (v *ChatView) updateViewportSize() {
	// Account for border (2 chars top/bottom, 2 chars left/right) and header (1 line)
	contentWidth := v.width - 2
	contentHeight := v.height - 2 - 1 // -1 for header

	// Reserve space for pending action bar if present
	if v.pendingAction != nil {
		contentHeight -= 2 // 1 line for content + 1 line padding
	}

	// Reserve space for pending permission request if present
	if v.pendingPermission != nil {
		contentHeight -= 2 // 1 line for content + 1 line padding
	}

	// Reserve space for input line
	if v.inputHeight > 0 {
		contentHeight -= v.inputHeight
	}

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

// SetPendingAction sets the pending action for this chat view.
func (v *ChatView) SetPendingAction(action *daemon.StagedAction) {
	hadAction := v.pendingAction != nil
	hasAction := action != nil
	v.pendingAction = action
	// Recalculate viewport size if pending action state changed
	if hadAction != hasAction {
		v.updateViewportSize()
	}
}

// HasPendingAction returns whether there's a pending action.
func (v *ChatView) HasPendingAction() bool {
	return v.pendingAction != nil
}

// SetPendingPermission sets the pending permission request for this chat view.
func (v *ChatView) SetPendingPermission(req *daemon.PermissionRequest) {
	hadPermission := v.pendingPermission != nil
	hasPermission := req != nil
	v.pendingPermission = req
	// Recalculate viewport size if pending permission state changed
	if hadPermission != hasPermission {
		v.updateViewportSize()
	}
}

// HasPendingPermission returns whether there's a pending permission request.
func (v *ChatView) HasPendingPermission() bool {
	return v.pendingPermission != nil
}

// PendingPermissionID returns the ID of the pending permission request, or empty string.
func (v *ChatView) PendingPermissionID() string {
	if v.pendingPermission == nil {
		return ""
	}
	return v.pendingPermission.ID
}

// SetInputView sets the rendered input line view to display.
func (v *ChatView) SetInputView(view string, height int) {
	v.inputView = view
	v.inputHeight = height
}

// AppendEntry adds a chat entry to the view.
func (v *ChatView) AppendEntry(entry daemon.ChatEntryDTO) {
	// Capture scroll position before updating content
	wasAtBottom := v.viewport.AtBottom() || v.viewport.YOffset >= v.viewport.TotalLineCount()-v.viewport.Height-5

	v.entries = append(v.entries, entry)

	// Cap at max entries to prevent unbounded growth
	const maxEntries = 1000
	if len(v.entries) > maxEntries {
		v.entries = v.entries[len(v.entries)-maxEntries:]
	}

	v.updateContent()

	// Auto-scroll to bottom if we were at/near the bottom
	if wasAtBottom {
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
	v.viewport.ScrollUp(n)
}

// ScrollDown scrolls the viewport down.
func (v *ChatView) ScrollDown(n int) {
	v.viewport.ScrollDown(n)
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
	v.viewport.PageUp()
}

// PageDown scrolls down by one page.
func (v *ChatView) PageDown() {
	v.viewport.PageDown()
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
	// Calculate available width for content (viewport width minus some padding)
	contentWidth := v.viewport.Width - 2
	if contentWidth < 20 {
		contentWidth = 20
	}

	switch entry.Role {
	case "assistant":
		prefix := "Claude: "
		prefixLen := len(prefix)
		// Wrap content, accounting for prefix on first line
		wrapped := wrapText(entry.Content, contentWidth-prefixLen, prefixLen)
		return chatAssistantStyle.Render(prefix) + wrapped

	case "user":
		prefix := "You: "
		prefixLen := len(prefix)
		// Wrap content, accounting for prefix on first line
		wrapped := wrapText(entry.Content, contentWidth-prefixLen, prefixLen)
		return chatUserStyle.Render(prefix) + wrapped

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

// wrapText wraps text to the given width with optional indentation for continuation lines.
func wrapText(text string, width, indent int) string {
	if width <= 0 {
		return text
	}

	// Wrap to available width
	wrapped := wordwrap.String(text, width)

	// If no indent needed, return as-is
	if indent <= 0 {
		return wrapped
	}

	// Add indent to continuation lines
	indentStr := strings.Repeat(" ", indent)
	lines := strings.Split(wrapped, "\n")
	for i := 1; i < len(lines); i++ {
		lines[i] = indentStr + lines[i]
	}
	return strings.Join(lines, "\n")
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
	emptyHeight := v.height - 3
	if v.pendingAction != nil {
		emptyHeight -= 2
	}
	if v.pendingPermission != nil {
		emptyHeight -= 2
	}
	if v.inputHeight > 0 {
		emptyHeight -= v.inputHeight
	}
	if len(v.entries) == 0 {
		content = ptyEmptyStyle.Width(v.width - 2).Height(emptyHeight).Render("Waiting for messages...")
	} else {
		content = v.viewport.View()
	}

	// Build the inner content
	parts := []string{header, content}

	// Add pending permission bar if present (takes priority over action)
	if v.pendingPermission != nil {
		parts = append(parts, v.renderPendingPermission())
	} else if v.pendingAction != nil {
		// Add pending action bar if present
		parts = append(parts, v.renderPendingAction())
	}

	// Add input line if present
	if v.inputView != "" {
		parts = append(parts, v.inputView)
	}

	inner := lipgloss.JoinVertical(lipgloss.Left, parts...)

	// Apply border
	var borderStyle lipgloss.Style
	if v.focused {
		borderStyle = chatViewFocusedBorderStyle
	} else {
		borderStyle = chatViewBorderStyle
	}

	return borderStyle.Width(v.width - 2).Height(v.height - 2).Render(inner)
}

// renderPendingAction renders the pending action approval bar.
func (v ChatView) renderPendingAction() string {
	if v.pendingAction == nil {
		return ""
	}

	// Truncate payload for display
	payload := v.pendingAction.Payload
	maxLen := v.width - 30
	if maxLen < 20 {
		maxLen = 20
	}
	if len(payload) > maxLen {
		payload = payload[:maxLen-3] + "..."
	}

	// Replace newlines with spaces for single-line display
	payload = strings.ReplaceAll(payload, "\n", " ")

	label := pendingActionLabelStyle.Render("⏸ Pending:")
	return pendingActionStyle.Width(v.width - 4).Render(label + " " + payload)
}

// renderPendingPermission renders the pending permission request bar.
func (v ChatView) renderPendingPermission() string {
	if v.pendingPermission == nil {
		return ""
	}

	// Format tool input for display
	toolInput := string(v.pendingPermission.ToolInput)
	// Remove outer braces and quotes for cleaner display
	toolInput = strings.TrimPrefix(toolInput, "{")
	toolInput = strings.TrimSuffix(toolInput, "}")
	toolInput = strings.ReplaceAll(toolInput, "\n", " ")
	toolInput = strings.TrimSpace(toolInput)

	// Truncate for display
	maxLen := v.width - 40
	if maxLen < 20 {
		maxLen = 20
	}
	if len(toolInput) > maxLen {
		toolInput = toolInput[:maxLen-3] + "..."
	}

	label := pendingPermissionLabelStyle.Render("🔐 Permission:")
	toolName := pendingPermissionToolStyle.Render("[" + v.pendingPermission.ToolName + "]")
	return pendingPermissionStyle.Width(v.width - 4).Render(label + " " + toolName + " " + toolInput)
}
