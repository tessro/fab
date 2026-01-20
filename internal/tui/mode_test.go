package tui

import (
	"testing"
)

func TestNewModeState(t *testing.T) {
	state := NewModeState()

	if state.Mode != ModeNormal {
		t.Errorf("expected ModeNormal, got %v", state.Mode)
	}
	if state.Focus != FocusAgentList {
		t.Errorf("expected FocusAgentList, got %v", state.Focus)
	}
	if state.AbortAgentID != "" {
		t.Errorf("expected empty AbortAgentID, got %q", state.AbortAgentID)
	}
}

func TestModeState_SetFocus(t *testing.T) {
	tests := []struct {
		name        string
		initialMode Mode
		targetFocus Focus
		wantErr     bool
	}{
		{
			name:        "normal mode allows focus change",
			initialMode: ModeNormal,
			targetFocus: FocusChatView,
			wantErr:     false,
		},
		{
			name:        "input mode rejects focus change",
			initialMode: ModeInput,
			targetFocus: FocusAgentList,
			wantErr:     true,
		},
		{
			name:        "abort mode rejects focus change",
			initialMode: ModeAbortConfirm,
			targetFocus: FocusAgentList,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := NewModeState()
			state.Mode = tt.initialMode
			if tt.initialMode == ModeAbortConfirm {
				state.AbortAgentID = "test-agent"
			}

			err := state.SetFocus(tt.targetFocus)
			if (err != nil) != tt.wantErr {
				t.Errorf("SetFocus() error = %v, wantErr %v", err, tt.wantErr)
			}

			if !tt.wantErr && state.Focus != tt.targetFocus {
				t.Errorf("Focus = %v, want %v", state.Focus, tt.targetFocus)
			}
		})
	}
}

func TestModeState_CycleFocus(t *testing.T) {
	tests := []struct {
		name         string
		initialFocus Focus
		wantFocus    Focus
	}{
		{
			name:         "agent list to chat view",
			initialFocus: FocusAgentList,
			wantFocus:    FocusChatView,
		},
		{
			name:         "chat view to agent list",
			initialFocus: FocusChatView,
			wantFocus:    FocusAgentList,
		},
		{
			name:         "input line to agent list",
			initialFocus: FocusInputLine,
			wantFocus:    FocusAgentList,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := NewModeState()
			state.Focus = tt.initialFocus

			gotFocus, err := state.CycleFocus()
			if err != nil {
				t.Errorf("CycleFocus() unexpected error: %v", err)
			}
			if gotFocus != tt.wantFocus {
				t.Errorf("CycleFocus() = %v, want %v", gotFocus, tt.wantFocus)
			}
		})
	}

	// Test that cycling is blocked in non-normal modes
	t.Run("cycle blocked in input mode", func(t *testing.T) {
		state := NewModeState()
		state.Mode = ModeInput

		_, err := state.CycleFocus()
		if err == nil {
			t.Error("expected error when cycling focus in input mode")
		}
	})
}

func TestModeState_InputMode(t *testing.T) {
	state := NewModeState()

	// Enter input mode
	err := state.EnterInputMode()
	if err != nil {
		t.Errorf("EnterInputMode() unexpected error: %v", err)
	}
	if !state.IsInputting() {
		t.Error("expected IsInputting() to be true")
	}
	if state.Focus != FocusInputLine {
		t.Errorf("Focus = %v, want FocusInputLine", state.Focus)
	}

	// Double enter should fail
	err = state.EnterInputMode()
	if err != ErrAlreadyInMode {
		t.Errorf("expected ErrAlreadyInMode, got %v", err)
	}

	// Exit input mode
	err = state.ExitInputMode()
	if err != nil {
		t.Errorf("ExitInputMode() unexpected error: %v", err)
	}
	if state.IsInputting() {
		t.Error("expected IsInputting() to be false")
	}
	if state.Focus != FocusChatView {
		t.Errorf("Focus = %v, want FocusChatView", state.Focus)
	}

	// Double exit should fail
	err = state.ExitInputMode()
	if err != ErrInvalidModeTransition {
		t.Errorf("expected ErrInvalidModeTransition, got %v", err)
	}
}

