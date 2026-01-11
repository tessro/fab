package tui

import "github.com/charmbracelet/bubbles/key"

// KeyBindings defines all keyboard shortcuts for the TUI.
type KeyBindings struct {
	// Global keys
	Quit      key.Binding
	Tab       key.Binding
	FocusChat key.Binding
	Reconnect key.Binding

	// Navigation keys
	Up       key.Binding
	Down     key.Binding
	Top      key.Binding
	Bottom   key.Binding
	PageUp   key.Binding
	PageDown key.Binding

	// Action keys
	Approve key.Binding
	Reject  key.Binding
	Abort   key.Binding
	Plan    key.Binding

	// Input keys
	Submit      key.Binding
	Cancel      key.Binding
	HistoryUp   key.Binding
	HistoryDown key.Binding
	NewLine     key.Binding
}

// DefaultKeyBindings returns the default key bindings.
func DefaultKeyBindings() KeyBindings {
	return KeyBindings{
		Quit: key.NewBinding(
			key.WithKeys("q", "ctrl+c"),
			key.WithHelp("q", "quit"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch pane"),
		),
		FocusChat: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "input"),
		),
		Reconnect: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "reconnect"),
		),

		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k", "up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j", "down"),
		),
		Top: key.NewBinding(
			key.WithKeys("g", "home"),
			key.WithHelp("g", "top"),
		),
		Bottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G", "bottom"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("ctrl+u", "pgup"),
			key.WithHelp("pgup", "page up"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("ctrl+d", "pgdown"),
			key.WithHelp("pgdn", "page down"),
		),

		Approve: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "approve"),
		),
		Reject: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "reject"),
		),
		Abort: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "abort"),
		),
		Plan: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "plan"),
		),

		Submit: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "send"),
		),
		Cancel: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
		HistoryUp: key.NewBinding(
			key.WithKeys("up"),
			key.WithHelp("↑", "history prev"),
		),
		HistoryDown: key.NewBinding(
			key.WithKeys("down"),
			key.WithHelp("↓", "history next"),
		),
		NewLine: key.NewBinding(
			key.WithKeys("shift+enter"),
			key.WithHelp("shift+enter", "new line"),
		),
	}
}
