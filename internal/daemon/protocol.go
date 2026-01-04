// Package daemon provides the fab daemon server and IPC protocol.
package daemon

import "time"

// MessageType identifies the type of IPC message.
type MessageType string

const (
	// Server management
	MsgPing     MessageType = "ping"
	MsgShutdown MessageType = "shutdown"

	// Supervisor control
	MsgStart  MessageType = "start"  // Start orchestration for a project
	MsgStop   MessageType = "stop"   // Stop orchestration for a project
	MsgStatus MessageType = "status" // Get daemon/supervisor status

	// Project management
	MsgProjectAdd    MessageType = "project.add"
	MsgProjectRemove MessageType = "project.remove"
	MsgProjectList   MessageType = "project.list"
	MsgProjectSet    MessageType = "project.set"

	// Agent management
	MsgAgentList   MessageType = "agent.list"
	MsgAgentCreate MessageType = "agent.create"
	MsgAgentDelete MessageType = "agent.delete"
	MsgAgentInput  MessageType = "agent.input"  // Send input to agent PTY
	MsgAgentOutput MessageType = "agent.output" // Get buffered output from agent

	// TUI streaming
	MsgAttach MessageType = "attach" // Subscribe to agent output streams
	MsgDetach MessageType = "detach" // Unsubscribe from streams

	// Orchestrator (agent signals and staged actions)
	MsgAgentDone         MessageType = "agent.done"           // Agent signals task completion
	MsgListStagedActions MessageType = "orchestrator.actions" // Get pending actions for TUI
	MsgApproveAction     MessageType = "orchestrator.approve" // Approve a staged action
	MsgRejectAction      MessageType = "orchestrator.reject"  // Reject/skip a staged action
)

// Request is the envelope for all IPC requests.
type Request struct {
	Type    MessageType `json:"type"`
	ID      string      `json:"id,omitempty"`      // Optional request ID for correlation
	Payload any         `json:"payload,omitempty"` // Type-specific payload
}

// Response is the envelope for all IPC responses.
type Response struct {
	Type    MessageType `json:"type"`
	ID      string      `json:"id,omitempty"` // Correlates with request ID
	Success bool        `json:"success"`
	Error   string      `json:"error,omitempty"`
	Payload any         `json:"payload,omitempty"` // Type-specific payload
}

// PingResponse is the payload for ping responses.
type PingResponse struct {
	Version   string    `json:"version"`
	Uptime    string    `json:"uptime"`
	StartedAt time.Time `json:"started_at"`
}

// StartRequest is the payload for start requests.
type StartRequest struct {
	Project string `json:"project"`       // Project name, or empty for all
	All     bool   `json:"all,omitempty"` // Start all projects
}

// StopRequest is the payload for stop requests.
type StopRequest struct {
	Project string `json:"project"`       // Project name, or empty for all
	All     bool   `json:"all,omitempty"` // Stop all projects
}

// StatusResponse is the payload for status responses.
type StatusResponse struct {
	Daemon     DaemonStatus     `json:"daemon"`
	Supervisor SupervisorStatus `json:"supervisor"`
	Projects   []ProjectStatus  `json:"projects"`
}

