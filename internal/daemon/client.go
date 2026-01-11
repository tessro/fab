// Package daemon provides the fab daemon server and IPC protocol.
package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Client connects to the fab daemon over Unix socket.
type Client struct {
	socketPath string

	mu sync.Mutex
	// +checklocks:mu
	conn net.Conn
	// +checklocks:mu
	encoder *json.Encoder
	// +checklocks:mu
	decoder *json.Decoder
	// +checklocks:mu
	attached bool

	// ioMu serializes all I/O operations (encode/decode).
	// This prevents concurrent access to the encoder/decoder which can cause panics.
	// Must be acquired AFTER mu if both are needed.
	ioMu sync.Mutex

	reqID atomic.Uint64

	// Event streaming via dedicated connection
	eventMu sync.Mutex
	// +checklocks:eventMu
	eventConn net.Conn
	// +checklocks:eventMu
	eventDone chan struct{}
}

// NewClient creates a new daemon client.
func NewClient(socketPath string) *Client {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	return &Client{
		socketPath: socketPath,
	}
}

// ConnectTimeout is the default timeout for connecting to the daemon.
const ConnectTimeout = 5 * time.Second

// RequestTimeout is the default timeout for request/response operations.
const RequestTimeout = 30 * time.Second

// Connect establishes a connection to the daemon.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return nil // Already connected
	}

	conn, err := net.DialTimeout("unix", c.socketPath, ConnectTimeout)
	if err != nil {
		return fmt.Errorf("dial daemon: %w", err)
	}

	c.conn = conn
	c.encoder = json.NewEncoder(conn)
	c.decoder = json.NewDecoder(conn)
	return nil
}

// Close closes the connection to the daemon.
func (c *Client) Close() error {
	// Stop event stream first
	c.StopEventStream()

	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn == nil {
		return nil
	}

	err := c.conn.Close()
	c.conn = nil
	c.encoder = nil
	c.decoder = nil
	c.attached = false
	return err
}

// IsConnected returns true if the client is connected.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// SocketPath returns the socket path this client connects to.
func (c *Client) SocketPath() string {
	return c.socketPath
}

// nextID generates the next request ID.
func (c *Client) nextID() string {
	return fmt.Sprintf("req-%d", c.reqID.Add(1))
}

// decodePayload decodes the response payload into the given type.
// Returns a pointer to the decoded value, or an error if decoding fails.
// If payload is nil, returns a pointer to the zero value of T.
func decodePayload[T any](payload any) (*T, error) {
	var result T
	if payload == nil {
		return &result, nil
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("unmarshal payload: %w", err)
	}
	return &result, nil
}

// Send sends a request and waits for the response.
// This blocks until the response is received or an error occurs.
// Send and RecvEvent are mutually exclusive - only one can run at a time.
// On connection errors, the connection is closed so that IsConnected() returns false.
func (c *Client) Send(req *Request) (*Response, error) {
	// Get connection state under mu
	c.mu.Lock()
	if c.conn == nil {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	conn := c.conn
	encoder := c.encoder
	decoder := c.decoder
	c.mu.Unlock()

	// Assign request ID if not set
	if req.ID == "" {
		req.ID = c.nextID()
	}

	// Serialize all I/O operations
	c.ioMu.Lock()
	defer c.ioMu.Unlock()

	// Set deadline for this request/response cycle
	if err := conn.SetDeadline(time.Now().Add(RequestTimeout)); err != nil {
		c.closeConnLocked()
		return nil, fmt.Errorf("set deadline: %w", err)
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }() // Always clear deadline on exit

	if err := encoder.Encode(req); err != nil {
		c.closeConnLocked()
		return nil, fmt.Errorf("encode request: %w", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		c.closeConnLocked()
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &resp, nil
}

// closeConnLocked closes the main connection and clears connection state.
// Caller must NOT hold c.mu (this method acquires it).
func (c *Client) closeConnLocked() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.encoder = nil
		c.decoder = nil
		c.attached = false
	}
}

// Ping sends a ping request to check daemon connectivity.
func (c *Client) Ping() (*PingResponse, error) {
	resp, err := c.Send(&Request{Type: MsgPing})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("ping", resp.Error)
	}
	return decodePayload[PingResponse](resp.Payload)
}

// Shutdown requests the daemon to shut down.
func (c *Client) Shutdown() error {
	resp, err := c.Send(&Request{Type: MsgShutdown})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("shutdown", resp.Error)
	}
	return nil
}

