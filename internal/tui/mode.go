package tui

import (
	"errors"
	"strings"
)

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
	// ModePlanProjectSelect means the user is selecting a project for planning.
	ModePlanProjectSelect
	// ModePlanPrompt means the user is entering a planning prompt.
	ModePlanPrompt
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
	case ModePlanProjectSelect:
		return "plan_project_select"
	case ModePlanPrompt:
		return "plan_prompt"
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

	// HasPendingUserQuestion indicates if there's a user question awaiting response.
	HasPendingUserQuestion bool

	// PlanProject is the selected project for planning (only valid when Mode == ModePlanPrompt).
	PlanProject string

	// PlanProjects is the list of available projects for planning (only valid when Mode == ModePlanProjectSelect).
	PlanProjects []string

	// PlanProjectIndex is the currently selected project index (only valid when Mode == ModePlanProjectSelect).
	PlanProjectIndex int

	// PlanProjectFilter is the current filter text for fuzzy matching (only valid when Mode == ModePlanProjectSelect).
	PlanProjectFilter string

	// PlanProjectFiltered is the list of projects that match the filter (only valid when Mode == ModePlanProjectSelect).
	PlanProjectFiltered []string
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
// AgentList -> ChatView -> AgentList
// InputLine is not part of the cycle - it's accessed via the FocusChat key binding.
// Returns the new focus value, or an error if not in normal mode.
func (s *ModeState) CycleFocus() (Focus, error) {
	if s.Mode != ModeNormal {
		return s.Focus, ErrInvalidModeTransition
	}

	switch s.Focus {
	case FocusAgentList:
		s.Focus = FocusChatView
	case FocusChatView, FocusInputLine:
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
// The second parameter (hasAction) is kept for API compatibility but ignored.
func (s *ModeState) SetPendingApprovals(hasPermission, hasAction, hasUserQuestion bool) {
	s.HasPendingPermission = hasPermission
	s.HasPendingUserQuestion = hasUserQuestion
}

// NeedsApproval returns true if there's any pending approval.
func (s *ModeState) NeedsApproval() bool {
	return s.HasPendingPermission || s.HasPendingUserQuestion
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

// EnterPlanProjectSelect transitions to plan project selection mode.
// projects is the list of available projects to choose from.
func (s *ModeState) EnterPlanProjectSelect(projects []string) error {
	if s.Mode != ModeNormal {
		return ErrInvalidModeTransition
	}
	if len(projects) == 0 {
		return errors.New("no projects available")
	}
	s.Mode = ModePlanProjectSelect
	s.PlanProjects = projects
	s.PlanProjectIndex = 0
	s.PlanProjectFilter = ""
	s.PlanProjectFiltered = projects // Initially show all projects
	return nil
}

// PlanProjectSelectUp moves the selection up in the project list.
func (s *ModeState) PlanProjectSelectUp() {
	if s.Mode != ModePlanProjectSelect {
		return
	}
	if s.PlanProjectIndex > 0 {
		s.PlanProjectIndex--
	}
}

// PlanProjectSelectDown moves the selection down in the project list.
func (s *ModeState) PlanProjectSelectDown() {
	if s.Mode != ModePlanProjectSelect {
		return
	}
	if s.PlanProjectIndex < len(s.PlanProjectFiltered)-1 {
		s.PlanProjectIndex++
	}
}

// SelectPlanProject selects the current project and transitions to prompt mode.
func (s *ModeState) SelectPlanProject() (string, error) {
	if s.Mode != ModePlanProjectSelect {
		return "", ErrInvalidModeTransition
	}
	if len(s.PlanProjectFiltered) == 0 {
		return "", errors.New("no matching projects")
	}
	if s.PlanProjectIndex < 0 || s.PlanProjectIndex >= len(s.PlanProjectFiltered) {
		return "", errors.New("invalid project selection")
	}
	s.PlanProject = s.PlanProjectFiltered[s.PlanProjectIndex]
	s.Mode = ModePlanPrompt
	s.Focus = FocusInputLine
	return s.PlanProject, nil
}

// CancelPlanProjectSelect cancels project selection and returns to normal mode.
func (s *ModeState) CancelPlanProjectSelect() error {
	if s.Mode != ModePlanProjectSelect {
		return ErrInvalidModeTransition
	}
	s.Mode = ModeNormal
	s.PlanProjects = nil
	s.PlanProjectIndex = 0
	s.PlanProjectFilter = ""
	s.PlanProjectFiltered = nil
	return nil
}

// ExitPlanPromptMode returns from plan prompt mode to normal mode.
// Returns the selected project name, or an error if not in plan prompt mode.
func (s *ModeState) ExitPlanPromptMode() (string, error) {
	if s.Mode != ModePlanPrompt {
		return "", ErrInvalidModeTransition
	}
	project := s.PlanProject
	s.Mode = ModeNormal
	s.Focus = FocusChatView
	s.PlanProject = ""
	s.PlanProjects = nil
	s.PlanProjectIndex = 0
	return project, nil
}

// CancelPlanPromptMode cancels plan prompt mode without completing.
func (s *ModeState) CancelPlanPromptMode() error {
	if s.Mode != ModePlanPrompt {
		return ErrInvalidModeTransition
	}
	s.Mode = ModeNormal
	s.Focus = FocusAgentList
	s.PlanProject = ""
	s.PlanProjects = nil
	s.PlanProjectIndex = 0
	return nil
}

// IsPlanProjectSelect returns true if in plan project selection mode.
func (s *ModeState) IsPlanProjectSelect() bool {
	return s.Mode == ModePlanProjectSelect
}

// IsPlanPrompt returns true if in plan prompt mode.
func (s *ModeState) IsPlanPrompt() bool {
	return s.Mode == ModePlanPrompt
}

// SelectedPlanProject returns the selected project name and the filtered list of projects.
func (s *ModeState) SelectedPlanProject() (string, []string, int) {
	return s.PlanProject, s.PlanProjectFiltered, s.PlanProjectIndex
}

// PlanProjectFilterState returns the current filter string.
func (s *ModeState) PlanProjectFilterState() string {
	return s.PlanProjectFilter
}

// PlanProjectSetFilter updates the filter and recomputes the filtered list.
func (s *ModeState) PlanProjectSetFilter(filter string) {
	if s.Mode != ModePlanProjectSelect {
		return
	}
	s.PlanProjectFilter = filter
	s.PlanProjectFiltered = filterProjects(s.PlanProjects, filter)
	// Reset index to 0, but ensure it's valid
	s.PlanProjectIndex = 0
}

// PlanProjectAppendFilter appends a character to the filter.
func (s *ModeState) PlanProjectAppendFilter(ch rune) {
	if s.Mode != ModePlanProjectSelect {
		return
	}
	s.PlanProjectSetFilter(s.PlanProjectFilter + string(ch))
}

// PlanProjectBackspaceFilter removes the last character from the filter.
func (s *ModeState) PlanProjectBackspaceFilter() {
	if s.Mode != ModePlanProjectSelect {
		return
	}
	if len(s.PlanProjectFilter) > 0 {
		s.PlanProjectSetFilter(s.PlanProjectFilter[:len(s.PlanProjectFilter)-1])
	}
}

// filterProjects returns projects that fuzzy match the filter string.
// A project matches if it contains all characters from the filter in order (case-insensitive).
func filterProjects(projects []string, filter string) []string {
	if filter == "" {
		return projects
	}
	var result []string
	filterLower := strings.ToLower(filter)
	for _, project := range projects {
		if fuzzyMatch(strings.ToLower(project), filterLower) {
			result = append(result, project)
		}
	}
	return result
}

// fuzzyMatch checks if text contains all characters from pattern in order.
func fuzzyMatch(text, pattern string) bool {
	if pattern == "" {
		return true
	}
	pi := 0
	for i := 0; i < len(text) && pi < len(pattern); i++ {
		if text[i] == pattern[pi] {
			pi++
		}
	}
	return pi == len(pattern)
}
