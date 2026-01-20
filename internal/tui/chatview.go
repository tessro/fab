package tui

import (
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/reflow/wordwrap"

	"github.com/tessro/fab/internal/daemon"
)

// ChatView displays chat entries for a selected agent in a conversational format.
type ChatView struct {
	entries             []daemon.ChatEntryDTO
	width               int
	height              int
	focused             bool
	agentID             string
	project             string
	backend             string // CLI backend name (e.g., "claude", "codex")
	viewport            viewport.Model
	ready               bool
	pendingPermission   *daemon.PermissionRequest // pending permission request
	pendingUserQuestion *daemon.UserQuestion      // pending user question
	questionSelected    int                       // index of selected option (0-based, per question)
	questionIndex       int                       // which question we're on (for multi-question)
	inputView           string                    // rendered input line view
	inputHeight         int                       // height of input line (for layout)
	inputFocused        bool                      // whether input line is focused (input mode)
	abortConfirming     bool                      // awaiting abort confirmation
	abortAgentID        string                    // agent being aborted

	// Plan mode state
	planProjectSelect bool     // in plan project selection mode
	planProjects      []string // list of available projects
	planProjectIndex  int      // selected project index
	planProjectFilter string   // current filter text for fuzzy matching
	planPromptMode    bool     // in plan prompt mode
	planPromptProject string   // project for plan prompt
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

	// Reserve space for pending permission request if present
	if v.pendingPermission != nil {
		contentHeight -= 2 // 1 line for content + 1 line padding
	}

	// Reserve space for pending user question if present
	if v.pendingUserQuestion != nil {
		// Calculate height based on number of options
		questionHeight := v.calculateUserQuestionHeight()
		contentHeight -= questionHeight
	}

	// Reserve space for abort confirmation bar if present
	if v.abortConfirming {
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
func (v *ChatView) SetAgent(agentID, project, backend string) {
	if v.agentID != agentID {
		v.agentID = agentID
		v.project = project
		v.backend = backend
		v.entries = make([]daemon.ChatEntryDTO, 0)
		v.updateContent()
	}
}

// ClearAgent clears the current agent view.
func (v *ChatView) ClearAgent() {
	v.agentID = ""
	v.project = ""
	v.backend = ""
	v.entries = make([]daemon.ChatEntryDTO, 0)
	v.updateContent()
}

// AgentID returns the current agent ID.
func (v *ChatView) AgentID() string {
	return v.agentID
}

// Project returns the project name of the current agent.
func (v *ChatView) Project() string {
	return v.project
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

// SetPendingUserQuestion sets the pending user question for this chat view.
func (v *ChatView) SetPendingUserQuestion(question *daemon.UserQuestion) {
	hadQuestion := v.pendingUserQuestion != nil
	hasQuestion := question != nil
	v.pendingUserQuestion = question
	// Reset selection state when question changes
	if question != nil && (v.pendingUserQuestion == nil || v.pendingUserQuestion.ID != question.ID) {
		v.questionSelected = 0
		v.questionIndex = 0
	}
	// Recalculate viewport size if pending question state changed
	if hadQuestion != hasQuestion {
		v.updateViewportSize()
	}
}

// HasPendingUserQuestion returns whether there's a pending user question.
func (v *ChatView) HasPendingUserQuestion() bool {
	return v.pendingUserQuestion != nil
}

// PendingUserQuestionID returns the ID of the pending user question, or empty string.
func (v *ChatView) PendingUserQuestionID() string {
	if v.pendingUserQuestion == nil {
		return ""
	}
	return v.pendingUserQuestion.ID
}

// QuestionMoveUp moves the selection up in the user question options.
func (v *ChatView) QuestionMoveUp() {
	if v.pendingUserQuestion == nil || v.questionIndex >= len(v.pendingUserQuestion.Questions) {
		return
	}
	options := v.pendingUserQuestion.Questions[v.questionIndex].Options
	if v.questionSelected > 0 {
		v.questionSelected--
	} else {
		// Wrap to bottom, including "Other" option
		v.questionSelected = len(options) // "Other" is the last option
	}
}

// QuestionMoveDown moves the selection down in the user question options.
func (v *ChatView) QuestionMoveDown() {
	if v.pendingUserQuestion == nil || v.questionIndex >= len(v.pendingUserQuestion.Questions) {
		return
	}
	options := v.pendingUserQuestion.Questions[v.questionIndex].Options
	// +1 for "Other" option
	if v.questionSelected < len(options) {
		v.questionSelected++
	} else {
		// Wrap to top
		v.questionSelected = 0
	}
}

// GetSelectedAnswer returns the selected answer for the current question.
// Returns the option label, or "" for "Other" (which requires freeform input).
func (v *ChatView) GetSelectedAnswer() (header string, label string, isOther bool) {
	if v.pendingUserQuestion == nil || v.questionIndex >= len(v.pendingUserQuestion.Questions) {
		return "", "", false
	}
	q := v.pendingUserQuestion.Questions[v.questionIndex]
	if v.questionSelected >= len(q.Options) {
		// "Other" option selected
		return q.Header, "", true
	}
	return q.Header, q.Options[v.questionSelected].Label, false
}

// calculateUserQuestionHeight calculates the height needed for the user question UI.
func (v *ChatView) calculateUserQuestionHeight() int {
	if v.pendingUserQuestion == nil || v.questionIndex >= len(v.pendingUserQuestion.Questions) {
		return 0
	}
	q := v.pendingUserQuestion.Questions[v.questionIndex]
	// 1 for question header + options count + 1 for "Other" + 1 for padding
	return 1 + len(q.Options) + 1 + 1
}

// SetInputView sets the rendered input line view to display.
func (v *ChatView) SetInputView(view string, height int, focused bool) {
	v.inputView = view
	v.inputHeight = height
	v.inputFocused = focused
}

// SetAbortConfirming sets the abort confirmation state.
func (v *ChatView) SetAbortConfirming(confirming bool, agentID string) {
	wasConfirming := v.abortConfirming
	v.abortConfirming = confirming
	v.abortAgentID = agentID
	// Recalculate viewport size if state changed
	if wasConfirming != confirming {
		v.updateViewportSize()
	}
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

// SetEntries merges historical entries with any streaming entries that may have
// arrived while the history was being fetched. This prevents a race condition
// where switching agents triggers a history fetch, but streaming events arrive
// before the history response - without merging, those streaming events would
// be lost when SetEntries replaces v.entries.
func (v *ChatView) SetEntries(entries []daemon.ChatEntryDTO) {
	if len(entries) == 0 {
		// No history to load; keep any streaming entries that arrived
		v.updateContent()
		v.viewport.GotoBottom()
		return
	}

	// Find the timestamp of the most recent history entry
	lastHistoryTS := entries[len(entries)-1].Timestamp

	// Keep any existing entries that are newer than the history
	// (these are streaming entries that arrived during the fetch)
	var streamingEntries []daemon.ChatEntryDTO
	for _, entry := range v.entries {
		if entry.Timestamp > lastHistoryTS {
			streamingEntries = append(streamingEntries, entry)
		}
	}

	// Merge: history entries + any streaming entries that arrived after
	v.entries = append(entries, streamingEntries...)
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
	var lastToolName string
	for _, entry := range v.entries {
		// Track the last seen tool name for linking tool_result entries
		if entry.Role == "tool" && entry.ToolName != "" {
			lastToolName = entry.ToolName
		}
		rendered := v.renderEntry(entry, lastToolName)
		lines = append(lines, rendered)
	}

	content := strings.Join(lines, "\n\n")
	v.viewport.SetContent(content)
}

// formatTime formats an RFC3339 timestamp as "1:23 PM" or returns empty string on error.
func formatTime(timestamp string) string {
	if timestamp == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, timestamp)
	if err != nil {
		return ""
	}
	return t.Format("3:04 PM")
}

// renderEntry renders a single chat entry to a string.
// lastToolName is the most recent tool name seen, used for tool_result entries.
func (v *ChatView) renderEntry(entry daemon.ChatEntryDTO, lastToolName string) string {
	// Calculate available width for content (viewport width minus some padding)
	contentWidth := v.viewport.Width - 2
	if contentWidth < 20 {
		contentWidth = 20
	}

	// Format the timestamp
	timeStr := formatTime(entry.Timestamp)
	var timePrefix string
	if timeStr != "" {
		timePrefix = chatTimeStyle.Render(timeStr) + " "
	}

	switch entry.Role {
	case "assistant":
		// Use backend name for prefix, capitalize first letter
		backendName := v.backend
		if backendName == "" {
			backendName = "claude"
		}
		// Capitalize first letter
		if len(backendName) > 0 {
			backendName = strings.ToUpper(backendName[:1]) + backendName[1:]
		}
		prefix := backendName + ": "
		prefixLen := len(prefix)
		if timeStr != "" {
			prefixLen += len(timeStr) + 1 // +1 for space
		}
		// Wrap content, accounting for prefix on first line
		wrapped := wrapText(entry.Content, contentWidth-prefixLen, prefixLen)
		return timePrefix + chatAssistantStyle.Render(prefix) + wrapped

	case "user":
		prefix := "You: "
		prefixLen := len(prefix)
		if timeStr != "" {
			prefixLen += len(timeStr) + 1 // +1 for space
		}
		// Wrap content, accounting for prefix on first line
		wrapped := wrapText(entry.Content, contentWidth-prefixLen, prefixLen)
		return timePrefix + chatUserStyle.Render(prefix) + wrapped

	case "tool":
		var parts []string

		// Tool invocation line (only show if we have a tool name)
		if entry.ToolName != "" {
			toolLine := "  " + chatToolStyle.Render("["+entry.ToolName+"]") + " " + truncateToolInput(entry.ToolInput)
			parts = append(parts, toolLine)
		}

		// Tool result (if present)
		if entry.ToolResult != "" {
			// Use the entry's tool name if available, otherwise use lastToolName
			toolName := entry.ToolName
			if toolName == "" {
				toolName = lastToolName
			}
			resultLine := "  " + chatResultStyle.Render("->") + " " + summarizeToolResult(toolName, entry.ToolResult, v.width-6, entry.IsError)
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

// summarizeToolResult returns a one-line summary for certain tool results.
// For Read and Grep tools, it shows line counts instead of content.
// For error results, it shows the full output to help diagnose failures.
// For other tools, it uses the existing truncateResult behavior.
func summarizeToolResult(toolName, result string, maxWidth int, isError bool) string {
	// Show full output for errors (e.g., test failures)
	if isError {
		return formatFullResult(result, maxWidth)
	}

	switch toolName {
	case "Read":
		// Count lines in the result
		lineCount := strings.Count(result, "\n")
		if !strings.HasSuffix(result, "\n") && len(result) > 0 {
			lineCount++
		}
		return "Read " + formatLineCount(lineCount)

	case "Grep":
		// Count lines (matches) in the result
		lines := strings.Split(result, "\n")
		// Filter empty lines
		matchCount := 0
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				matchCount++
			}
		}
		if matchCount == 0 {
			return "No matches"
		}
		return formatMatchCount(matchCount)

	default:
		return truncateResult(result, maxWidth)
	}
}

// formatFullResult formats a result for display without line truncation.
// Used for error output where we need to see the full content.
func formatFullResult(result string, maxWidth int) string {
	lines := strings.Split(result, "\n")

	// Truncate each line to terminal width
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

// formatLineCount formats a line count for display.
func formatLineCount(count int) string {
	if count == 1 {
		return "1 line"
	}
	return formatNumber(count) + " lines"
}

// formatMatchCount formats a match count for display.
func formatMatchCount(count int) string {
	if count == 1 {
		return "1 match"
	}
	return formatNumber(count) + " matches"
}

// formatNumber formats a number as a string.
func formatNumber(n int) string {
	return strconv.Itoa(n)
}

// View renders the chat view.
func (v ChatView) View() string {
	// Handle plan project selection mode
	if v.planProjectSelect {
		innerWidth := v.width - 2
		header := paneTitleFocusedStyle.Width(innerWidth).Render("New Plan Agent")
		content := v.renderPlanProjectSelection()
		inner := lipgloss.JoinVertical(lipgloss.Left, header, content)
		return chatViewFocusedBorderStyle.Width(v.width - 2).Height(v.height - 2).Render(inner)
	}

	// Handle plan prompt mode
	if v.planPromptMode {
		innerWidth := v.width - 2
		innerHeight := v.height - 2 - 1
		header := paneTitleFocusedStyle.Width(innerWidth).Render("New Plan Agent")
		content := v.renderPlanPromptMode()
		parts := []string{header, content}
		// Add input line with divider
		if v.inputView != "" {
			indicator := inputModeIndicatorStyle.Render(" PLAN ")
			indicatorWidth := lipgloss.Width(indicator)
			remainingWidth := innerWidth - indicatorWidth
			leftDash := inputDividerFocusedStyle.Render(strings.Repeat("─", 2))
			rightDash := inputDividerFocusedStyle.Render(strings.Repeat("─", remainingWidth-2))
			divider := leftDash + indicator + rightDash
			parts = append(parts, divider, v.inputView)
		}
		inner := lipgloss.JoinVertical(lipgloss.Left, parts...)
		// Use remaining space
		_ = innerHeight
		return chatViewFocusedBorderStyle.Width(v.width - 2).Height(v.height - 2).Render(inner)
	}

	if v.agentID == "" {
		// Show empty state with consistent border
		innerWidth := v.width - 2
		innerHeight := v.height - 2 - 1
		header := paneTitleStyle.Width(innerWidth).Render("Chat")
		content := chatEmptyStyle.Width(innerWidth).Height(innerHeight).Render("Select an agent to view chat")
		inner := lipgloss.JoinVertical(lipgloss.Left, header, content)
		return paneBorderStyle.Width(v.width - 2).Height(v.height - 2).Render(inner)
	}

	// Header showing agent info - use pane title styles for consistency
	headerText := v.agentID
	if v.project != "" {
		headerText += " · " + v.project
	}

	titleStyle := paneTitleStyle
	if v.focused {
		titleStyle = paneTitleFocusedStyle
	}
	header := titleStyle.Width(v.width - 2).Render(headerText)

	// Viewport content
	var content string
	emptyHeight := v.height - 3
	if v.pendingPermission != nil {
		emptyHeight -= 2
	}
	if v.pendingUserQuestion != nil {
		emptyHeight -= v.calculateUserQuestionHeight()
	}
	if v.abortConfirming {
		emptyHeight -= 2
	}
	if v.inputHeight > 0 {
		emptyHeight -= v.inputHeight
	}
	if len(v.entries) == 0 {
		content = chatEmptyStyle.Width(v.width - 2).Height(emptyHeight).Render("Waiting for messages...")
	} else {
		content = v.viewport.View()
	}

	// Build the inner content
	parts := []string{header, content}

	// Abort confirmation takes highest priority
	if v.abortConfirming {
		parts = append(parts, v.renderAbortConfirmation())
	} else if v.pendingUserQuestion != nil {
		// User question takes priority over permission
		parts = append(parts, v.renderPendingUserQuestion())
	} else if v.pendingPermission != nil {
		// Add pending permission bar if present
		parts = append(parts, v.renderPendingPermission())
	}

	// Add input line with divider if present
	if v.inputView != "" {
		// Draw a horizontal divider line above the input
		// When in input mode, show "INPUT" indicator prominently
		var divider string
		if v.inputFocused {
			indicator := inputModeIndicatorStyle.Render(" INPUT ")
			indicatorWidth := lipgloss.Width(indicator)
			remainingWidth := v.width - 2 - indicatorWidth
			leftDash := inputDividerFocusedStyle.Render(strings.Repeat("─", 2))
			rightDash := inputDividerFocusedStyle.Render(strings.Repeat("─", remainingWidth-2))
			divider = leftDash + indicator + rightDash
		} else {
			dividerStyle := inputDividerStyle
			if v.focused {
				dividerStyle = inputDividerFocusedStyle
			}
			divider = dividerStyle.Render(strings.Repeat("─", v.width-2))
		}
		parts = append(parts, divider, v.inputView)
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

// renderAbortConfirmation renders the abort confirmation bar.
func (v ChatView) renderAbortConfirmation() string {
	if !v.abortConfirming {
		return ""
	}

	label := abortConfirmLabelStyle.Render("⚠ Abort agent " + v.abortAgentID + "?")
	hint := abortConfirmHintStyle.Render("(y: confirm, n: cancel)")
	return abortConfirmStyle.Width(v.width - 4).Render(label + " " + hint)
}

// renderPendingUserQuestion renders the user question with selectable options.
func (v ChatView) renderPendingUserQuestion() string {
	if v.pendingUserQuestion == nil || v.questionIndex >= len(v.pendingUserQuestion.Questions) {
		return ""
	}

	q := v.pendingUserQuestion.Questions[v.questionIndex]

	var lines []string

	// Question header with icon
	headerLine := userQuestionHeaderStyle.Render("❓ " + q.Question)
	lines = append(lines, headerLine)

	// Render each option
	for i, opt := range q.Options {
		var line string
		if i == v.questionSelected {
			// Selected option
			line = userQuestionSelectedStyle.Render("▶ " + opt.Label)
			if opt.Description != "" {
				line += userQuestionDescStyle.Render(" - " + opt.Description)
			}
		} else {
			// Unselected option
			line = userQuestionOptionStyle.Render("  " + opt.Label)
			if opt.Description != "" {
				line += userQuestionDescStyle.Render(" - " + opt.Description)
			}
		}
		lines = append(lines, line)
	}

	// "Other" option (always present)
	otherIndex := len(q.Options)
	if v.questionSelected == otherIndex {
		lines = append(lines, userQuestionSelectedStyle.Render("▶ Other")+userQuestionDescStyle.Render(" - Enter custom response"))
	} else {
		lines = append(lines, userQuestionOptionStyle.Render("  Other")+userQuestionDescStyle.Render(" - Enter custom response"))
	}

	// Join all lines
	content := strings.Join(lines, "\n")

	return userQuestionStyle.Width(v.width - 4).Render(content)
}

// SetPlanProjectSelection sets the plan project selection mode state.
func (v *ChatView) SetPlanProjectSelection(projects []string, selectedIndex int) {
	v.planProjectSelect = true
	v.planProjects = projects
	v.planProjectIndex = selectedIndex
	v.planProjectFilter = ""
	v.updateViewportSize()
}

// SetPlanProjectSelectionWithFilter sets the plan project selection mode state with a filter.
func (v *ChatView) SetPlanProjectSelectionWithFilter(projects []string, selectedIndex int, filter string) {
	v.planProjectSelect = true
	v.planProjects = projects
	v.planProjectIndex = selectedIndex
	v.planProjectFilter = filter
	v.updateViewportSize()
}

// ClearPlanProjectSelection clears plan project selection mode.
func (v *ChatView) ClearPlanProjectSelection() {
	v.planProjectSelect = false
	v.planProjects = nil
	v.planProjectIndex = 0
	v.planProjectFilter = ""
	v.updateViewportSize()
}

// SetPlanPromptMode sets the plan prompt mode state.
func (v *ChatView) SetPlanPromptMode(project string) {
	v.planPromptMode = true
	v.planPromptProject = project
	v.updateViewportSize()
}

// ClearPlanPromptMode clears plan prompt mode.
func (v *ChatView) ClearPlanPromptMode() {
	v.planPromptMode = false
	v.planPromptProject = ""
	v.updateViewportSize()
}

// renderPlanProjectSelection renders the project selection UI.
func (v *ChatView) renderPlanProjectSelection() string {
	if !v.planProjectSelect {
		return ""
	}

	style := lipgloss.NewStyle().
		Background(lipgloss.Color("#2B4B3B")). // Dark green background
		Padding(0, 1)

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4ADE80")). // Light green
		Bold(true)

	optionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E0E0E0"))

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#3B6B4B")).
		Bold(true)

	filterStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#3B5B4B"))

	var lines []string
	lines = append(lines, headerStyle.Render("Select a project to plan for:"))

	// Show filter input
	filterDisplay := v.planProjectFilter
	if filterDisplay == "" {
		filterDisplay = "Type to filter..."
	}
	lines = append(lines, filterStyle.Render("▸ "+filterDisplay+"█"))
	lines = append(lines, "") // Empty line

	if len(v.planProjects) == 0 {
		// No matching projects
		noMatchStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6666")).
			Italic(true)
		lines = append(lines, noMatchStyle.Render("  No matching projects"))
	} else {
		for i, project := range v.planProjects {
			if i == v.planProjectIndex {
				lines = append(lines, selectedStyle.Render("▶ "+project))
			} else {
				lines = append(lines, optionStyle.Render("  "+project))
			}
		}
	}

	lines = append(lines, "")
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	lines = append(lines, hintStyle.Render("↑/↓: select  Enter: confirm  Esc: cancel"))

	content := strings.Join(lines, "\n")
	return style.Width(v.width - 4).Render(content)
}

// renderPlanPromptMode renders the plan prompt mode header.
func (v *ChatView) renderPlanPromptMode() string {
	if !v.planPromptMode {
		return ""
	}

	style := lipgloss.NewStyle().
		Background(lipgloss.Color("#2B4B3B")). // Dark green background
		Padding(0, 1)

	headerStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#4ADE80")). // Light green
		Bold(true)

	projectStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Bold(true)

	var lines []string
	lines = append(lines, headerStyle.Render("New Plan Agent"))
	lines = append(lines, "Project: "+projectStyle.Render(v.planPromptProject))
	lines = append(lines, "")

	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	lines = append(lines, hintStyle.Render("Enter your planning task below. Press Enter to start, Esc to cancel."))

	content := strings.Join(lines, "\n")
	return style.Width(v.width - 4).Render(content)
}