// Status gets the daemon and supervisor status.
func (c *Client) Status() (*StatusResponse, error) {
	resp, err := c.Send(&Request{Type: MsgStatus})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("status", resp.Error)
	}
	return decodePayload[StatusResponse](resp.Payload)
}

// Start starts orchestration for a project.
func (c *Client) Start(project string, all bool) error {
	resp, err := c.Send(&Request{
		Type:    MsgStart,
		Payload: StartRequest{Project: project, All: all},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("start", resp.Error)
	}
	return nil
}

// Stop stops orchestration for a project.
func (c *Client) Stop(project string, all bool) error {
	resp, err := c.Send(&Request{
		Type:    MsgStop,
		Payload: StopRequest{Project: project, All: all},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("stop", resp.Error)
	}
	return nil
}

// ProjectAdd adds a project to the daemon.
func (c *Client) ProjectAdd(remoteURL, name string, maxAgents int, autostart bool, backend string) (*ProjectAddResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgProjectAdd,
		Payload: ProjectAddRequest{RemoteURL: remoteURL, Name: name, MaxAgents: maxAgents, Autostart: autostart, Backend: backend},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("project add", resp.Error)
	}
	return decodePayload[ProjectAddResponse](resp.Payload)
}

// ProjectRemove removes a project from the daemon.
func (c *Client) ProjectRemove(name string, deleteWorktrees bool) error {
	resp, err := c.Send(&Request{
		Type:    MsgProjectRemove,
		Payload: ProjectRemoveRequest{Name: name, DeleteWorktrees: deleteWorktrees},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("project remove", resp.Error)
	}
	return nil
}

// ProjectList lists all projects.
func (c *Client) ProjectList() (*ProjectListResponse, error) {
	resp, err := c.Send(&Request{Type: MsgProjectList})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("project list", resp.Error)
	}
	return decodePayload[ProjectListResponse](resp.Payload)
}

// ProjectSet updates project settings.
// Deprecated: Use ProjectConfigSet instead.
func (c *Client) ProjectSet(name string, maxAgents *int, autostart *bool) error {
	resp, err := c.Send(&Request{
		Type:    MsgProjectSet,
		Payload: ProjectSetRequest{Name: name, MaxAgents: maxAgents, Autostart: autostart},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("project set", resp.Error)
	}
	return nil
}

// ProjectConfigShow returns all config for a project.
func (c *Client) ProjectConfigShow(name string) (*ProjectConfigShowResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgProjectConfigShow,
		Payload: ProjectConfigShowRequest{Name: name},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("project config show", resp.Error)
	}
	return decodePayload[ProjectConfigShowResponse](resp.Payload)
}

// ProjectConfigGet returns a single config value for a project.
func (c *Client) ProjectConfigGet(name, key string) (*ProjectConfigGetResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgProjectConfigGet,
		Payload: ProjectConfigGetRequest{Name: name, Key: key},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("project config get", resp.Error)
	}
	return decodePayload[ProjectConfigGetResponse](resp.Payload)
}

// ProjectConfigSet sets a single config value for a project.
func (c *Client) ProjectConfigSet(name, key, value string) error {
	resp, err := c.Send(&Request{
		Type:    MsgProjectConfigSet,
		Payload: ProjectConfigSetRequest{Name: name, Key: key, Value: value},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("project config set", resp.Error)
	}
	return nil
}

// AgentList lists agents, optionally filtered by project.
func (c *Client) AgentList(project string) (*AgentListResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgAgentList,
		Payload: AgentListRequest{Project: project},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("agent list", resp.Error)
	}
	return decodePayload[AgentListResponse](resp.Payload)
}

// AgentCreate creates a new agent for a project.
func (c *Client) AgentCreate(project, task string) (*AgentCreateResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgAgentCreate,
		Payload: AgentCreateRequest{Project: project, Task: task},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("agent create", resp.Error)
	}
	return decodePayload[AgentCreateResponse](resp.Payload)
}

