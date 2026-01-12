package tui

import (
	"time"

	"github.com/tessro/fab/internal/daemon"
)

// streamEventMsg wraps a daemon stream event for Bubble Tea.
type streamEventMsg struct {
	Event *daemon.StreamEvent
	Err   error
}

// agentListMsg contains updated agent list from daemon.
type agentListMsg struct {
	Agents []daemon.AgentStatus
	Err    error
}

// agentInputMsg is the result of sending input to an agent.
type agentInputMsg struct {
	Err error
}

// agentChatHistoryMsg contains chat history fetched for an agent.
type agentChatHistoryMsg struct {
	AgentID string
	Entries []daemon.ChatEntryDTO
	Err     error
}

// statsMsg contains aggregated session statistics.
type statsMsg struct {
	Stats *daemon.StatsResponse
	Err   error
}

// commitListMsg contains recent commits for the recent work section.
type commitListMsg struct {
	Commits []daemon.CommitInfo
	Err     error
}

// permissionResultMsg is the result of responding to a permission request.
type permissionResultMsg struct {
	Err error
}

// userQuestionResultMsg is the result of responding to a user question.
type userQuestionResultMsg struct {
	QuestionID string
	Err        error
}

// abortResultMsg is the result of aborting an agent.
type abortResultMsg struct {
	Err error
}

// projectListMsg contains the list of projects for plan mode.
type projectListMsg struct {
	Projects []string
	Err      error
}

// planStartResultMsg is the result of starting a planner.
type planStartResultMsg struct {
	PlannerID string
	Project   string
	Err       error
}

// tickMsg is sent on regular intervals to drive spinner animation.
type tickMsg time.Time

// clearErrorMsg is sent to clear the error display after a timeout.
type clearErrorMsg struct{}

// streamStartMsg is sent when the event stream is started successfully.
type streamStartMsg struct {
	EventChan <-chan daemon.EventResult
}

// reconnectMsg signals the result of a reconnection attempt.
type reconnectMsg struct {
	Success   bool
	Err       error
	EventChan <-chan daemon.EventResult
}

// usageUpdateMsg contains updated usage statistics.
type usageUpdateMsg struct {
	Percent   int
	Remaining time.Duration
	Err       error
}
