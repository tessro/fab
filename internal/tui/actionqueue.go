package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/tessro/fab/internal/daemon"
)

// ActionQueue displays a navigable list of pending staged actions.
type ActionQueue struct {
	width    int
	height   int
	actions  []daemon.StagedAction
	selected int
	focused  bool
}

// NewActionQueue creates a new action queue component.
func NewActionQueue() ActionQueue {
	return ActionQueue{
		selected: 0,
	}
}

// SetSize updates the component dimensions.
func (q *ActionQueue) SetSize(width, height int) {
	q.width = width
	q.height = height
}

// SetActions updates the action list.
func (q *ActionQueue) SetActions(actions []daemon.StagedAction) {
	q.actions = actions
	// Adjust selection if list shrunk
	if q.selected >= len(actions) && len(actions) > 0 {
		q.selected = len(actions) - 1
	}
	if len(actions) == 0 {
		q.selected = 0
	}
}

// Actions returns the current action list.
func (q *ActionQueue) Actions() []daemon.StagedAction {
	return q.actions
}

// Selected returns the currently selected action, or nil if none.
func (q *ActionQueue) Selected() *daemon.StagedAction {
	if len(q.actions) == 0 || q.selected < 0 || q.selected >= len(q.actions) {
		return nil
	}
	return &q.actions[q.selected]
}

// SelectedIndex returns the current selection index.
func (q *ActionQueue) SelectedIndex() int {
	return q.selected
}

// MoveUp moves selection up one item.
func (q *ActionQueue) MoveUp() {
	if q.selected > 0 {
		q.selected--
	}
}

// MoveDown moves selection down one item.
func (q *ActionQueue) MoveDown() {
	if q.selected < len(q.actions)-1 {
		q.selected++
	}
}

// MoveToTop moves selection to the first item.
func (q *ActionQueue) MoveToTop() {
	q.selected = 0
}

// MoveToBottom moves selection to the last item.
func (q *ActionQueue) MoveToBottom() {
	if len(q.actions) > 0 {
		q.selected = len(q.actions) - 1
	}
}

// Len returns the number of pending actions.
func (q *ActionQueue) Len() int {
	return len(q.actions)
}

// SetFocused sets the focus state.
func (q *ActionQueue) SetFocused(focused bool) {
	q.focused = focused
}

// IsFocused returns whether the action queue is focused.
func (q *ActionQueue) IsFocused() bool {
	return q.focused
}

// View renders the action queue.
func (q ActionQueue) View() string {
	// Calculate inner dimensions (accounting for border)
	innerWidth := q.width - 2
	innerHeight := q.height - 2 - 1 // -2 for border, -1 for header
	if innerWidth < 1 {
		innerWidth = 1
	}
	if innerHeight < 1 {
		innerHeight = 1
	}

	// Header with action count
	titleStyle := paneTitleStyle
	if q.focused {
		titleStyle = paneTitleFocusedStyle
	}
	title := "Actions"
	if len(q.actions) > 0 {
		title = fmt.Sprintf("Actions (%d)", len(q.actions))
	}
	header := titleStyle.Width(innerWidth).Render(title)

	// Content
	var content string
	if len(q.actions) == 0 {
		content = actionQueueEmptyStyle.Width(innerWidth).Height(innerHeight).Render("No pending actions")
	} else {
		var rows []string
		for i, action := range q.actions {
			row := q.renderAction(i, action, innerWidth)
			rows = append(rows, row)
		}
		content = actionQueueContainerStyle.Width(innerWidth).Height(innerHeight).Render(strings.Join(rows, "\n"))
	}

	// Combine header and content
	inner := lipgloss.JoinVertical(lipgloss.Left, header, content)

	// Apply border
	borderStyle := paneBorderStyle
	if q.focused {
		borderStyle = paneBorderFocusedStyle
	}

	return borderStyle.Width(q.width - 2).Height(q.height - 2).Render(inner)
}

// renderAction renders a single action row.
func (q ActionQueue) renderAction(index int, action daemon.StagedAction, width int) string {
	isSelected := index == q.selected

	// Type icon
	typeIcon := q.typeIcon(action.Type)
	typeStyle := q.typeStyle(action.Type)
	typeStr := typeStyle.Render(typeIcon)

	// Agent ID
	agentStr := actionQueueAgentStyle.Render(action.AgentID)

	// Project name
	projectStr := actionQueueProjectStyle.Render(action.Project)

	// Payload preview (truncated)
	payload := q.truncatePayload(action.Payload, width-30)
	payloadStr := actionQueuePayloadStyle.Render(payload)

	// Time since created
	age := time.Since(action.CreatedAt).Truncate(time.Second)
	ageStr := actionQueueAgeStyle.Render(formatDuration(age))

	// Compose the row
	left := lipgloss.JoinHorizontal(lipgloss.Center,
		typeStr, " ",
		agentStr, " ",
		projectStr, " ",
		payloadStr,
	)

	// Right-align duration
	leftWidth := lipgloss.Width(left)
	rightWidth := lipgloss.Width(ageStr)
	spacerWidth := width - leftWidth - rightWidth - 2 // padding
	if spacerWidth < 1 {
		spacerWidth = 1
	}
	spacer := strings.Repeat(" ", spacerWidth)

	row := left + spacer + ageStr

	// Apply selection styling
	if isSelected {
		return actionQueueRowSelectedStyle.Width(width).Render(row)
	}
	return actionQueueRowStyle.Width(width).Render(row)
}

// typeIcon returns an icon for the action type.
func (q ActionQueue) typeIcon(t daemon.ActionType) string {
	switch t {
	case daemon.ActionSendMessage:
		return "✉"
	case daemon.ActionQuit:
		return "⏹"
	default:
		return "?"
	}
}

// typeStyle returns the style for an action type indicator.
func (q ActionQueue) typeStyle(t daemon.ActionType) lipgloss.Style {
	switch t {
	case daemon.ActionSendMessage:
		return lipgloss.NewStyle().Foreground(primaryColor)
	case daemon.ActionQuit:
		return lipgloss.NewStyle().Foreground(warningColor)
	default:
		return lipgloss.NewStyle().Foreground(mutedColor)
	}
}

// truncatePayload truncates payload text for display.
func (q ActionQueue) truncatePayload(payload string, maxLen int) string {
	// Replace newlines with spaces for single-line display
	payload = strings.ReplaceAll(payload, "\n", " ")
	payload = strings.TrimSpace(payload)

	if maxLen < 10 {
		maxLen = 10
	}
	if len(payload) > maxLen {
		return payload[:maxLen-3] + "..."
	}
	return payload
}