// DaemonStatus contains daemon health info.
type DaemonStatus struct {
	Running   bool      `json:"running"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Version   string    `json:"version"`
}

// SupervisorStatus contains supervisor state.
type SupervisorStatus struct {
	ActiveProjects int `json:"active_projects"` // Projects with orchestration running
	TotalAgents    int `json:"total_agents"`
	RunningAgents  int `json:"running_agents"`
	IdleAgents     int `json:"idle_agents"`
}

// ProjectStatus contains per-project status info.
type ProjectStatus struct {
	Name         string        `json:"name"`
	Path         string        `json:"path"`
	Running      bool          `json:"running"` // Orchestration active
	MaxAgents    int           `json:"max_agents"`
	ActiveAgents int           `json:"active_agents"`
	Agents       []AgentStatus `json:"agents,omitempty"`
}

// AgentStatus contains per-agent status info.
type AgentStatus struct {
	ID        string    `json:"id"`
	Project   string    `json:"project"`
	State     string    `json:"state"` // starting, running, idle, done
	Worktree  string    `json:"worktree"`
	StartedAt time.Time `json:"started_at"`
	Task      string    `json:"task,omitempty"` // Current task ID if known
}

// ProjectAddRequest is the payload for project.add requests.
type ProjectAddRequest struct {
	Path      string `json:"path"`
	Name      string `json:"name,omitempty"`       // Optional override
	MaxAgents int    `json:"max_agents,omitempty"` // Default: 3
}

// ProjectAddResponse is the payload for project.add responses.
type ProjectAddResponse struct {
	Name      string   `json:"name"`
	Path      string   `json:"path"`
	MaxAgents int      `json:"max_agents"`
	Worktrees []string `json:"worktrees"` // Created worktree paths
}

// ProjectRemoveRequest is the payload for project.remove requests.
type ProjectRemoveRequest struct {
	Name            string `json:"name"`
	DeleteWorktrees bool   `json:"delete_worktrees,omitempty"` // Clean up worktrees
}

// ProjectListResponse is the payload for project.list responses.
type ProjectListResponse struct {
	Projects []ProjectInfo `json:"projects"`
}

// ProjectInfo contains basic project info for listing.
type ProjectInfo struct {
	Name      string `json:"name"`
	Path      string `json:"path"`
	MaxAgents int    `json:"max_agents"`
	Running   bool   `json:"running"`
}

// ProjectSetRequest is the payload for project.set requests.
type ProjectSetRequest struct {
	Name      string `json:"name"`
	MaxAgents *int   `json:"max_agents,omitempty"` // Pointer to distinguish unset from zero
}

// AgentCreateRequest is the payload for agent.create requests.
type AgentCreateRequest struct {
	Project string `json:"project"`
	Task    string `json:"task,omitempty"` // Optional initial task
}

// AgentCreateResponse is the payload for agent.create responses.
type AgentCreateResponse struct {
	ID       string `json:"id"`
	Project  string `json:"project"`
	Worktree string `json:"worktree"`
}

// AgentDeleteRequest is the payload for agent.delete requests.
type AgentDeleteRequest struct {
	ID    string `json:"id"`
	Force bool   `json:"force,omitempty"` // Force kill if running
}

// AgentListRequest is the payload for agent.list requests.
type AgentListRequest struct {
	Project string `json:"project,omitempty"` // Filter by project
}

// AgentListResponse is the payload for agent.list responses.
type AgentListResponse struct {
	Agents []AgentStatus `json:"agents"`
}

// AgentInputRequest is the payload for agent.input requests.
type AgentInputRequest struct {
	ID    string `json:"id"`
	Input string `json:"input"` // Raw input to send to PTY
}

// AgentOutputRequest is the payload for agent.output requests.
type AgentOutputRequest struct {
	ID string `json:"id"`
}

// AgentOutputResponse is the payload for agent.output responses.
type AgentOutputResponse struct {
	ID     string `json:"id"`
	Output string `json:"output"` // Buffered PTY output
}

// AttachRequest is the payload for attach requests.
type AttachRequest struct {
	Projects []string `json:"projects,omitempty"` // Filter by projects, empty = all
}

// StreamEvent is sent to attached clients when agent output occurs.
type StreamEvent struct {
	Type    string `json:"type"` // "output", "state", "created", "deleted"
	AgentID string `json:"agent_id"`
	Project string `json:"project"`
	Data    string `json:"data,omitempty"`  // For output events
	State   string `json:"state,omitempty"` // For state events
}

// ActionType identifies the type of staged orchestrator action.
type ActionType string

const (
	// ActionSendMessage sends a message to an agent's PTY.
	ActionSendMessage ActionType = "send_message"

	// ActionQuit sends /quit to gracefully end the agent session.
	ActionQuit ActionType = "quit"
)

// StagedAction represents an orchestrator action pending user approval.
type StagedAction struct {
	ID        string     `json:"id"`
	AgentID   string     `json:"agent_id"`
	Project   string     `json:"project"`
	Type      ActionType `json:"type"`
	Payload   string     `json:"payload,omitempty"` // Action-specific data (e.g., message text)
	CreatedAt time.Time  `json:"created_at"`
}

// AgentDoneRequest is the payload for agent.done requests.
// Sent by agents to signal task completion.
type AgentDoneRequest struct {
	Reason string `json:"reason,omitempty"` // Optional completion reason
}

// StagedActionsRequest is the payload for orchestrator.actions requests.
type StagedActionsRequest struct {
	Project string `json:"project,omitempty"` // Filter by project, empty = all
}

// StagedActionsResponse is the payload for orchestrator.actions responses.
type StagedActionsResponse struct {
	Actions []StagedAction `json:"actions"`
}

// ApproveActionRequest is the payload for orchestrator.approve requests.
type ApproveActionRequest struct {
	ActionID string `json:"action_id"`
}

// RejectActionRequest is the payload for orchestrator.reject requests.
type RejectActionRequest struct {
	ActionID string `json:"action_id"`
	Reason   string `json:"reason,omitempty"` // Optional rejection reason
}
