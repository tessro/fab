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

// View renders the action queue.
func (q ActionQueue) View() string {
	if len(q.actions) == 0 {
		return actionQueueEmptyStyle.Width(q.width).Height(q.height).Render("No pending actions")
	}

	var rows []string
	for i, action := range q.actions {
		row := q.renderAction(i, action)
		rows = append(rows, row)
	}

	content := strings.Join(rows, "\n")
	return actionQueueContainerStyle.Width(q.width).Height(q.height).Render(content)
}

// renderAction renders a single action row.
func (q ActionQueue) renderAction(index int, action daemon.StagedAction) string {
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
	payload := q.truncatePayload(action.Payload, q.width-40)
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
	spacerWidth := q.width - leftWidth - rightWidth - 4 // padding
	if spacerWidth < 1 {
		spacerWidth = 1
	}
	spacer := strings.Repeat(" ", spacerWidth)

	row := left + spacer + ageStr

	// Apply selection styling
	if isSelected {
		return actionQueueRowSelectedStyle.Width(q.width).Render(row)
	}
	return actionQueueRowStyle.Width(q.width).Render(row)
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

// Header renders a header line for the action queue section.
func (q ActionQueue) Header() string {
	count := len(q.actions)
	if count == 0 {
		return actionQueueHeaderStyle.Render("Actions")
	}
	return actionQueueHeaderStyle.Render(fmt.Sprintf("Actions (%d)", count))
}
