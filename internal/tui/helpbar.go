package tui

import (
	"strings"

	"github.com/charmbracelet/bubbles/key"
)

// HelpBar displays context-sensitive keyboard shortcuts at the bottom of the TUI.
type HelpBar struct {
	width int
	keys  KeyBindings

	// Current context
	focus             Focus
	pendingPermission bool
	pendingAction     bool
	abortConfirming   bool

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

// SetContext updates the help bar's context for rendering appropriate shortcuts.
func (h *HelpBar) SetContext(focus Focus, pendingPermission, pendingAction, abortConfirming bool) {
	h.focus = focus
	h.pendingPermission = pendingPermission
	h.pendingAction = pendingAction
	h.abortConfirming = abortConfirming
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

	// Abort confirmation takes priority
	if h.abortConfirming {
		bindings = []key.Binding{h.keys.Approve, h.keys.Reject}
		helpText := formatHelp(bindings)
		return statusStyle.Width(h.width).Render("Abort agent? " + helpText)
	}

	switch h.focus {
	case FocusAgentList:
		if h.pendingPermission || h.pendingAction {
			bindings = []key.Binding{h.keys.Approve, h.keys.Reject, h.keys.Down, h.keys.Select, h.keys.Quit}
		} else {
			bindings = []key.Binding{h.keys.Down, h.keys.Select, h.keys.FocusChat, h.keys.Abort, h.keys.Quit}
		}
	case FocusChatView:
		if h.pendingPermission || h.pendingAction {
			bindings = []key.Binding{h.keys.Approve, h.keys.Reject, h.keys.Down, h.keys.Tab, h.keys.Quit}
		} else {
			bindings = []key.Binding{h.keys.Down, h.keys.PageUp, h.keys.FocusChat, h.keys.Abort, h.keys.Quit}
		}
	case FocusInputLine:
		bindings = []key.Binding{h.keys.Submit, h.keys.Cancel, h.keys.Tab}
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
