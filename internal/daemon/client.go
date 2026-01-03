// Package daemon provides the fab daemon server and IPC protocol.
package daemon

import (
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
)

// Client connects to the fab daemon over Unix socket.
type Client struct {
	socketPath string
	conn       net.Conn
	encoder    *json.Encoder
	decoder    *json.Decoder

	mu       sync.Mutex
	reqID    atomic.Uint64
	attached bool
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

// Connect establishes a connection to the daemon.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return nil // Already connected
	}

	conn, err := net.Dial("unix", c.socketPath)
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
// This is not safe to call concurrently with other Send/Recv operations.
func (c *Client) Send(req *Request) (*Response, error) {
	c.mu.Lock()
	if c.conn == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	encoder := c.encoder
	decoder := c.decoder
	c.mu.Unlock()

	// Assign request ID if not set
	if req.ID == "" {
		req.ID = c.nextID()
	}

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
func (c *Client) ProjectAdd(path, name string, maxAgents int) (*ProjectAddResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgProjectAdd,
		Payload: ProjectAddRequest{Path: path, Name: name, MaxAgents: maxAgents},
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
		json.Unmarshal(data, &result)
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
		json.Unmarshal(data, &result)
	}
	return &result, nil
}

// ProjectSet updates project settings.
func (c *Client) ProjectSet(name string, maxAgents *int) error {
	resp, err := c.Send(&Request{
		Type:    MsgProjectSet,
		Payload: ProjectSetRequest{Name: name, MaxAgents: maxAgents},
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
		json.Unmarshal(data, &result)
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
		json.Unmarshal(data, &result)
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

// AgentInput sends input to an agent's PTY.
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

// RecvEvent receives the next streaming event.
// This blocks until an event is received or an error occurs.
// Only call this after Attach has been called.
func (c *Client) RecvEvent() (*StreamEvent, error) {
	c.mu.Lock()
	if c.conn == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	decoder := c.decoder
	c.mu.Unlock()

	var event StreamEvent
	if err := decoder.Decode(&event); err != nil {
		return nil, fmt.Errorf("decode event: %w", err)
	}
	return &event, nil
}