func TestModeState_AbortConfirm(t *testing.T) {
	state := NewModeState()
	agentID := "agent-123"

	// Enter abort confirm
	err := state.EnterAbortConfirm(agentID)
	if err != nil {
		t.Errorf("EnterAbortConfirm() unexpected error: %v", err)
	}
	if !state.IsAbortConfirming() {
		t.Error("expected IsAbortConfirming() to be true")
	}
	if state.AbortAgentID != agentID {
		t.Errorf("AbortAgentID = %q, want %q", state.AbortAgentID, agentID)
	}

	// Confirm abort
	gotID, err := state.ConfirmAbort()
	if err != nil {
		t.Errorf("ConfirmAbort() unexpected error: %v", err)
	}
	if gotID != agentID {
		t.Errorf("ConfirmAbort() = %q, want %q", gotID, agentID)
	}
	if state.IsAbortConfirming() {
		t.Error("expected IsAbortConfirming() to be false after confirm")
	}
	if state.AbortAgentID != "" {
		t.Errorf("AbortAgentID should be empty after confirm, got %q", state.AbortAgentID)
	}
}

func TestModeState_CancelAbort(t *testing.T) {
	state := NewModeState()
	agentID := "agent-456"

	// Enter abort confirm
	_ = state.EnterAbortConfirm(agentID)

	// Cancel abort
	err := state.CancelAbort()
	if err != nil {
		t.Errorf("CancelAbort() unexpected error: %v", err)
	}
	if state.IsAbortConfirming() {
		t.Error("expected IsAbortConfirming() to be false after cancel")
	}
	if state.AbortAgentID != "" {
		t.Errorf("AbortAgentID should be empty after cancel, got %q", state.AbortAgentID)
	}
}

func TestModeState_AbortErrors(t *testing.T) {
	state := NewModeState()

	// Empty agent ID
	err := state.EnterAbortConfirm("")
	if err != ErrMissingAgentID {
		t.Errorf("expected ErrMissingAgentID, got %v", err)
	}

	// From input mode
	state.Mode = ModeInput
	err = state.EnterAbortConfirm("agent-789")
	if err != ErrInvalidModeTransition {
		t.Errorf("expected ErrInvalidModeTransition, got %v", err)
	}

	// Double enter
	state = NewModeState()
	_ = state.EnterAbortConfirm("agent-1")
	err = state.EnterAbortConfirm("agent-2")
	if err != ErrAlreadyInMode {
		t.Errorf("expected ErrAlreadyInMode, got %v", err)
	}

	// Cancel when not confirming
	state = NewModeState()
	err = state.CancelAbort()
	if err != ErrInvalidModeTransition {
		t.Errorf("expected ErrInvalidModeTransition, got %v", err)
	}

	// Confirm when not confirming
	_, err = state.ConfirmAbort()
	if err != ErrInvalidModeTransition {
		t.Errorf("expected ErrInvalidModeTransition, got %v", err)
	}
}

func TestModeState_PendingApprovals(t *testing.T) {
	state := NewModeState()

	if state.NeedsApproval() {
		t.Error("expected NeedsApproval() to be false initially")
	}

	state.SetPendingApprovals(true, false, false)
	if !state.NeedsApproval() {
		t.Error("expected NeedsApproval() to be true with pending permission")
	}

	// Second parameter (hasAction) is kept for API compatibility but ignored
	state.SetPendingApprovals(false, true, false)
	if state.NeedsApproval() {
		t.Error("expected NeedsApproval() to be false (hasAction is ignored)")
	}

	state.SetPendingApprovals(false, false, true)
	if !state.NeedsApproval() {
		t.Error("expected NeedsApproval() to be true with pending user question")
	}

	state.SetPendingApprovals(false, false, false)
	if state.NeedsApproval() {
		t.Error("expected NeedsApproval() to be false after clearing")
	}
}

