// Package daemon provides the fab daemon server and IPC protocol.
package daemon

import (
	"encoding/json"
	"time"
)

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
	MsgAttach             MessageType = "attach"               // Subscribe to agent output streams
	MsgDetach             MessageType = "detach"               // Unsubscribe from streams
	MsgAgentSendMessage   MessageType = "agent.send_message"
	MsgAgentChatHistory   MessageType = "agent.chat_history"   // Get chat history for an agent

	// Orchestrator (agent signals and staged actions)
	MsgAgentDone         MessageType = "agent.done"           // Agent signals task completion
	MsgListStagedActions MessageType = "orchestrator.actions" // Get pending actions for TUI
	MsgApproveAction     MessageType = "orchestrator.approve" // Approve a staged action
	MsgRejectAction      MessageType = "orchestrator.reject"  // Reject/skip a staged action

	// Permission handling (Claude Code hook callbacks)
	MsgPermissionRequest MessageType = "permission.request" // Hook requests permission decision
	MsgPermissionRespond MessageType = "permission.respond" // TUI responds to permission request
	MsgPermissionList    MessageType = "permission.list"    // List pending permission requests

	// Ticket claims (prevent duplicate work across agents)
	MsgAgentClaim MessageType = "agent.claim" // Claim a ticket for an agent
	MsgClaimList  MessageType = "claim.list"  // List all active claims
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
	RemoteURL    string        `json:"remote_url"`
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
	RemoteURL string `json:"remote_url"`           // Git remote URL
	Name      string `json:"name,omitempty"`       // Optional override
	MaxAgents int    `json:"max_agents,omitempty"` // Default: 3
}

// ProjectAddResponse is the payload for project.add responses.
type ProjectAddResponse struct {
	Name      string   `json:"name"`
	RemoteURL string   `json:"remote_url"`
	RepoDir   string   `json:"repo_dir"` // Local clone path
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
	RemoteURL string `json:"remote_url"`
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

// AgentSendMessageRequest is the payload for agent.send_message requests.
type AgentSendMessageRequest struct {
	ID      string `json:"id"`      // Agent ID
	Content string `json:"content"` // Message text to send
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

// AgentChatHistoryRequest is the payload for agent.chat_history requests.
type AgentChatHistoryRequest struct {
	ID    string `json:"id"`              // Agent ID
	Limit int    `json:"limit,omitempty"` // Max entries to return (0 = all)
}

// AgentChatHistoryResponse is the payload for agent.chat_history responses.
type AgentChatHistoryResponse struct {
	AgentID string         `json:"agent_id"`
	Entries []ChatEntryDTO `json:"entries"`
}

// StreamEvent is sent to attached clients when agent output occurs.
type StreamEvent struct {
	Type              string             `json:"type"` // "output", "state", "created", "deleted", "permission_request"
	AgentID           string             `json:"agent_id"`
	Project           string             `json:"project"`
	Data              string             `json:"data,omitempty"`               // For output events
	State             string             `json:"state,omitempty"`              // For state events
	ChatEntry         *ChatEntryDTO      `json:"chat_entry,omitempty"`         // For "chat_entry" events
	PermissionRequest *PermissionRequest `json:"permission_request,omitempty"` // For "permission_request" events
}

// ChatEntryDTO is the wire format for chat entries sent to TUI clients
type ChatEntryDTO struct {
	Role       string `json:"role"`                  // "assistant", "user", "tool"
	Content    string `json:"content,omitempty"`     // Text content
	ToolName   string `json:"tool_name,omitempty"`   // Tool name (e.g., "Bash")
	ToolInput  string `json:"tool_input,omitempty"`  // Tool input summary
	ToolResult string `json:"tool_result,omitempty"` // Tool output
	Timestamp  string `json:"timestamp"`             // RFC3339 format
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
	AgentID string `json:"agent_id,omitempty"` // Agent ID (from FAB_AGENT_ID env)
	TaskID  string `json:"task_id,omitempty"`  // Task ID that was completed
	Error   string `json:"error,omitempty"`    // Error message if task failed
}

// AgentDoneResponse is the payload for agent.done responses.
type AgentDoneResponse struct {
	Merged     bool   `json:"merged"`                 // True if merge to main succeeded
	BranchName string `json:"branch_name,omitempty"`  // The branch that was processed
	MergeError string `json:"merge_error,omitempty"`  // Conflict message if merge failed
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

// PermissionRequest represents a tool permission request from Claude Code.
// Sent by the fab hook command when Claude Code needs permission to use a tool.
type PermissionRequest struct {
	ID          string          `json:"id"`                    // Unique request ID (generated by daemon)
	AgentID     string          `json:"agent_id"`              // FAB_AGENT_ID from environment
	Project     string          `json:"project"`               // Project name
	ToolName    string          `json:"tool_name"`             // Tool requesting permission (e.g., "Bash")
	ToolInput   json.RawMessage `json:"tool_input"`            // Raw tool input arguments
	ToolUseID   string          `json:"tool_use_id,omitempty"` // Claude's tool_use_id for correlation
	RequestedAt time.Time       `json:"requested_at"`          // When the request was made
}

// PermissionResponse is the decision for a permission request.
type PermissionResponse struct {
	ID        string `json:"id"`                // Matches PermissionRequest.ID
	Behavior  string `json:"behavior"`          // "allow" or "deny"
	Message   string `json:"message,omitempty"` // Optional message (shown on deny)
	Interrupt bool   `json:"interrupt"`         // If true, stop Claude entirely
}

// PermissionRequestPayload is the payload from the fab hook command.
type PermissionRequestPayload struct {
	AgentID   string          `json:"agent_id"`
	ToolName  string          `json:"tool_name"`
	ToolInput json.RawMessage `json:"tool_input"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
}

// PermissionRespondPayload is the payload for permission.respond requests.
type PermissionRespondPayload struct {
	ID        string `json:"id"`                // Permission request ID
	Behavior  string `json:"behavior"`          // "allow" or "deny"
	Message   string `json:"message,omitempty"` // Optional denial message
	Interrupt bool   `json:"interrupt"`         // Stop Claude entirely
}

// PermissionListRequest is the payload for permission.list requests.
type PermissionListRequest struct {
	Project string `json:"project,omitempty"` // Filter by project, empty = all
}

// PermissionListResponse is the payload for permission.list responses.
type PermissionListResponse struct {
	Requests []PermissionRequest `json:"requests"`
}

// AgentClaimRequest is the payload for agent.claim requests.
type AgentClaimRequest struct {
	AgentID  string `json:"agent_id"`  // Agent ID (from FAB_AGENT_ID env)
	TicketID string `json:"ticket_id"` // Ticket to claim
}

// ClaimListRequest is the payload for claim.list requests.
type ClaimListRequest struct {
	Project string `json:"project,omitempty"` // Filter by project, empty = all
}

// ClaimListResponse is the payload for claim.list responses.
type ClaimListResponse struct {
	Claims []ClaimInfo `json:"claims"`
}

// ClaimInfo describes a single ticket claim.
type ClaimInfo struct {
	TicketID string `json:"ticket_id"`
	AgentID  string `json:"agent_id"`
	Project  string `json:"project"`
}
