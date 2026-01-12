// Package agenthost implements the agent host process that wraps Claude Code agents.
package agenthost

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/tessro/fab/internal/logging"
	"github.com/tessro/fab/internal/version"
)

// contextKey is a type for context keys to avoid collisions.
type contextKey string

const (
	connKey    contextKey = "conn"
	serverKey  contextKey = "server"
	encoderKey contextKey = "encoder"
	writeMuKey contextKey = "writeMu"
)

// BroadcastTimeout is how long to wait for a client write before giving up.
const BroadcastTimeout = 100 * time.Millisecond

// Server is the Unix socket RPC server for an agent host process.
// Each agent host manages a single coding agent and its associated socket.
type Server struct {
	socketPath string
	manager    *Manager
	listener   net.Listener

	mu sync.Mutex
	// +checklocks:mu
	conns map[net.Conn]struct{}
	// +checklocks:mu
	attached map[net.Conn]*attachedClient
	// +checklocks:mu
	started   bool
	startedAt time.Time
	done      chan struct{}
}

// attachedClient tracks a client subscribed to streaming events.
type attachedClient struct {
	encoder *json.Encoder
	mu      *sync.Mutex // Shared mutex for all writes to the connection
	offset  int64       // Current stream offset for this client
}

// NewServer creates a new agent host server.
func NewServer(socketPath string, manager *Manager) *Server {
	return &Server{
		socketPath: socketPath,
		manager:    manager,
		conns:      make(map[net.Conn]struct{}),
		attached:   make(map[net.Conn]*attachedClient),
		done:       make(chan struct{}),
	}
}

// SocketPath returns the socket path this server listens on.
func (s *Server) SocketPath() string {
	return s.socketPath
}

// Start begins listening on the Unix socket.
// Returns an error if the server is already running or cannot bind.
func (s *Server) Start() error {
	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return errors.New("server already started")
	}
	s.mu.Unlock()

	// Ensure the socket directory exists
	dir := filepath.Dir(s.socketPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("create socket directory: %w", err)
	}

	// Remove stale socket file if it exists
	if err := os.Remove(s.socketPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove stale socket: %w", err)
	}

	listener, err := net.Listen("unix", s.socketPath)
	if err != nil {
		return fmt.Errorf("listen on socket: %w", err)
	}

	// Set socket permissions (owner only)
	if err := os.Chmod(s.socketPath, 0600); err != nil {
		listener.Close()
		return fmt.Errorf("set socket permissions: %w", err)
	}

	s.mu.Lock()
	s.listener = listener
	s.started = true
	s.startedAt = time.Now()
	s.mu.Unlock()

	slog.Info("agent host server started", "socket", s.socketPath)

	go s.acceptLoop()

	return nil
}

// acceptLoop accepts incoming connections.
func (s *Server) acceptLoop() {
	defer logging.LogPanic("agenthost-accept-loop", nil)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return // Server shutting down
			default:
				slog.Error("accept connection failed", "error", err)
				continue
			}
		}

		s.mu.Lock()
		s.conns[conn] = struct{}{}
		connCount := len(s.conns)
		s.mu.Unlock()

		slog.Debug("client connected", "connections", connCount)

		go s.handleConnection(conn)
	}
}

// handleConnection processes requests from a single client.
func (s *Server) handleConnection(conn net.Conn) {
	defer logging.LogPanic("agenthost-connection-handler", nil)
	defer func() {
		conn.Close()
		s.mu.Lock()
		delete(s.conns, conn)
		delete(s.attached, conn)
		connCount := len(s.conns)
		s.mu.Unlock()
		slog.Debug("client disconnected", "connections", connCount)
	}()

	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)
	var writeMu sync.Mutex // Protects all writes to conn

	// Create base context with connection, server, and encoder info
	baseCtx := context.WithValue(context.Background(), connKey, conn)
	baseCtx = context.WithValue(baseCtx, serverKey, s)
	baseCtx = context.WithValue(baseCtx, encoderKey, encoder)
	baseCtx = context.WithValue(baseCtx, writeMuKey, &writeMu)

	for {
		var req Request
		if err := decoder.Decode(&req); err != nil {
			if err == io.EOF {
				return // Client disconnected
			}
			slog.Warn("decode request failed", "error", err)
			// Send error response for malformed request
			resp := &Response{
				Success: false,
				Error:   fmt.Sprintf("decode request: %v", err),
			}
			writeMu.Lock()
			_ = encoder.Encode(resp)
			writeMu.Unlock()
			return
		}

		slog.Debug("request received", "type", req.Type, "id", req.ID)

		// Dispatch to handler
		resp := s.handle(baseCtx, &req)
		if resp == nil {
			resp = &Response{
				Type:    req.Type,
				ID:      req.ID,
				Success: false,
				Error:   "handler returned nil response",
			}
		}

		// Ensure response has correct correlation info
		if resp.Type == "" {
			resp.Type = req.Type
		}
		if resp.ID == "" {
			resp.ID = req.ID
		}

		if !resp.Success {
			slog.Warn("request failed", "type", req.Type, "error", resp.Error)
		}

		writeMu.Lock()
		err := encoder.Encode(resp)
		writeMu.Unlock()
		if err != nil {
			slog.Debug("write response failed", "error", err)
			return // Client disconnected or write error
		}
	}
}

