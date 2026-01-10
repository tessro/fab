package tui

import (
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// InputLine is a text input component for sending input to agents.
type InputLine struct {
	width   int
	height  int
	focused bool
	input   textinput.Model
}

// NewInputLine creates a new input line component.
func NewInputLine() InputLine {
	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.CharLimit = 4096
	ti.Prompt = "> "
	return InputLine{
		input: ti,
	}
}

// SetSize updates the component dimensions.
func (i *InputLine) SetSize(width, height int) {
	i.width = width
	i.height = height
	i.input.Width = width - 8 // Account for border (2), padding (2), and prompt (4)
}

// SetFocused sets the focus state.
func (i *InputLine) SetFocused(focused bool) {
	i.focused = focused
	if focused {
		i.input.Focus()
	} else {
		i.input.Blur()
	}
}

// IsFocused returns whether the input is focused.
func (i *InputLine) IsFocused() bool {
	return i.focused
}

// Update handles input events and returns a command.
func (i *InputLine) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	i.input, cmd = i.input.Update(msg)
	return cmd
}

// Value returns the current input value.
func (i *InputLine) Value() string {
	return i.input.Value()
}

// Clear resets the input value.
func (i *InputLine) Clear() {
	i.input.SetValue("")
}

// SetPlaceholder sets the placeholder text.
func (i *InputLine) SetPlaceholder(text string) {
	i.input.Placeholder = text
}

// Focus sets focus to the input.
func (i *InputLine) Focus() {
	i.input.Focus()
	i.focused = true
}

// View renders the input line.
func (i InputLine) View() string {
	style := inputLineStyle
	if i.focused {
		style = inputLineFocusedStyle
	}
	// Account for border when setting width
	return style.Width(i.width - 2).Render(i.input.View())
}
