package tui

import (
	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
)

// maxHistorySize limits the number of entries stored in history.
const maxHistorySize = 100

// maxInputHeight limits how tall the input can grow (in lines of content).
const maxInputHeight = 8

// InputLine is a text input component for sending input to agents.
type InputLine struct {
	width   int
	height  int
	focused bool
	input   textarea.Model

	// Input history for up/down navigation
	history      []string
	historyIndex int    // -1 means not browsing history; 0+ is index into history
	savedInput   string // Saved current input when browsing history
}

// NewInputLine creates a new input line component.
func NewInputLine() InputLine {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.CharLimit = 4096
	ta.Prompt = "> "
	ta.ShowLineNumbers = false
	ta.SetHeight(1)
	// Remove default enter key behavior - we'll handle it ourselves
	ta.KeyMap.InsertNewline.SetEnabled(false)
	return InputLine{
		input:        ta,
		historyIndex: -1,
	}
}

// SetSize updates the component dimensions.
func (i *InputLine) SetSize(width, height int) {
	i.width = width
	i.height = height
	i.input.SetWidth(width - 6) // Account for padding (2) and prompt (4)
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
	i.updateHeight()
	return cmd
}

// Value returns the current input value.
func (i *InputLine) Value() string {
	return i.input.Value()
}

// Clear resets the input value.
func (i *InputLine) Clear() {
	i.input.SetValue("")
	i.input.SetHeight(1) // Reset to single line
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
	// No border - docked inside chat pane
	return style.Width(i.width).Render(i.input.View())
}

// AddToHistory adds the given input to history if non-empty.
func (i *InputLine) AddToHistory(input string) {
	if input == "" {
		return
	}
	// Avoid duplicates at the end
	if len(i.history) > 0 && i.history[len(i.history)-1] == input {
		return
	}
	i.history = append(i.history, input)
	// Trim to max size
	if len(i.history) > maxHistorySize {
		i.history = i.history[len(i.history)-maxHistorySize:]
	}
	i.historyIndex = -1
	i.savedInput = ""
}

// HistoryUp navigates to the previous (older) history entry.
// Returns true if the input was changed.
func (i *InputLine) HistoryUp() bool {
	if len(i.history) == 0 {
		return false
	}

	// First time pressing up: save current input and start from most recent
	if i.historyIndex == -1 {
		i.savedInput = i.input.Value()
		i.historyIndex = len(i.history) - 1
	} else if i.historyIndex > 0 {
		// Go to older entry
		i.historyIndex--
	} else {
		// Already at oldest entry
		return false
	}

	i.input.SetValue(i.history[i.historyIndex])
	i.input.CursorEnd()
	return true
}

// HistoryDown navigates to the next (newer) history entry.
// Returns true if the input was changed.
func (i *InputLine) HistoryDown() bool {
	if i.historyIndex == -1 {
		return false // Not browsing history
	}

	if i.historyIndex < len(i.history)-1 {
		// Go to newer entry
		i.historyIndex++
		i.input.SetValue(i.history[i.historyIndex])
		i.input.CursorEnd()
		return true
	}

	// At newest entry, restore saved input
	i.historyIndex = -1
	i.input.SetValue(i.savedInput)
	i.input.CursorEnd()
	i.savedInput = ""
	return true
}

// ResetHistoryNavigation resets history browsing state.
func (i *InputLine) ResetHistoryNavigation() {
	i.historyIndex = -1
	i.savedInput = ""
}

// InsertNewline inserts a newline at the cursor position (for shift+enter).
func (i *InputLine) InsertNewline() {
	i.input.InsertString("\n")
	i.updateHeight()
}

// ContentHeight returns the height needed to display the current content.
// Minimum 1, maximum maxInputHeight.
func (i *InputLine) ContentHeight() int {
	lines := i.input.LineCount()
	if lines < 1 {
		lines = 1
	}
	if lines > maxInputHeight {
		lines = maxInputHeight
	}
	return lines
}

// updateHeight adjusts the textarea height based on content.
func (i *InputLine) updateHeight() {
	i.input.SetHeight(i.ContentHeight())
}
