package tui

// HelpBar displays context-sensitive keyboard shortcuts at the bottom of the TUI.
type HelpBar struct {
	width int

	// Current context
	focus             Focus
	pendingPermission bool
	pendingAction     bool
}

// NewHelpBar creates a new help bar component.
func NewHelpBar() HelpBar {
	return HelpBar{}
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
	var helpText string

	switch h.focus {
	case FocusAgentList:
		if h.pendingPermission {
			helpText = "y: allow  n: deny  j/k: navigate  enter: view  q: quit"
		} else if h.pendingAction {
			helpText = "y: approve  n: reject  j/k: navigate  enter: view  q: quit"
		} else {
			helpText = "j/k: navigate  enter: view chat  i: input  tab: switch pane  q: quit"
		}
	case FocusChatView:
		if h.pendingPermission {
			helpText = "y: allow  n: deny  j/k: scroll  tab: switch pane  q: quit"
		} else if h.pendingAction {
			helpText = "y: approve  n: reject  j/k: scroll  tab: switch pane  q: quit"
		} else {
			helpText = "j/k/pgup/pgdn: scroll  i: input  tab: switch pane  q: quit"
		}
	case FocusInputLine:
		helpText = "enter: send  esc: cancel  tab: switch pane"
	}

	return statusStyle.Width(h.width).Render(helpText)
}
