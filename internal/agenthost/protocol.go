// Package agenthost defines the IPC protocol for agent host processes.
//
// The agent host is a lightweight process that wraps a Claude Code agent,
// allowing the daemon to restart and reattach to running agent subprocesses.
// The host maintains its own Unix socket for bidirectional communication.
//
// # Protocol Overview
//
// The protocol uses JSON-encoded request/response messaging over Unix sockets,
// following the same envelope pattern as the main daemon protocol.
//
// # Versioning
//
// The protocol includes a version field for compatibility checking.
// Clients should verify ProtocolVersion matches before proceeding.
// Breaking changes increment the major version; additive changes increment minor.
package agenthost

import (
	"encoding/json"
	"time"
)

// ProtocolVersion is the current agent host protocol version.
// Format: major.minor where major changes break compatibility.
const ProtocolVersion = "1.0"

// MinProtocolVersion is the minimum supported protocol version.
// Hosts and clients should reject connections with older versions.
const MinProtocolVersion = "1.0"

// MessageType identifies the type of agent host IPC message.
type MessageType string

const (
	// Host management
	MsgHostPing   MessageType = "host.ping"   // Health check and version info
	MsgHostStatus MessageType = "host.status" // Get detailed host and agent status

	// Agent listing
	MsgHostList MessageType = "host.list" // List all agents managed by this host

	// Stream management
	MsgHostAttach MessageType = "host.attach" // Attach to agent output stream
	MsgHostDetach MessageType = "host.detach" // Detach from agent output stream

	// Agent communication
	MsgHostSend MessageType = "host.send" // Send input to the agent

	// Lifecycle control
	MsgHostStop MessageType = "host.stop" // Gracefully stop the agent and host
)

// Request is the envelope for all agent host IPC requests.
type Request struct {
	Type    MessageType `json:"type"`
	ID      string      `json:"id,omitempty"`      // Optional request ID for correlation
	Payload any         `json:"payload,omitempty"` // Type-specific payload
}

// Response is the envelope for all agent host IPC responses.
type Response struct {
	Type    MessageType `json:"type"`
	ID      string      `json:"id,omitempty"` // Correlates with request ID
	Success bool        `json:"success"`
	Error   string      `json:"error,omitempty"`
	Payload any         `json:"payload,omitempty"` // Type-specific payload
}

// PingResponse is the payload for host.ping responses.
type PingResponse struct {
	Version         string    `json:"version"`          // Host process version
	ProtocolVersion string    `json:"protocol_version"` // IPC protocol version
	Uptime          string    `json:"uptime"`           // Human-readable uptime
	StartedAt       time.Time `json:"started_at"`       // When the host started
}

// StatusResponse is the payload for host.status responses.
type StatusResponse struct {
	Host  HostInfo  `json:"host"`
	Agent AgentInfo `json:"agent"`
}

// HostInfo contains host process status.
type HostInfo struct {
	PID             int       `json:"pid"`              // Host process ID
	Version         string    `json:"version"`          // Host version
	ProtocolVersion string    `json:"protocol_version"` // Protocol version
	StartedAt       time.Time `json:"started_at"`       // When host started
	SocketPath      string    `json:"socket_path"`      // Path to host socket
}

// AgentInfo contains agent subprocess status.
type AgentInfo struct {
	ID          string    `json:"id"`                    // Agent ID
	Project     string    `json:"project"`               // Project name
	State       string    `json:"state"`                 // starting, running, idle, done, error
	PID         int       `json:"pid"`                   // Agent subprocess PID (0 if not running)
	Worktree    string    `json:"worktree"`              // Worktree path
	StartedAt   time.Time `json:"started_at"`            // When agent started
	Task        string    `json:"task,omitempty"`        // Current task ID
	Description string    `json:"description,omitempty"` // Agent description
	Backend     string    `json:"backend,omitempty"`     // CLI backend (claude, codex)
}

// ListResponse is the payload for host.list responses.
type ListResponse struct {
	Agents []AgentInfo `json:"agents"`
}

// AttachRequest is the payload for host.attach requests.
type AttachRequest struct {
	Offset int64 `json:"offset,omitempty"` // Stream offset to resume from (0 = beginning)
}

// AttachResponse is the payload for host.attach responses.
type AttachResponse struct {
	AgentID      string `json:"agent_id"`      // Agent being attached to
	StreamOffset int64  `json:"stream_offset"` // Current stream position
}

// StreamEvent is sent to attached clients when agent output occurs.
// This mirrors the daemon StreamEvent but is specific to a single agent.
type StreamEvent struct {
	Type      string `json:"type"`                 // output, state, chat_entry, error
	AgentID   string `json:"agent_id"`             // Agent that produced the event
	Offset    int64  `json:"offset"`               // Stream position of this event
	Timestamp string `json:"timestamp"`            // RFC3339 timestamp
	Data      string `json:"data,omitempty"`       // For output events: raw output data
	State     string `json:"state,omitempty"`      // For state events: new state
	ChatEntry any    `json:"chat_entry,omitempty"` // For chat_entry events: parsed chat message
	Error     string `json:"error,omitempty"`      // For error events: error message
}

// SendRequest is the payload for host.send requests.
type SendRequest struct {
	Input string `json:"input"` // Raw input to send to agent
}

// StopRequest is the payload for host.stop requests.
type StopRequest struct {
	Force   bool   `json:"force,omitempty"`   // Force kill (SIGKILL) vs graceful (/quit)
	Timeout int    `json:"timeout,omitempty"` // Graceful shutdown timeout in seconds (default: 30)
	Reason  string `json:"reason,omitempty"`  // Optional reason for stopping
}

// StopResponse is the payload for host.stop responses.
type StopResponse struct {
	Stopped   bool   `json:"stopped"`             // Whether agent was successfully stopped
	ExitCode  int    `json:"exit_code,omitempty"` // Agent exit code if available
	Graceful  bool   `json:"graceful"`            // Whether shutdown was graceful
	Duration  string `json:"duration,omitempty"`  // How long shutdown took
	FinalState string `json:"final_state"`        // Final agent state before exit
}

// Helper functions for creating responses

// SuccessResponse creates a success response for a request.
func SuccessResponse(req *Request, payload any) *Response {
	return &Response{
		Type:    req.Type,
		ID:      req.ID,
		Success: true,
		Payload: payload,
	}
}

// ErrorResponse creates an error response for a request.
func ErrorResponse(req *Request, msg string) *Response {
	return &Response{
		Type:    req.Type,
		ID:      req.ID,
		Success: false,
		Error:   msg,
	}
}

// UnmarshalPayload converts a generic payload to a specific type.
// This handles the JSON round-trip needed because payloads arrive as map[string]any.
func UnmarshalPayload(payload any, dst any) error {
	if payload == nil {
		return nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// DecodePayload is a generic helper that decodes a response payload to type T.
func DecodePayload[T any](payload any) (*T, error) {
	var result T
	if err := UnmarshalPayload(payload, &result); err != nil {
		return nil, err
	}
	return &result, nil
}
