package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// HelpBar displays context-sensitive keyboard shortcuts at the bottom of the TUI.
type HelpBar struct {
	width int
	keys  KeyBindings

	// Current mode state
	modeState ModeState

	// Error display
	errorMsg string
}

// NewHelpBar creates a new help bar component.
func NewHelpBar() HelpBar {
	return HelpBar{
		keys: DefaultKeyBindings(),
	}
}

// SetWidth updates the help bar width.
func (h *HelpBar) SetWidth(width int) {
	h.width = width
}

// SetModeState updates the help bar's mode state for rendering appropriate shortcuts.
func (h *HelpBar) SetModeState(state ModeState) {
	h.modeState = state
}

// SetError sets the error message to display.
func (h *HelpBar) SetError(msg string) {
	h.errorMsg = msg
}

// ClearError clears the error message.
func (h *HelpBar) ClearError() {
	h.errorMsg = ""
}

// View renders the help bar with context-sensitive keyboard shortcuts.
func (h HelpBar) View() string {
	// Error display takes top priority
	if h.errorMsg != "" {
		return errorBarStyle.Width(h.width).Render("Error: " + h.errorMsg)
	}

	var bindings []key.Binding

	// Abort confirmation mode takes priority
	if h.modeState.IsAbortConfirming() {
		bindings = []key.Binding{h.keys.Approve, h.keys.Reject}
		helpText := formatHelp(bindings)
		return statusStyle.Width(h.width).Render("Abort agent? " + helpText)
	}

	// Input mode has its own set of bindings
	if h.modeState.IsInputting() {
		bindings = []key.Binding{h.keys.Submit, h.keys.Cancel, h.keys.Tab}
		helpText := formatHelp(bindings)
		return statusStyle.Width(h.width).Render("-- INPUT -- " + helpText)
	}

	// Plan project selection mode
	if h.modeState.IsPlanProjectSelect() {
		bindings = []key.Binding{h.keys.Approve, h.keys.Down, h.keys.Cancel, h.keys.Quit}
		helpText := formatHelp(bindings)
		return statusStyle.Width(h.width).Render("-- SELECT PROJECT -- " + helpText)
	}

	// Plan prompt mode
	if h.modeState.IsPlanPrompt() {
		bindings = []key.Binding{h.keys.Submit, h.keys.Cancel, h.keys.Quit}
		helpText := formatHelp(bindings)
		return statusStyle.Width(h.width).Render("-- PLAN -- " + helpText)
	}

	// Normal mode bindings depend on focus and pending approvals
	switch h.modeState.Focus {
	case FocusAgentList:
		if h.modeState.NeedsApproval() {
			bindings = []key.Binding{h.keys.Approve, h.keys.Reject, h.keys.Down, h.keys.Tab, h.keys.Quit}
		} else {
			bindings = []key.Binding{h.keys.Down, h.keys.Tab, h.keys.Plan, h.keys.Abort, h.keys.Quit}
		}
	case FocusChatView:
		if h.modeState.NeedsApproval() {
			bindings = []key.Binding{h.keys.Approve, h.keys.Reject, h.keys.Down, h.keys.Tab, h.keys.Quit}
		} else {
			bindings = []key.Binding{h.keys.FocusChat, h.keys.Down, h.keys.PageUp, h.keys.Plan, h.keys.Abort, h.keys.Quit}
		}
	case FocusInputLine:
		bindings = []key.Binding{h.keys.Tab, h.keys.Quit}
	}

	helpText := formatHelp(bindings)
	return statusStyle.Width(h.width).Render(helpText)
}

// formatHelp formats a list of key bindings as help text.
func formatHelp(bindings []key.Binding) string {
	var parts []string
	for _, b := range bindings {
		help := b.Help()
		parts = append(parts, help.Key+": "+help.Desc)
	}
	return strings.Join(parts, "  ")
}
