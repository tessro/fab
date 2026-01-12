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
	MsgProjectAdd        MessageType = "project.add"
	MsgProjectRemove     MessageType = "project.remove"
	MsgProjectList       MessageType = "project.list"
	MsgProjectSet        MessageType = "project.set"         // Deprecated: use project.config.*
	MsgProjectConfigShow MessageType = "project.config.show" // Show all config for a project
	MsgProjectConfigGet  MessageType = "project.config.get"  // Get a single config value
	MsgProjectConfigSet  MessageType = "project.config.set"  // Set a single config value

	// Agent management
	MsgAgentList     MessageType = "agent.list"
	MsgAgentCreate   MessageType = "agent.create"
	MsgAgentDelete   MessageType = "agent.delete"
	MsgAgentAbort    MessageType = "agent.abort"    // Abort/kill a running agent
	MsgAgentInput    MessageType = "agent.input"    // Send input to agent
	MsgAgentOutput   MessageType = "agent.output"   // Get buffered output from agent
	MsgAgentDescribe MessageType = "agent.describe" // Set agent description
	MsgAgentIdle     MessageType = "agent.idle"     // Agent signals it has gone idle (Stop hook)

	// TUI streaming
	MsgAttach           MessageType = "attach" // Subscribe to agent output streams
	MsgDetach           MessageType = "detach" // Unsubscribe from streams
	MsgAgentSendMessage MessageType = "agent.send_message"
	MsgAgentChatHistory MessageType = "agent.chat_history" // Get chat history for an agent

	// Orchestrator (agent signals)
	MsgAgentDone MessageType = "agent.done" // Agent signals task completion

	// Permission handling (Claude Code hook callbacks)
	MsgPermissionRequest MessageType = "permission.request" // Hook requests permission decision
	MsgPermissionRespond MessageType = "permission.respond" // TUI responds to permission request
	MsgPermissionList    MessageType = "permission.list"    // List pending permission requests

	// User question handling (Claude Code AskUserQuestion tool)
	MsgUserQuestionRequest MessageType = "question.request" // Hook requests user answer
	MsgUserQuestionRespond MessageType = "question.respond" // TUI responds to user question

	// Ticket claims (prevent duplicate work across agents)
	MsgAgentClaim MessageType = "agent.claim" // Claim a ticket for an agent
	MsgClaimList  MessageType = "claim.list"  // List all active claims

	// Commit tracking
	MsgCommitList MessageType = "commit.list" // List recent commits

	// Stats
	MsgStats MessageType = "stats" // Get aggregated session statistics

	// Manager agent (interactive user conversation)
	MsgManagerStart       MessageType = "manager.start"        // Start the manager agent
	MsgManagerStop        MessageType = "manager.stop"         // Stop the manager agent
	MsgManagerStatus      MessageType = "manager.status"       // Get manager status
	MsgManagerSendMessage  MessageType = "manager.send_message"  // Send message to manager
	MsgManagerChatHistory  MessageType = "manager.chat_history"  // Get manager chat history
	MsgManagerClearHistory MessageType = "manager.clear_history" // Clear manager chat history

	// Planning agents (implementation planning mode)
	MsgPlanStart       MessageType = "plan.start"        // Start a planning agent
	MsgPlanStop        MessageType = "plan.stop"         // Stop a planning agent
	MsgPlanList        MessageType = "plan.list"         // List planning agents
	MsgPlanSendMessage MessageType = "plan.send_message" // Send message to planner
	MsgPlanChatHistory MessageType = "plan.chat_history" // Get planner chat history
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
	ID          string    `json:"id"`
	Project     string    `json:"project"`
	State       string    `json:"state"` // starting, running, idle, done
	Worktree    string    `json:"worktree"`
	StartedAt   time.Time `json:"started_at"`
	Task        string    `json:"task,omitempty"`        // Current task ID if known
	Description string    `json:"description,omitempty"` // Human-readable description
	Backend     string    `json:"backend,omitempty"`     // CLI backend name (e.g., "claude", "codex")
}

