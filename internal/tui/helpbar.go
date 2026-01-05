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
func (h *HelpBar) SetContext(focus Focus, pendingPermission, pendingAction bool) {
	h.focus = focus
	h.pendingPermission = pendingPermission
	h.pendingAction = pendingAction
}

// View renders the help bar with context-sensitive keyboard shortcuts.
func (h HelpBar) View() string {
	var bindings []key.Binding

	switch h.focus {
	case FocusAgentList:
		if h.pendingPermission || h.pendingAction {
			bindings = []key.Binding{h.keys.Approve, h.keys.Reject, h.keys.Down, h.keys.Select, h.keys.Quit}
		} else {
			bindings = []key.Binding{h.keys.Down, h.keys.Select, h.keys.FocusChat, h.keys.Tab, h.keys.Quit}
		}
	case FocusChatView:
		if h.pendingPermission || h.pendingAction {
			bindings = []key.Binding{h.keys.Approve, h.keys.Reject, h.keys.Down, h.keys.Tab, h.keys.Quit}
		} else {
			bindings = []key.Binding{h.keys.Down, h.keys.PageUp, h.keys.FocusChat, h.keys.Tab, h.keys.Quit}
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
