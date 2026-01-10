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
	eventMu   sync.Mutex
	eventConn net.Conn
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

// Send sends a request and waits for the response.
// This blocks until the response is received or an error occurs.
// Send and RecvEvent are mutually exclusive - only one can run at a time.
func (c *Client) Send(req *Request) (*Response, error) {
	// Get connection state under mu
	c.mu.Lock()
	if c.conn == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
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
		return nil, fmt.Errorf("set deadline: %w", err)
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }() // Always clear deadline on exit

	if err := encoder.Encode(req); err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &resp, nil
}

// Ping sends a ping request to check daemon connectivity.
func (c *Client) Ping() (*PingResponse, error) {
	resp, err := c.Send(&Request{Type: MsgPing})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("ping failed: %s", resp.Error)
	}

	// Decode payload
	var ping PingResponse
	if resp.Payload != nil {
		data, err := json.Marshal(resp.Payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		if err := json.Unmarshal(data, &ping); err != nil {
			return nil, fmt.Errorf("unmarshal ping response: %w", err)
		}
	}
	return &ping, nil
}

// Shutdown requests the daemon to shut down.
func (c *Client) Shutdown() error {
	resp, err := c.Send(&Request{Type: MsgShutdown})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("shutdown failed: %s", resp.Error)
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
		return nil, fmt.Errorf("status failed: %s", resp.Error)
	}

	var status StatusResponse
	if resp.Payload != nil {
		data, err := json.Marshal(resp.Payload)
		if err != nil {
			return nil, fmt.Errorf("marshal payload: %w", err)
		}
		if err := json.Unmarshal(data, &status); err != nil {
			return nil, fmt.Errorf("unmarshal status response: %w", err)
		}
	}
	return &status, nil
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
		return fmt.Errorf("start failed: %s", resp.Error)
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
		return fmt.Errorf("stop failed: %s", resp.Error)
	}
	return nil
}

// ProjectAdd adds a project to the daemon.
func (c *Client) ProjectAdd(remoteURL, name string, maxAgents int, autostart bool) (*ProjectAddResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgProjectAdd,
		Payload: ProjectAddRequest{RemoteURL: remoteURL, Name: name, MaxAgents: maxAgents, Autostart: autostart},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("project add failed: %s", resp.Error)
	}

	var result ProjectAddResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
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
		return fmt.Errorf("project remove failed: %s", resp.Error)
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
		return nil, fmt.Errorf("project list failed: %s", resp.Error)
	}

	var result ProjectListResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
}

// ProjectSet updates project settings.
func (c *Client) ProjectSet(name string, maxAgents *int, autostart *bool) error {
	resp, err := c.Send(&Request{
		Type:    MsgProjectSet,
		Payload: ProjectSetRequest{Name: name, MaxAgents: maxAgents, Autostart: autostart},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("project set failed: %s", resp.Error)
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
		return nil, fmt.Errorf("agent list failed: %s", resp.Error)
	}

	var result AgentListResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
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
		return nil, fmt.Errorf("agent create failed: %s", resp.Error)
	}

	var result AgentCreateResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
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
		return fmt.Errorf("agent delete failed: %s", resp.Error)
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
		return fmt.Errorf("agent abort failed: %s", resp.Error)
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
		return fmt.Errorf("agent input failed: %s", resp.Error)
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
		return nil, fmt.Errorf("agent output failed: %s", resp.Error)
	}

	var result AgentOutputResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
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
		return fmt.Errorf("agent done failed: %s", resp.Error)
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
		return fmt.Errorf("%s", resp.Error)
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
		return nil, fmt.Errorf("claim list failed: %s", resp.Error)
	}

	var result ClaimListResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
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
		return fmt.Errorf("agent send message failed: %s", resp.Error)
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
		return fmt.Errorf("agent describe failed: %s", resp.Error)
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
		return nil, fmt.Errorf("agent chat history failed: %s", resp.Error)
	}

	var result AgentChatHistoryResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
}

// ListStagedActions returns pending actions for user approval.
func (c *Client) ListStagedActions(project string) (*StagedActionsResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgListStagedActions,
		Payload: StagedActionsRequest{Project: project},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("list actions failed: %s", resp.Error)
	}

	var result StagedActionsResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
}

// ApproveAction approves and executes a staged action.
func (c *Client) ApproveAction(actionID string) error {
	resp, err := c.Send(&Request{
		Type:    MsgApproveAction,
		Payload: ApproveActionRequest{ActionID: actionID},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("approve action failed: %s", resp.Error)
	}
	return nil
}

// RejectAction rejects a staged action without executing it.
func (c *Client) RejectAction(actionID, reason string) error {
	resp, err := c.Send(&Request{
		Type:    MsgRejectAction,
		Payload: RejectActionRequest{ActionID: actionID, Reason: reason},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("reject action failed: %s", resp.Error)
	}
	return nil
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
		return nil, fmt.Errorf("permission request failed: %s", resp.Error)
	}

	var result PermissionResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
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
		return fmt.Errorf("respond permission failed: %s", resp.Error)
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
		return nil, fmt.Errorf("user question request failed: %s", resp.Error)
	}

	var result UserQuestionResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
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
		return fmt.Errorf("respond user question failed: %s", resp.Error)
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
		return nil, fmt.Errorf("stats failed: %s", resp.Error)
	}

	var result StatsResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
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
		return nil, fmt.Errorf("list permissions failed: %s", resp.Error)
	}

	var result PermissionListResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
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
		return fmt.Errorf("attach failed: %s", resp.Error)
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
		return fmt.Errorf("detach failed: %s", resp.Error)
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
		return nil, fmt.Errorf("not connected")
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
		return nil, fmt.Errorf("attach failed: %s", resp.Error)
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

// ManagerStart starts the manager agent.
func (c *Client) ManagerStart() error {
	resp, err := c.Send(&Request{Type: MsgManagerStart})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("manager start failed: %s", resp.Error)
	}
	return nil
}

// ManagerStop stops the manager agent.
func (c *Client) ManagerStop() error {
	resp, err := c.Send(&Request{Type: MsgManagerStop})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("manager stop failed: %s", resp.Error)
	}
	return nil
}

// ManagerStatus returns the manager agent status.
func (c *Client) ManagerStatus() (*ManagerStatusResponse, error) {
	resp, err := c.Send(&Request{Type: MsgManagerStatus})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("manager status failed: %s", resp.Error)
	}

	var result ManagerStatusResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
}

// ManagerSendMessage sends a message to the manager agent.
func (c *Client) ManagerSendMessage(content string) error {
	resp, err := c.Send(&Request{
		Type:    MsgManagerSendMessage,
		Payload: ManagerSendMessageRequest{Content: content},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("manager send message failed: %s", resp.Error)
	}
	return nil
}

// ManagerChatHistory retrieves the chat history for the manager agent.
func (c *Client) ManagerChatHistory(limit int) (*ManagerChatHistoryResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgManagerChatHistory,
		Payload: ManagerChatHistoryRequest{Limit: limit},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("manager chat history failed: %s", resp.Error)
	}

	var result ManagerChatHistoryResponse
	if resp.Payload != nil {
		data, _ := json.Marshal(resp.Payload)
		_ = json.Unmarshal(data, &result)
	}
	return &result, nil
}

// ManagerClearHistory clears the manager agent's chat history.
func (c *Client) ManagerClearHistory() error {
	resp, err := c.Send(&Request{Type: MsgManagerClearHistory})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("manager clear history failed: %s", resp.Error)
	}
	return nil
}