func TestModeState_Validate(t *testing.T) {
	tests := []struct {
		name    string
		state   ModeState
		wantErr bool
	}{
		{
			name:    "valid normal mode",
			state:   ModeState{Mode: ModeNormal, Focus: FocusAgentList},
			wantErr: false,
		},
		{
			name:    "valid input mode",
			state:   ModeState{Mode: ModeInput, Focus: FocusInputLine},
			wantErr: false,
		},
		{
			name:    "valid abort mode",
			state:   ModeState{Mode: ModeAbortConfirm, AbortAgentID: "test"},
			wantErr: false,
		},
		{
			name:    "invalid abort mode - missing agent ID",
			state:   ModeState{Mode: ModeAbortConfirm, AbortAgentID: ""},
			wantErr: true,
		},
		{
			name:    "invalid normal mode - has agent ID",
			state:   ModeState{Mode: ModeNormal, AbortAgentID: "stale"},
			wantErr: true,
		},
		{
			name:    "invalid input mode - has agent ID",
			state:   ModeState{Mode: ModeInput, AbortAgentID: "stale"},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.state.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestMode_String(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{ModeNormal, "normal"},
		{ModeInput, "input"},
		{ModeAbortConfirm, "abort_confirm"},
		{Mode(99), "unknown"},
	}

	for _, tt := range tests {
		got := tt.mode.String()
		if got != tt.want {
			t.Errorf("Mode(%d).String() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

func TestFuzzyMatch(t *testing.T) {
	tests := []struct {
		text    string
		pattern string
		want    bool
	}{
		{"", "", true},
		{"hello", "", true},
		{"", "a", false},
		{"hello", "hlo", true},
		{"hello", "helo", true},
		{"hello", "hello", true},
		{"hello", "HELLO", false}, // case-sensitive at this level
		{"hello", "xyz", false},
		{"my-project", "mp", true},
		{"my-project", "mypro", true},
		{"my-project", "myproject", true},
		{"my-project", "my-pro", true},
		{"fab", "fb", true},
		{"fab", "fab", true},
		{"fab", "fa", true},
		{"fab", "ab", true},
		{"fab", "b", true},
		{"fab", "fba", false}, // order matters
	}

	for _, tt := range tests {
		got := fuzzyMatch(tt.text, tt.pattern)
		if got != tt.want {
			t.Errorf("fuzzyMatch(%q, %q) = %v, want %v", tt.text, tt.pattern, got, tt.want)
		}
	}
}

func TestFilterProjects(t *testing.T) {
	projects := []string{"Alpha", "Beta", "gamma", "AlphaBeta", "Zebra"}

	tests := []struct {
		filter string
		want   []string
	}{
		{"", projects},
		{"a", []string{"Alpha", "Beta", "gamma", "AlphaBeta", "Zebra"}}, // All contain 'a' (case-insensitive)
		{"al", []string{"Alpha", "AlphaBeta"}},
		{"ab", []string{"AlphaBeta"}}, // Only AlphaBeta has 'a' followed by 'b' in order
		{"g", []string{"gamma"}},
		{"xyz", nil},
		{"bet", []string{"Beta", "AlphaBeta"}}, // Both have b-e-t in order
	}

	for _, tt := range tests {
		got := filterProjects(projects, tt.filter)
		if len(got) != len(tt.want) {
			t.Errorf("filterProjects(%v, %q) = %v, want %v", projects, tt.filter, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("filterProjects(%v, %q) = %v, want %v", projects, tt.filter, got, tt.want)
				break
			}
		}
	}
}

func TestModeState_PlanProjectFilter(t *testing.T) {
	state := NewModeState()
	projects := []string{"Zebra", "Alpha", "Beta"}

	// Enter plan project select mode
	err := state.EnterPlanProjectSelect(projects)
	if err != nil {
		t.Fatalf("EnterPlanProjectSelect() error: %v", err)
	}

	// Initially all projects visible
	if len(state.PlanProjectFiltered) != 3 {
		t.Errorf("expected 3 filtered projects initially, got %d", len(state.PlanProjectFiltered))
	}
	if state.PlanProjectFilter != "" {
		t.Errorf("expected empty filter initially, got %q", state.PlanProjectFilter)
	}

	// Append filter characters
	state.PlanProjectAppendFilter('a')
	if state.PlanProjectFilter != "a" {
		t.Errorf("expected filter 'a', got %q", state.PlanProjectFilter)
	}
	// Alpha, Beta, Zebra all contain 'a' (case-insensitive)
	if len(state.PlanProjectFiltered) != 3 {
		t.Errorf("expected 3 filtered projects with 'a', got %d: %v", len(state.PlanProjectFiltered), state.PlanProjectFiltered)
	}

	state.PlanProjectAppendFilter('l')
	if state.PlanProjectFilter != "al" {
		t.Errorf("expected filter 'al', got %q", state.PlanProjectFilter)
	}
	// Only Alpha matches "al"
	if len(state.PlanProjectFiltered) != 1 || state.PlanProjectFiltered[0] != "Alpha" {
		t.Errorf("expected [Alpha] with 'al', got %v", state.PlanProjectFiltered)
	}

	// Backspace
	state.PlanProjectBackspaceFilter()
	if state.PlanProjectFilter != "a" {
		t.Errorf("expected filter 'a' after backspace, got %q", state.PlanProjectFilter)
	}
	if len(state.PlanProjectFiltered) != 3 {
		t.Errorf("expected 3 filtered projects after backspace, got %d", len(state.PlanProjectFiltered))
	}

	// Clear filter completely
	state.PlanProjectBackspaceFilter()
	if state.PlanProjectFilter != "" {
		t.Errorf("expected empty filter after backspace, got %q", state.PlanProjectFilter)
	}
	if len(state.PlanProjectFiltered) != 3 {
		t.Errorf("expected 3 filtered projects with empty filter, got %d", len(state.PlanProjectFiltered))
	}
}

func TestModeState_SelectPlanProjectFromFiltered(t *testing.T) {
	state := NewModeState()
	projects := []string{"Alpha", "Beta", "Gamma"}

	err := state.EnterPlanProjectSelect(projects)
	if err != nil {
		t.Fatalf("EnterPlanProjectSelect() error: %v", err)
	}

	// Filter to only show "Beta"
	state.PlanProjectSetFilter("bet")
	if len(state.PlanProjectFiltered) != 1 {
		t.Fatalf("expected 1 filtered project, got %d", len(state.PlanProjectFiltered))
	}

	// Select the filtered project
	project, err := state.SelectPlanProject()
	if err != nil {
		t.Errorf("SelectPlanProject() error: %v", err)
	}
	if project != "Beta" {
		t.Errorf("expected 'Beta', got %q", project)
	}
}

func TestModeState_SelectPlanProjectNoMatches(t *testing.T) {
	state := NewModeState()
	projects := []string{"Alpha", "Beta"}

	err := state.EnterPlanProjectSelect(projects)
	if err != nil {
		t.Fatalf("EnterPlanProjectSelect() error: %v", err)
	}

	// Filter to no matches
	state.PlanProjectSetFilter("xyz")
	if len(state.PlanProjectFiltered) != 0 {
		t.Fatalf("expected 0 filtered projects, got %d", len(state.PlanProjectFiltered))
	}

	// Try to select - should fail
	_, err = state.SelectPlanProject()
	if err == nil {
		t.Error("expected error when selecting with no matches")
	}
}