// handle processes a single request and returns a response.
func (s *Server) handle(ctx context.Context, req *Request) *Response {
	switch req.Type {
	case MsgHostPing:
		return s.handlePing(req)
	case MsgHostStatus:
		return s.handleStatus(req)
	case MsgHostList:
		return s.handleList(req)
	case MsgHostAttach:
		return s.handleAttach(ctx, req)
	case MsgHostDetach:
		return s.handleDetach(ctx, req)
	case MsgHostSend:
		return s.handleSend(req)
	case MsgHostStop:
		return s.handleStop(req)
	default:
		return ErrorResponse(req, fmt.Sprintf("unknown message type: %s", req.Type))
	}
}

// handlePing handles ping requests for health checks.
func (s *Server) handlePing(req *Request) *Response {
	s.mu.Lock()
	startedAt := s.startedAt
	s.mu.Unlock()

	uptime := time.Since(startedAt)
	payload := PingResponse{
		Version:         version.Version,
		ProtocolVersion: ProtocolVersion,
		Uptime:          uptime.Round(time.Second).String(),
		StartedAt:       startedAt,
	}
	return SuccessResponse(req, payload)
}

// handleStatus handles status requests for detailed host and agent info.
func (s *Server) handleStatus(req *Request) *Response {
	s.mu.Lock()
	startedAt := s.startedAt
	s.mu.Unlock()

	hostInfo := HostInfo{
		PID:             os.Getpid(),
		Version:         version.Version,
		ProtocolVersion: ProtocolVersion,
		StartedAt:       startedAt,
		SocketPath:      s.socketPath,
	}

	agentInfo := s.manager.AgentInfo()

	payload := StatusResponse{
		Host:  hostInfo,
		Agent: agentInfo,
	}
	return SuccessResponse(req, payload)
}

// handleList handles list requests (returns single agent for this host).
func (s *Server) handleList(req *Request) *Response {
	agentInfo := s.manager.AgentInfo()
	payload := ListResponse{
		Agents: []AgentInfo{agentInfo},
	}
	return SuccessResponse(req, payload)
}

// handleAttach handles attach requests to subscribe to the output stream.
func (s *Server) handleAttach(ctx context.Context, req *Request) *Response {
	conn := ctx.Value(connKey).(net.Conn)
	encoder := ctx.Value(encoderKey).(*json.Encoder)
	writeMu := ctx.Value(writeMuKey).(*sync.Mutex)

	var attachReq AttachRequest
	if err := UnmarshalPayload(req.Payload, &attachReq); err != nil {
		return ErrorResponse(req, fmt.Sprintf("invalid attach payload: %v", err))
	}

	// Register client for streaming
	s.attach(conn, encoder, writeMu, attachReq.Offset)

	// Get current stream offset from manager
	currentOffset := s.manager.StreamOffset()
	agentID := s.manager.AgentID()

	// Send any buffered history since the requested offset
	if attachReq.Offset < currentOffset {
		s.sendBufferedHistory(conn, encoder, writeMu, attachReq.Offset)
	}

	payload := AttachResponse{
		AgentID:      agentID,
		StreamOffset: currentOffset,
	}
	return SuccessResponse(req, payload)
}

// handleDetach handles detach requests to unsubscribe from the output stream.
func (s *Server) handleDetach(ctx context.Context, req *Request) *Response {
	conn := ctx.Value(connKey).(net.Conn)
	s.detach(conn)
	return SuccessResponse(req, nil)
}