// ProjectAddRequest is the payload for project.add requests.
type ProjectAddRequest struct {
	RemoteURL string `json:"remote_url"`           // Git remote URL
	Name      string `json:"name,omitempty"`       // Optional override
	MaxAgents int    `json:"max_agents,omitempty"` // Default: 3
	Autostart bool   `json:"autostart,omitempty"`  // Start orchestration when daemon starts
	Backend   string `json:"backend,omitempty"`    // Agent backend (claude/codex)
}

// ProjectAddResponse is the payload for project.add responses.
type ProjectAddResponse struct {
	Name      string `json:"name"`
	RemoteURL string `json:"remote_url"`
	RepoDir   string `json:"repo_dir"` // Local clone path
	MaxAgents int    `json:"max_agents"`
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
	Backend   string `json:"backend"` // Agent backend (claude/codex)
}

// ProjectSetRequest is the payload for project.set requests.
// Deprecated: Use ProjectConfigSetRequest instead.
type ProjectSetRequest struct {
	Name      string `json:"name"`
	MaxAgents *int   `json:"max_agents,omitempty"` // Pointer to distinguish unset from zero
	Autostart *bool  `json:"autostart,omitempty"`  // Pointer to distinguish unset from false
}

// ProjectConfigShowRequest is the payload for project.config.show requests.
type ProjectConfigShowRequest struct {
	Name string `json:"name"` // Project name
}

// ProjectConfigShowResponse is the payload for project.config.show responses.
type ProjectConfigShowResponse struct {
	Name   string         `json:"name"`   // Project name
	Config map[string]any `json:"config"` // Config key-value pairs
}

// ProjectConfigGetRequest is the payload for project.config.get requests.
type ProjectConfigGetRequest struct {
	Name string `json:"name"` // Project name
	Key  string `json:"key"`  // Config key
}

// ProjectConfigGetResponse is the payload for project.config.get responses.
type ProjectConfigGetResponse struct {
	Name  string `json:"name"`  // Project name
	Key   string `json:"key"`   // Config key
	Value any    `json:"value"` // Config value
}

