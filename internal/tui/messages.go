package tui

import (
	"time"

	"github.com/tessro/fab/internal/daemon"
)

// StreamEventMsg wraps a daemon stream event for Bubble Tea.
type StreamEventMsg struct {
	Event *daemon.StreamEvent
	Err   error
}

// AgentListMsg contains updated agent list from daemon.
type AgentListMsg struct {
	Agents []daemon.AgentStatus
	Err    error
}

// AgentInputMsg is the result of sending input to an agent.
type AgentInputMsg struct {
	Err error
}

// AgentChatHistoryMsg contains chat history fetched for an agent.
type AgentChatHistoryMsg struct {
	AgentID string
	Entries []daemon.ChatEntryDTO
	Err     error
}

// StagedActionsMsg contains pending actions that need user approval.
type StagedActionsMsg struct {
	Actions []daemon.StagedAction
	Err     error
}

// StatsMsg contains aggregated session statistics.
type StatsMsg struct {
	Stats *daemon.StatsResponse
	Err   error
}

// ActionResultMsg is the result of approving/rejecting an action.
type ActionResultMsg struct {
	Err error
}

// PermissionResultMsg is the result of responding to a permission request.
type PermissionResultMsg struct {
	Err error
}

// UserQuestionResultMsg is the result of responding to a user question.
type UserQuestionResultMsg struct {
	QuestionID string
	Err        error
}

// AbortResultMsg is the result of aborting an agent.
type AbortResultMsg struct {
	Err error
}

// ProjectListMsg contains the list of projects for plan mode.
type ProjectListMsg struct {
	Projects []string
	Err      error
}

// PlanStartResultMsg is the result of starting a planner.
type PlanStartResultMsg struct {
	PlannerID string
	Project   string
	Err       error
}

// tickMsg is sent on regular intervals to drive spinner animation.
type tickMsg time.Time

// clearErrorMsg is sent to clear the error display after a timeout.
type clearErrorMsg struct{}

// StreamStartMsg is sent when the event stream is started successfully.
type StreamStartMsg struct {
	EventChan <-chan daemon.EventResult
}

// reconnectMsg signals the result of a reconnection attempt.
type reconnectMsg struct {
	Success   bool
	Err       error
	EventChan <-chan daemon.EventResult
}

// UsageUpdateMsg contains updated usage statistics.
type UsageUpdateMsg struct {
	Percent   int
	Remaining time.Duration
	Err       error
}