// handleSend handles send requests to write input to the agent.
func (s *Server) handleSend(req *Request) *Response {
	var sendReq SendRequest
	if err := UnmarshalPayload(req.Payload, &sendReq); err != nil {
		return ErrorResponse(req, fmt.Sprintf("invalid send payload: %v", err))
	}

	if err := s.manager.SendMessage(sendReq.Input); err != nil {
		return ErrorResponse(req, fmt.Sprintf("send failed: %v", err))
	}

	return SuccessResponse(req, nil)
}

// handleStop handles stop requests to terminate the agent.
func (s *Server) handleStop(req *Request) *Response {
	var stopReq StopRequest
	if err := UnmarshalPayload(req.Payload, &stopReq); err != nil {
		return ErrorResponse(req, fmt.Sprintf("invalid stop payload: %v", err))
	}

	startTime := time.Now()
	timeout := time.Duration(stopReq.Timeout) * time.Second
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	exitCode := 0
	graceful := true

	if err := s.manager.Stop(stopReq.Force, timeout); err != nil {
		slog.Warn("stop failed", "error", err)
		graceful = false
	}

	// Get final state after stop
	finalState := s.manager.AgentInfo().State

	duration := time.Since(startTime)
	payload := StopResponse{
		Stopped:    true,
		ExitCode:   exitCode,
		Graceful:   graceful,
		Duration:   duration.Round(time.Millisecond).String(),
		FinalState: finalState,
	}

	// Schedule server shutdown after responding.
	// The delay ensures the response is written to the client before closing.
	go func() {
		time.Sleep(100 * time.Millisecond)
		_ = s.Stop()
	}()

	return SuccessResponse(req, payload)
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	s.started = false
	connCount := len(s.conns)
	s.mu.Unlock()

	slog.Info("agent host server stopping", "active_connections", connCount)

	// Signal acceptLoop to stop
	close(s.done)

	// Close listener to unblock Accept
	if s.listener != nil {
		s.listener.Close()
	}

	// Close all active connections
	s.mu.Lock()
	for conn := range s.conns {
		conn.Close()
	}
	s.conns = make(map[net.Conn]struct{})
	s.attached = make(map[net.Conn]*attachedClient)
	s.mu.Unlock()

	// Remove socket file
	_ = os.Remove(s.socketPath)

	slog.Info("agent host server stopped")

	return nil
}

// attach registers a connection for streaming events.
func (s *Server) attach(conn net.Conn, encoder *json.Encoder, mu *sync.Mutex, offset int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attached[conn] = &attachedClient{
		encoder: encoder,
		mu:      mu,
		offset:  offset,
	}
}

// detach removes a connection from streaming events.
func (s *Server) detach(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attached, conn)
}

// sendBufferedHistory sends buffered history events to a newly attached client.
func (s *Server) sendBufferedHistory(conn net.Conn, encoder *json.Encoder, mu *sync.Mutex, fromOffset int64) {
	events := s.manager.GetBufferedEvents(fromOffset)
	for _, event := range events {
		_ = conn.SetWriteDeadline(time.Now().Add(BroadcastTimeout))
		mu.Lock()
		if err := encoder.Encode(event); err != nil {
			mu.Unlock()
			slog.Debug("failed to send buffered history", "error", err)
			break
		}
		mu.Unlock()
		_ = conn.SetWriteDeadline(time.Time{})
	}
}

// Broadcast sends a stream event to all attached clients.
func (s *Server) Broadcast(event *StreamEvent) {
	s.mu.Lock()
	clients := make([]*attachedClient, 0, len(s.attached))
	conns := make([]net.Conn, 0, len(s.attached))
	for conn, client := range s.attached {
		clients = append(clients, client)
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	for i, client := range clients {
		// Only send events with offset >= client's offset
		if event.Offset < client.offset {
			continue
		}

		conn := conns[i]
		_ = conn.SetWriteDeadline(time.Now().Add(BroadcastTimeout))

		client.mu.Lock()
		if err := client.encoder.Encode(event); err != nil {
			slog.Debug("broadcast encode error", "type", event.Type, "error", err)
		} else {
			slog.Debug("broadcast sent", "type", event.Type, "agent", event.AgentID)
		}
		client.mu.Unlock()

		_ = conn.SetWriteDeadline(time.Time{})
	}
}

// AttachedCount returns the number of attached streaming clients.
func (s *Server) AttachedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.attached)
}
