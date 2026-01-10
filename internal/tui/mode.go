package tui

import "errors"

// Mode represents the current interaction mode of the TUI.
// Only one mode can be active at a time, providing clear state management.
type Mode int

const (
	// ModeNormal is the default mode for navigating the TUI.
	ModeNormal Mode = iota
	// ModeInput means the user is typing in the input line.
	ModeInput
	// ModeAbortConfirm means the user is being asked to confirm an abort.
	ModeAbortConfirm
	// ModeUserQuestion means the user is answering a question from Claude.
	ModeUserQuestion
)

// String returns the string representation of a Mode.
func (m Mode) String() string {
	switch m {
	case ModeNormal:
		return "normal"
	case ModeInput:
		return "input"
	case ModeAbortConfirm:
		return "abort_confirm"
	case ModeUserQuestion:
		return "user_question"
	default:
		return "unknown"
	}
}

// ModeState centralizes all mode and focus-related state for the TUI.
// This provides a single source of truth for the current interaction state.
type ModeState struct {
	// Mode is the current interaction mode (normal, input, abort confirmation, user question).
	Mode Mode

	// Focus indicates which panel is currently focused when in normal mode.
	Focus Focus

	// AbortAgentID is the agent being aborted (only valid when Mode == ModeAbortConfirm).
	AbortAgentID string

	// HasPendingPermission indicates if there's a permission request awaiting approval.
	HasPendingPermission bool

	// HasPendingAction indicates if there's a staged action awaiting approval.
	HasPendingAction bool

	// HasPendingUserQuestion indicates if there's a user question awaiting response.
	HasPendingUserQuestion bool
}

// NewModeState creates a new ModeState with default values.
func NewModeState() ModeState {
	return ModeState{
		Mode:  ModeNormal,
		Focus: FocusAgentList,
	}
}

// Validation errors for mode state transitions.
var (
	ErrInvalidModeTransition = errors.New("invalid mode transition")
	ErrMissingAgentID        = errors.New("abort requires an agent ID")
	ErrAlreadyInMode         = errors.New("already in this mode")
)

// SetFocus changes the focus panel. Only valid in normal mode.
// Returns an error if called in a mode that doesn't support focus changes.
func (s *ModeState) SetFocus(focus Focus) error {
	// Focus changes are only valid in normal mode
	if s.Mode != ModeNormal {
		return ErrInvalidModeTransition
	}
	s.Focus = focus
	return nil
}

// CycleFocus advances focus to the next panel in the cycle.
// AgentList -> ChatView -> InputLine -> AgentList
// Returns the new focus value, or an error if not in normal mode.
func (s *ModeState) CycleFocus() (Focus, error) {
	if s.Mode != ModeNormal {
		return s.Focus, ErrInvalidModeTransition
	}

	switch s.Focus {
	case FocusAgentList:
		s.Focus = FocusChatView
	case FocusChatView:
		s.Focus = FocusInputLine
	case FocusInputLine:
		s.Focus = FocusAgentList
	}
	return s.Focus, nil
}

// EnterInputMode transitions to input mode.
// Returns an error if already in input mode or in abort confirmation.
func (s *ModeState) EnterInputMode() error {
	if s.Mode == ModeInput {
		return ErrAlreadyInMode
	}
	if s.Mode == ModeAbortConfirm {
		return ErrInvalidModeTransition
	}
	s.Mode = ModeInput
	s.Focus = FocusInputLine
	return nil
}

// ExitInputMode returns from input mode to normal mode.
// Returns an error if not currently in input mode.
func (s *ModeState) ExitInputMode() error {
	if s.Mode != ModeInput {
		return ErrInvalidModeTransition
	}
	s.Mode = ModeNormal
	s.Focus = FocusChatView
	return nil
}

// EnterAbortConfirm transitions to abort confirmation mode for the given agent.
// Returns an error if agentID is empty or already in abort confirmation.
func (s *ModeState) EnterAbortConfirm(agentID string) error {
	if agentID == "" {
		return ErrMissingAgentID
	}
	if s.Mode == ModeAbortConfirm {
		return ErrAlreadyInMode
	}
	if s.Mode == ModeInput {
		return ErrInvalidModeTransition
	}
	s.Mode = ModeAbortConfirm
	s.AbortAgentID = agentID
	return nil
}

// ConfirmAbort confirms the abort and returns to normal mode.
// Returns the agent ID that was being aborted, or an error if not in abort mode.
func (s *ModeState) ConfirmAbort() (string, error) {
	if s.Mode != ModeAbortConfirm {
		return "", ErrInvalidModeTransition
	}
	agentID := s.AbortAgentID
	s.Mode = ModeNormal
	s.AbortAgentID = ""
	return agentID, nil
}

// CancelAbort cancels the abort confirmation and returns to normal mode.
// Returns an error if not in abort confirmation mode.
func (s *ModeState) CancelAbort() error {
	if s.Mode != ModeAbortConfirm {
		return ErrInvalidModeTransition
	}
	s.Mode = ModeNormal
	s.AbortAgentID = ""
	return nil
}

// SetPendingApprovals updates the pending approval state.
func (s *ModeState) SetPendingApprovals(hasPermission, hasAction, hasUserQuestion bool) {
	s.HasPendingPermission = hasPermission
	s.HasPendingAction = hasAction
	s.HasPendingUserQuestion = hasUserQuestion
}

// NeedsApproval returns true if there's any pending approval.
func (s *ModeState) NeedsApproval() bool {
	return s.HasPendingPermission || s.HasPendingAction || s.HasPendingUserQuestion
}

// EnterUserQuestionMode transitions to user question mode.
// Returns an error if already in user question mode or in another modal mode.
func (s *ModeState) EnterUserQuestionMode() error {
	if s.Mode == ModeUserQuestion {
		return ErrAlreadyInMode
	}
	if s.Mode == ModeAbortConfirm || s.Mode == ModeInput {
		return ErrInvalidModeTransition
	}
	s.Mode = ModeUserQuestion
	return nil
}

// ExitUserQuestionMode returns from user question mode to normal mode.
// Returns an error if not currently in user question mode.
func (s *ModeState) ExitUserQuestionMode() error {
	if s.Mode != ModeUserQuestion {
		return ErrInvalidModeTransition
	}
	s.Mode = ModeNormal
	s.Focus = FocusAgentList
	return nil
}

// IsUserQuestion returns true if in user question mode.
func (s *ModeState) IsUserQuestion() bool {
	return s.Mode == ModeUserQuestion
}

// IsNormal returns true if in normal mode.
func (s *ModeState) IsNormal() bool {
	return s.Mode == ModeNormal
}

// IsInputting returns true if in input mode.
func (s *ModeState) IsInputting() bool {
	return s.Mode == ModeInput
}

// IsAbortConfirming returns true if in abort confirmation mode.
func (s *ModeState) IsAbortConfirming() bool {
	return s.Mode == ModeAbortConfirm
}

// Validate checks that the mode state is internally consistent.
// Returns an error if the state is invalid.
func (s *ModeState) Validate() error {
	switch s.Mode {
	case ModeAbortConfirm:
		if s.AbortAgentID == "" {
			return ErrMissingAgentID
		}
	case ModeNormal, ModeInput, ModeUserQuestion:
		// AbortAgentID should be empty when not in abort mode
		if s.AbortAgentID != "" {
			return errors.New("abort agent ID should be empty when not in abort mode")
		}
	}
	return nil
}
