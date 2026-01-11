// Package processagent provides shared process lifecycle management for Claude Code instances.
// It extracts common patterns from manager and planner packages to reduce code duplication.
package processagent

// State represents the process agent state.
type State string

const (
	StateStopped  State = "stopped"
	StateStarting State = "starting"
	StateRunning  State = "running"
	StateStopping State = "stopping"
)
