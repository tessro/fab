// Package agenthost provides client/server communication with agent host processes.
package agenthost

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/tessro/fab/internal/paths"
)

// Client errors.
var (
	ErrNotConnected = errors.New("not connected to agent host")
	ErrHostNotFound = errors.New("agent host not found")
)

// Client connects to an agent host process over Unix socket.
type Client struct {
	socketPath string
	agentID    string

	mu sync.Mutex
	// +checklocks:mu
	conn net.Conn
	// +checklocks:mu
	encoder *json.Encoder
	// +checklocks:mu
	decoder *json.Decoder

	reqID atomic.Uint64
}

// ConnectTimeout is the default timeout for connecting to an agent host.
const ConnectTimeout = 2 * time.Second

// RequestTimeout is the default timeout for request/response operations.
const RequestTimeout = 10 * time.Second

// NewClient creates a new agent host client for the given agent ID.
// The socket path is determined from the agent ID using paths.AgentHostSocketPath.
func NewClient(agentID string) (*Client, error) {
	socketPath, err := paths.AgentHostSocketPath(agentID)
	if err != nil {
		return nil, fmt.Errorf("get socket path: %w", err)
	}
	return &Client{
		socketPath: socketPath,
		agentID:    agentID,
	}, nil
}

// NewClientWithSocket creates a new agent host client with an explicit socket path.
// This is useful for testing or when the socket path is already known.
func NewClientWithSocket(agentID, socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
		agentID:    agentID,
	}
}

// Connect establishes a connection to the agent host.
func (c *Client) Connect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.conn != nil {
		return nil // Already connected
	}

	conn, err := net.DialTimeout("unix", c.socketPath, ConnectTimeout)
	if err != nil {
		return fmt.Errorf("dial agent host: %w", err)
	}

	c.conn = conn
	c.encoder = json.NewEncoder(conn)
	c.decoder = json.NewDecoder(conn)
	return nil
}

// Close closes the connection to the agent host.
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

// AgentID returns the agent ID this client is for.
func (c *Client) AgentID() string {
	return c.agentID
}

// nextID generates the next request ID.
func (c *Client) nextID() string {
	return fmt.Sprintf("req-%d", c.reqID.Add(1))
}

// Send sends a request and waits for the response.
func (c *Client) Send(req *Request) (*Response, error) {
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

	// Set deadline for this request/response cycle
	if err := conn.SetDeadline(time.Now().Add(RequestTimeout)); err != nil {
		c.closeConn()
		return nil, fmt.Errorf("set deadline: %w", err)
	}
	defer func() { _ = conn.SetDeadline(time.Time{}) }()

	if err := encoder.Encode(req); err != nil {
		c.closeConn()
		return nil, fmt.Errorf("encode request: %w", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		c.closeConn()
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return &resp, nil
}

// closeConn closes the connection and clears state.
func (c *Client) closeConn() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		_ = c.conn.Close()
		c.conn = nil
		c.encoder = nil
		c.decoder = nil
	}
}

// Ping sends a ping request to check host connectivity.
func (c *Client) Ping() (*PingResponse, error) {
	resp, err := c.Send(&Request{Type: MsgHostPing})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("ping failed: %s", resp.Error)
	}
	return DecodePayload[PingResponse](resp.Payload)
}

// Status gets the detailed host and agent status.
func (c *Client) Status() (*StatusResponse, error) {
	resp, err := c.Send(&Request{Type: MsgHostStatus})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("status failed: %s", resp.Error)
	}
	return DecodePayload[StatusResponse](resp.Payload)
}

// List returns all agents managed by this host.
func (c *Client) List() (*ListResponse, error) {
	resp, err := c.Send(&Request{Type: MsgHostList})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("list failed: %s", resp.Error)
	}
	return DecodePayload[ListResponse](resp.Payload)
}

// Attach attaches to the agent's output stream.
// After attaching, use RecvStreamEvent to receive events.
func (c *Client) Attach(offset int64) (*AttachResponse, error) {
	resp, err := c.Send(&Request{
		Type:    MsgHostAttach,
		Payload: AttachRequest{Offset: offset},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("attach failed: %s", resp.Error)
	}
	return DecodePayload[AttachResponse](resp.Payload)
}

// Detach detaches from the agent's output stream.
func (c *Client) Detach() error {
	resp, err := c.Send(&Request{Type: MsgHostDetach})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("detach failed: %s", resp.Error)
	}
	return nil
}

// SendInput sends input to the agent.
func (c *Client) SendInput(input string) error {
	resp, err := c.Send(&Request{
		Type:    MsgHostSend,
		Payload: SendRequest{Input: input},
	})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("send failed: %s", resp.Error)
	}
	return nil
}

// Stop gracefully stops the agent and host.
func (c *Client) Stop(force bool, timeout int, reason string) (*StopResponse, error) {
	resp, err := c.Send(&Request{
		Type: MsgHostStop,
		Payload: StopRequest{
			Force:   force,
			Timeout: timeout,
			Reason:  reason,
		},
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("stop failed: %s", resp.Error)
	}
	return DecodePayload[StopResponse](resp.Payload)
}

// RecvStreamEvent receives the next stream event from an attached connection.
// Only call this after Attach has been called.
func (c *Client) RecvStreamEvent() (*StreamEvent, error) {
	c.mu.Lock()
	if c.conn == nil {
		c.mu.Unlock()
		return nil, ErrNotConnected
	}
	decoder := c.decoder
	c.mu.Unlock()

	var event StreamEvent
	if err := decoder.Decode(&event); err != nil {
		return nil, fmt.Errorf("decode event: %w", err)
	}
	return &event, nil
}

// ProbeHost attempts to connect to an agent host and returns its status.
// Returns ErrHostNotFound if the host socket doesn't exist or isn't responding.
func ProbeHost(agentID string) (*StatusResponse, error) {
	client, err := NewClient(agentID)
	if err != nil {
		return nil, err
	}

	if err := client.Connect(); err != nil {
		return nil, ErrHostNotFound
	}
	defer client.Close()

	status, err := client.Status()
	if err != nil {
		return nil, ErrHostNotFound
	}

	return status, nil
}