// AgentDelete deletes an agent.
func (c *Client) AgentDelete(id string, force bool) error {
	resp, err := c.Send(&Request{
		Type:    MsgAgentDelete,
		Payload: AgentDeleteRequest{ID: id, Force: force},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("agent delete", resp.Error)
	}
	return nil
}

// AgentAbort aborts a running agent by sending /quit or killing the process.
// If force is true, the agent is killed immediately (SIGKILL) without graceful shutdown.
func (c *Client) AgentAbort(id string, force bool) error {
	resp, err := c.Send(&Request{
		Type:    MsgAgentAbort,
		Payload: AgentAbortRequest{ID: id, Force: force},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("agent abort", resp.Error)
	}
	return nil
}

// AgentInput sends input to an agent.
func (c *Client) AgentInput(id, input string) error {
	resp, err := c.Send(&Request{
		Type:    MsgAgentInput,
		Payload: AgentInputRequest{ID: id, Input: input},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("agent input", resp.Error)
	}
	return nil
}

// AgentOutput retrieves buffered output from an agent.
func (c *Client) AgentOutput(id string) (*AgentOutputResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgAgentOutput,
		Payload: AgentOutputRequest{ID: id},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("agent output", resp.Error)
	}
	return decodePayload[AgentOutputResponse](resp.Payload)
}

// AgentDone signals that an agent has completed its task.
// This is called by agents to notify the orchestrator they are done.
func (c *Client) AgentDone(agentID, taskID, errorMsg string) error {
	resp, err := c.Send(&Request{
		Type: MsgAgentDone,
		Payload: AgentDoneRequest{
			AgentID: agentID,
			TaskID:  taskID,
			Error:   errorMsg,
		},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("agent done", resp.Error)
	}
	return nil
}

// AgentClaim claims a ticket for an agent to prevent duplicate work.
// Returns an error if the ticket is already claimed by another agent.
func (c *Client) AgentClaim(agentID, ticketID string) error {
	resp, err := c.Send(&Request{
		Type: MsgAgentClaim,
		Payload: AgentClaimRequest{
			AgentID:  agentID,
			TicketID: ticketID,
		},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("claim", resp.Error)
	}
	return nil
}

// ClaimList returns all active ticket claims.
func (c *Client) ClaimList(project string) (*ClaimListResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgClaimList,
		Payload: ClaimListRequest{Project: project},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("claim list", resp.Error)
	}
	return decodePayload[ClaimListResponse](resp.Payload)
}

// AgentSendMessage sends a user message to an agent via stream-json.
func (c *Client) AgentSendMessage(id, content string) error {
	resp, err := c.Send(&Request{
		Type:    MsgAgentSendMessage,
		Payload: AgentSendMessageRequest{ID: id, Content: content},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("agent send message", resp.Error)
	}
	return nil
}

// AgentDescribe sets the description for an agent.
func (c *Client) AgentDescribe(agentID, description string) error {
	resp, err := c.Send(&Request{
		Type:    MsgAgentDescribe,
		Payload: AgentDescribeRequest{AgentID: agentID, Description: description},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("agent describe", resp.Error)
	}
	return nil
}

// NotifyIdle notifies the daemon that an agent has gone idle (finished responding).
// Called by the Stop hook when Claude Code completes a response.
func (c *Client) NotifyIdle(agentID string) error {
	resp, err := c.Send(&Request{
		Type:    MsgAgentIdle,
		Payload: AgentIdleRequest{AgentID: agentID},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("agent idle", resp.Error)
	}
	return nil
}

// AgentChatHistory retrieves the chat history for an agent.
func (c *Client) AgentChatHistory(id string, limit int) (*AgentChatHistoryResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgAgentChatHistory,
		Payload: AgentChatHistoryRequest{ID: id, Limit: limit},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("agent chat history", resp.Error)
	}
	return decodePayload[AgentChatHistoryResponse](resp.Payload)
}

// RequestPermission sends a permission request and blocks until a response is received.
// This is called by the fab hook command when Claude Code needs tool permission.
// The method blocks until the TUI user approves or denies the request.
func (c *Client) RequestPermission(req *PermissionRequestPayload) (*PermissionResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgPermissionRequest,
		Payload: req,
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("permission request", resp.Error)
	}
	return decodePayload[PermissionResponse](resp.Payload)
}

// RespondPermission sends a response to a pending permission request.
// Called by the TUI when the user approves or denies a permission.
func (c *Client) RespondPermission(id, behavior, message string, interrupt bool) error {
	resp, err := c.Send(&Request{
		Type: MsgPermissionRespond,
		Payload: PermissionRespondPayload{
			ID:        id,
			Behavior:  behavior,
			Message:   message,
			Interrupt: interrupt,
		},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("respond permission", resp.Error)
	}
	return nil
}

// RequestUserQuestion sends a user question request and blocks until a response is received.
// This is called by the fab hook command when Claude Code's AskUserQuestion tool is invoked.
// The method blocks until the TUI user selects answers.
func (c *Client) RequestUserQuestion(req *UserQuestionRequestPayload) (*UserQuestionResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgUserQuestionRequest,
		Payload: req,
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("user question request", resp.Error)
	}
	return decodePayload[UserQuestionResponse](resp.Payload)
}

// RespondUserQuestion sends a response to a pending user question.
// Called by the TUI when the user selects answers.
func (c *Client) RespondUserQuestion(id string, answers map[string]string) error {
	resp, err := c.Send(&Request{
		Type: MsgUserQuestionRespond,
		Payload: UserQuestionRespondPayload{
			ID:      id,
			Answers: answers,
		},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("respond user question", resp.Error)
	}
	return nil
}

// Stats retrieves aggregated session statistics.
func (c *Client) Stats(project string) (*StatsResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgStats,
		Payload: StatsRequest{Project: project},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("stats", resp.Error)
	}
	return decodePayload[StatsResponse](resp.Payload)
}

// ListPendingPermissions returns pending permission requests awaiting user approval.
func (c *Client) ListPendingPermissions(project string) (*PermissionListResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgPermissionList,
		Payload: PermissionListRequest{Project: project},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("list permissions", resp.Error)
	}
	return decodePayload[PermissionListResponse](resp.Payload)
}

// Attach subscribes to streaming events.
// After calling Attach, use RecvEvent to receive events.
func (c *Client) Attach(projects []string) error {
	resp, err := c.Send(&Request{
		Type:    MsgAttach,
		Payload: AttachRequest{Projects: projects},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("attach", resp.Error)
	}

	c.mu.Lock()
	c.attached = true
	c.mu.Unlock()
	return nil
}

// Detach unsubscribes from streaming events.
func (c *Client) Detach() error {
	resp, err := c.Send(&Request{Type: MsgDetach})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("detach", resp.Error)
	}

	c.mu.Lock()
	c.attached = false
	c.mu.Unlock()
	return nil
}

// IsAttached returns true if the client is attached for streaming.
func (c *Client) IsAttached() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.attached
}

// EventTimeout is the read timeout for streaming events.
// RecvEvent will return an error after this duration if no event is received.
// This allows Send operations to interleave with event receiving.
const EventTimeout = 100 * time.Millisecond

// RecvEvent receives the next streaming event.
// This blocks until an event is received, timeout occurs, or an error occurs.
// Only call this after Attach has been called.
// RecvEvent and Send are mutually exclusive - only one can run at a time.
func (c *Client) RecvEvent() (*StreamEvent, error) {
	c.mu.Lock()
	if c.conn == nil {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	conn := c.conn
	decoder := c.decoder
	c.mu.Unlock()

	// Serialize all I/O operations
	c.ioMu.Lock()
	defer c.ioMu.Unlock()

	// Set a short timeout so we periodically yield for Send operations
	_ = conn.SetReadDeadline(time.Now().Add(EventTimeout))

	var event StreamEvent
	if err := decoder.Decode(&event); err != nil {
		// Clear deadline on error
		_ = conn.SetReadDeadline(time.Time{})
		return nil, fmt.Errorf("decode event: %w", err)
	}

	// Clear deadline on success
	_ = conn.SetReadDeadline(time.Time{})
	return &event, nil
}

// EventResult contains either a stream event or an error.
type EventResult struct {
	Event *StreamEvent
	Err   error
}

// StreamEvents opens a dedicated connection for event streaming and returns a channel.
// Events are received on the channel until an error occurs or StopEventStream is called.
// This is preferred over RecvEvent as it uses a dedicated connection and doesn't require
// timeout-based polling.
func (c *Client) StreamEvents(projects []string) (<-chan EventResult, error) {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()

	// Close any existing event stream
	if c.eventConn != nil {
		c.eventConn.Close()
		if c.eventDone != nil {
			close(c.eventDone)
		}
	}

	// Create a new dedicated connection for events
	conn, err := net.DialTimeout("unix", c.socketPath, ConnectTimeout)
	if err != nil {
		return nil, fmt.Errorf("dial daemon for events: %w", err)
	}

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Send attach request on this connection
	req := &Request{
		ID:      "event-stream",
		Type:    MsgAttach,
		Payload: AttachRequest{Projects: projects},
	}
	if err := encoder.Encode(req); err != nil {
		conn.Close()
		return nil, fmt.Errorf("encode attach request: %w", err)
	}

	// Wait for attach response
	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		conn.Close()
		return nil, fmt.Errorf("decode attach response: %w", err)
	}
	if !resp.Success {
		conn.Close()
		return nil, NewServerError("attach", resp.Error)
	}

	// Store connection and done channel
	c.eventConn = conn
	c.eventDone = make(chan struct{})
	done := c.eventDone

	// Create channel for events
	events := make(chan EventResult, 16)

	// Start reader goroutine
	go func() {
		defer close(events)
		defer conn.Close()

		for {
			select {
			case <-done:
				return
			default:
			}

			var event StreamEvent
			if err := decoder.Decode(&event); err != nil {
				select {
				case <-done:
					// Clean shutdown, don't send error
				case events <- EventResult{Err: fmt.Errorf("decode event: %w", err)}:
				}
				return
			}

			select {
			case <-done:
				return
			case events <- EventResult{Event: &event}:
			}
		}
	}()

	return events, nil
}

// StopEventStream stops the event streaming goroutine and closes the event connection.
func (c *Client) StopEventStream() {
	c.eventMu.Lock()
	defer c.eventMu.Unlock()

	if c.eventDone != nil {
		close(c.eventDone)
		c.eventDone = nil
	}
	if c.eventConn != nil {
		c.eventConn.Close()
		c.eventConn = nil
	}
}

// ManagerStart starts the manager agent for a project.
func (c *Client) ManagerStart(project string) error {
	resp, err := c.Send(&Request{
		Type:    MsgManagerStart,
		Payload: ManagerStartRequest{Project: project},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("manager start", resp.Error)
	}
	return nil
}

// ManagerStop stops the manager agent for a project.
func (c *Client) ManagerStop(project string) error {
	resp, err := c.Send(&Request{
		Type:    MsgManagerStop,
		Payload: ManagerStopRequest{Project: project},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("manager stop", resp.Error)
	}
	return nil
}

// ManagerStatus returns the manager agent status for a project.
func (c *Client) ManagerStatus(project string) (*ManagerStatusResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgManagerStatus,
		Payload: ManagerStatusRequest{Project: project},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("manager status", resp.Error)
	}
	return decodePayload[ManagerStatusResponse](resp.Payload)
}

// ManagerSendMessage sends a message to the manager agent for a project.
func (c *Client) ManagerSendMessage(project, content string) error {
	resp, err := c.Send(&Request{
		Type:    MsgManagerSendMessage,
		Payload: ManagerSendMessageRequest{Project: project, Content: content},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("manager send message", resp.Error)
	}
	return nil
}

// ManagerChatHistory retrieves the chat history for the manager agent of a project.
func (c *Client) ManagerChatHistory(project string, limit int) (*ManagerChatHistoryResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgManagerChatHistory,
		Payload: ManagerChatHistoryRequest{Project: project, Limit: limit},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("manager chat history", resp.Error)
	}
	return decodePayload[ManagerChatHistoryResponse](resp.Payload)
}

// ManagerClearHistory clears the manager agent's chat history for a project.
func (c *Client) ManagerClearHistory(project string) error {
	resp, err := c.Send(&Request{
		Type:    MsgManagerClearHistory,
		Payload: ManagerClearHistoryRequest{Project: project},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("manager clear history", resp.Error)
	}
	return nil
}

// PlanStart starts a planning agent.
func (c *Client) PlanStart(project, prompt string) (*PlanStartResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgPlanStart,
		Payload: PlanStartRequest{Project: project, Prompt: prompt},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("plan start", resp.Error)
	}
	return decodePayload[PlanStartResponse](resp.Payload)
}

// PlanStop stops a planning agent.
func (c *Client) PlanStop(id string) error {
	resp, err := c.Send(&Request{
		Type:    MsgPlanStop,
		Payload: PlanStopRequest{ID: id},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("plan stop", resp.Error)
	}
	return nil
}

// PlanList lists planning agents.
func (c *Client) PlanList(project string) (*PlanListResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgPlanList,
		Payload: PlanListRequest{Project: project},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("plan list", resp.Error)
	}
	return decodePayload[PlanListResponse](resp.Payload)
}

// PlanSendMessage sends a message to a planning agent.
func (c *Client) PlanSendMessage(id, content string) error {
	resp, err := c.Send(&Request{
		Type:    MsgPlanSendMessage,
		Payload: PlanSendMessageRequest{ID: id, Content: content},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return NewServerError("plan send message", resp.Error)
	}
	return nil
}

// PlanChatHistory retrieves the chat history for a planning agent.
func (c *Client) PlanChatHistory(id string, limit int) (*PlanChatHistoryResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgPlanChatHistory,
		Payload: PlanChatHistoryRequest{ID: id, Limit: limit},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, NewServerError("plan chat history", resp.Error)
	}
	return decodePayload[PlanChatHistoryResponse](resp.Payload)
}