// ProjectConfigSetRequest is the payload for project.config.set requests.
type ProjectConfigSetRequest struct {
	Name  string `json:"name"`  // Project name
	Key   string `json:"key"`   // Config key
	Value string `json:"value"` // Config value (as string, will be parsed based on key type)
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

// AgentAbortRequest is the payload for agent.abort requests.
type AgentAbortRequest struct {
	ID    string `json:"id"`
	Force bool   `json:"force,omitempty"` // Force kill immediately (SIGKILL vs graceful /quit)
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
	Input string `json:"input"` // Raw input to send to agent
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
	Output string `json:"output"` // Buffered agent output
}

// AgentDescribeRequest is the payload for agent.describe requests.
type AgentDescribeRequest struct {
	AgentID     string `json:"agent_id,omitempty"` // Agent ID (from FAB_AGENT_ID env, optional)
	Description string `json:"description"`        // Human-readable description of current work
}

// AgentIdleRequest is the payload for agent.idle requests.
// Sent by the Stop hook when Claude Code finishes responding.
type AgentIdleRequest struct {
	AgentID string `json:"agent_id,omitempty"` // Agent ID (from FAB_AGENT_ID env)
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
	Type              string             `json:"type"` // "output", "state", "created", "deleted", "info", "permission_request", "user_question", "intervention", "manager_chat_entry", "manager_state"
	AgentID           string             `json:"agent_id"`
	Project           string             `json:"project"`
	Data              string             `json:"data,omitempty"`               // For output events
	State             string             `json:"state,omitempty"`              // For state events
	StartedAt         string             `json:"started_at,omitempty"`         // For created events (RFC3339)
	Task              string             `json:"task,omitempty"`               // For "info" events (issue/ticket ID)
	Description       string             `json:"description,omitempty"`        // For "info" events (agent description)
	Backend           string             `json:"backend,omitempty"`            // For "created", "planner_created" events
	ChatEntry         *ChatEntryDTO      `json:"chat_entry,omitempty"`         // For "chat_entry" events
	PermissionRequest *PermissionRequest `json:"permission_request,omitempty"` // For "permission_request" events
	UserQuestion      *UserQuestion      `json:"user_question,omitempty"`      // For "user_question" events
	Intervening       *bool              `json:"intervening,omitempty"`        // For "intervention" events (user is intervening)
	ManagerState      string             `json:"manager_state,omitempty"`      // For "manager_state" events
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

// AgentDoneRequest is the payload for agent.done requests.
// Sent by agents to signal task completion.
type AgentDoneRequest struct {
	AgentID string `json:"agent_id,omitempty"` // Agent ID (from FAB_AGENT_ID env)
	TaskID  string `json:"task_id,omitempty"`  // Task ID that was completed
	Error   string `json:"error,omitempty"`    // Error message if task failed
}

// AgentDoneResponse is the payload for agent.done responses.
type AgentDoneResponse struct {
	Merged     bool   `json:"merged"`                // True if merge to main succeeded
	BranchName string `json:"branch_name,omitempty"` // The branch that was processed
	SHA        string `json:"sha,omitempty"`         // Commit SHA of merge commit (only if Merged is true)
	MergeError string `json:"merge_error,omitempty"` // Conflict message if merge failed
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

// UserQuestion represents a question from Claude Code's AskUserQuestion tool.
// Sent by the fab hook command when Claude Code needs user input.
type UserQuestion struct {
	ID          string             `json:"id"`           // Unique request ID (generated by daemon)
	AgentID     string             `json:"agent_id"`     // FAB_AGENT_ID from environment
	Project     string             `json:"project"`      // Project name
	Questions   []QuestionItem     `json:"questions"`    // Questions to present to user
	RequestedAt time.Time          `json:"requested_at"` // When the request was made
}

// QuestionItem represents a single question with options.
type QuestionItem struct {
	Question    string           `json:"question"`     // The full question text
	Header      string           `json:"header"`       // Short label (max 12 chars) for display
	MultiSelect bool             `json:"multiSelect"`  // Allow multiple selections
	Options     []QuestionOption `json:"options"`      // Available choices (2-4 options)
}

// QuestionOption represents a single option for a question.
type QuestionOption struct {
	Label       string `json:"label"`       // Short display text (1-5 words)
	Description string `json:"description"` // Explanation of what this option means
}

// UserQuestionResponse is the user's answer to a question.
type UserQuestionResponse struct {
	ID      string            `json:"id"`      // Matches UserQuestion.ID
	Answers map[string]string `json:"answers"` // Header -> selected option label(s)
}

// UserQuestionRequestPayload is the payload from the fab hook command.
type UserQuestionRequestPayload struct {
	AgentID   string         `json:"agent_id"`
	Questions []QuestionItem `json:"questions"`
}

// UserQuestionRespondPayload is the payload for question.respond requests.
type UserQuestionRespondPayload struct {
	ID      string            `json:"id"`      // Question request ID
	Answers map[string]string `json:"answers"` // Header -> selected option label(s)
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

// CommitListRequest is the payload for commit.list requests.
type CommitListRequest struct {
	Project string `json:"project,omitempty"` // Filter by project, empty = all
	Limit   int    `json:"limit,omitempty"`   // Max commits to return, 0 = all
}

// CommitListResponse is the payload for commit.list responses.
type CommitListResponse struct {
	Commits []CommitInfo `json:"commits"`
}

// CommitInfo describes a merged commit.
type CommitInfo struct {
	SHA      string `json:"sha"`
	Branch   string `json:"branch"`
	AgentID  string `json:"agent_id"`
	TaskID   string `json:"task_id,omitempty"`
	Project  string `json:"project"`
	MergedAt string `json:"merged_at"` // RFC3339 format
}

// StatsRequest is the payload for stats requests.
type StatsRequest struct {
	Project string `json:"project,omitempty"` // Filter by project, empty = all
}

// StatsResponse contains aggregated session statistics.
type StatsResponse struct {
	// Session stats (accumulated since daemon start)
	CommitCount int `json:"commit_count"` // Total commits merged this session

	// Usage stats (current billing window)
	Usage UsageStats `json:"usage"`
}

// UsageStats contains token usage for the current billing window.
type UsageStats struct {
	OutputTokens int64  `json:"output_tokens"`
	Percent      int    `json:"percent"`    // Usage as percentage (0-100+)
	WindowEnd    string `json:"window_end"` // RFC3339 format
	TimeLeft     string `json:"time_left"`  // Human-readable time remaining
	PlanLimit    int64  `json:"plan_limit"` // Output token limit for current plan
	Plan         string `json:"plan"`       // "pro" or "max"
}

// ManagerStartRequest is the payload for manager.start requests.
type ManagerStartRequest struct {
	Project string `json:"project"` // Project name (required)
}

// ManagerStopRequest is the payload for manager.stop requests.
type ManagerStopRequest struct {
	Project string `json:"project"` // Project name (required)
}

// ManagerStatusRequest is the payload for manager.status requests.
type ManagerStatusRequest struct {
	Project string `json:"project"` // Project name (required)
}

// ManagerStatusResponse is the payload for manager.status responses.
type ManagerStatusResponse struct {
	Project   string `json:"project"`
	Running   bool   `json:"running"`
	State     string `json:"state"`      // "stopped", "starting", "running", "stopping"
	StartedAt string `json:"started_at"` // RFC3339 format, empty if not running
	WorkDir   string `json:"workdir"`    // Working directory (worktree path)
}

// ManagerSendMessageRequest is the payload for manager.send_message requests.
type ManagerSendMessageRequest struct {
	Project string `json:"project"` // Project name (required)
	Content string `json:"content"`
}

// ManagerChatHistoryRequest is the payload for manager.chat_history requests.
type ManagerChatHistoryRequest struct {
	Project string `json:"project"`         // Project name (required)
	Limit   int    `json:"limit,omitempty"` // Max entries to return (0 = all)
}

// ManagerChatHistoryResponse is the payload for manager.chat_history responses.
type ManagerChatHistoryResponse struct {
	Project string         `json:"project"`
	Entries []ChatEntryDTO `json:"entries"`
}

// ManagerClearHistoryRequest is the payload for manager.clear_history requests.
type ManagerClearHistoryRequest struct {
	Project string `json:"project"` // Project name (required)
}

// PlanStartRequest is the payload for plan.start requests.
type PlanStartRequest struct {
	Project string `json:"project,omitempty"` // Optional project name (uses project's worktree)
	Prompt  string `json:"prompt"`            // Planning task description
}

// PlanStartResponse is the payload for plan.start responses.
type PlanStartResponse struct {
	ID      string `json:"id"`      // Planner ID
	Project string `json:"project"` // Project name (empty if no project)
	WorkDir string `json:"workdir"` // Working directory
}

// PlanStopRequest is the payload for plan.stop requests.
type PlanStopRequest struct {
	ID string `json:"id"` // Planner ID
}

// PlanListRequest is the payload for plan.list requests.
type PlanListRequest struct {
	Project string `json:"project,omitempty"` // Filter by project
}

// PlanListResponse is the payload for plan.list responses.
type PlanListResponse struct {
	Planners []PlannerStatus `json:"planners"`
}

// PlannerStatus contains planner agent status info.
type PlannerStatus struct {
	ID          string `json:"id"`
	Project     string `json:"project"`
	State       string `json:"state"` // "stopped", "starting", "running", "stopping"
	WorkDir     string `json:"workdir"`
	StartedAt   string `json:"started_at"` // RFC3339 format
	PlanFile    string `json:"plan_file,omitempty"`   // Path to generated plan (if complete)
	Description string `json:"description,omitempty"` // User-set description
	Backend     string `json:"backend,omitempty"`     // CLI backend name (e.g., "claude", "codex")
}

// PlanSendMessageRequest is the payload for plan.send_message requests.
type PlanSendMessageRequest struct {
	ID      string `json:"id"`      // Planner ID
	Content string `json:"content"` // Message text
}

// PlanChatHistoryRequest is the payload for plan.chat_history requests.
type PlanChatHistoryRequest struct {
	ID    string `json:"id"`              // Planner ID
	Limit int    `json:"limit,omitempty"` // Max entries to return (0 = all)
}

// PlanChatHistoryResponse is the payload for plan.chat_history responses.
type PlanChatHistoryResponse struct {
	PlannerID string         `json:"planner_id"`
	Entries   []ChatEntryDTO `json:"entries"`
}
