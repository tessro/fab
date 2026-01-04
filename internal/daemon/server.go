// Package daemon provides the fab daemon server and IPC protocol.
package daemon

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
)

// DefaultSocketPath returns the default Unix socket path.
func DefaultSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/fab.sock"
	}
	return filepath.Join(home, ".fab", "fab.sock")
}

// contextKey is a type for context keys to avoid collisions.
type contextKey string

const (
	connKey   contextKey = "conn"
	serverKey contextKey = "server"
)

// Handler processes IPC requests and returns responses.
// This interface is implemented by the supervisor or a stub for testing.
type Handler interface {
	// Handle processes a request and returns a response.
	// The context contains the connection and server for attach/detach operations.
	// Use ConnFromContext and ServerFromContext to retrieve them.
	Handle(ctx context.Context, req *Request) *Response
}

// ConnFromContext retrieves the client connection from the context.
func ConnFromContext(ctx context.Context) net.Conn {
	conn, _ := ctx.Value(connKey).(net.Conn)
	return conn
}

// ServerFromContext retrieves the server from the context.
func ServerFromContext(ctx context.Context) *Server {
	srv, _ := ctx.Value(serverKey).(*Server)
	return srv
}

// HandlerFunc is a function adapter for Handler.
type HandlerFunc func(ctx context.Context, req *Request) *Response

// Handle implements Handler.
func (f HandlerFunc) Handle(ctx context.Context, req *Request) *Response {
	return f(ctx, req)
}

// Server is the Unix socket RPC server for the fab daemon.
type Server struct {
	socketPath string
	handler    Handler
	listener   net.Listener // Set in Start before goroutine, closed in Stop

	mu sync.Mutex
	// +checklocks:mu
	conns map[net.Conn]struct{}
	// +checklocks:mu
	attached map[net.Conn]*attachedClient
	// +checklocks:mu
	started bool
	done    chan struct{}
}

// attachedClient tracks a client subscribed to streaming events.
type attachedClient struct {
	// +checklocks:mu
	encoder  *json.Encoder
	projects []string // Filter: empty means all projects (immutable after creation)
	mu       sync.Mutex
}

// NewServer creates a new daemon server.
func NewServer(socketPath string, handler Handler) *Server {
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	return &Server{
		socketPath: socketPath,
		handler:    handler,
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
	s.mu.Unlock()

	slog.Info("daemon server started", "socket", s.socketPath)

	go s.acceptLoop()

	return nil
}

// acceptLoop accepts incoming connections.
func (s *Server) acceptLoop() {
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

	// Create base context with connection and server info
	baseCtx := context.WithValue(context.Background(), connKey, conn)
	baseCtx = context.WithValue(baseCtx, serverKey, s)

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
			encoder.Encode(resp)
			return
		}

		slog.Debug("request received", "type", req.Type, "id", req.ID)

		// Use base context (could add per-request timeout here)
		ctx := baseCtx

		// Dispatch to handler
		resp := s.handler.Handle(ctx, &req)
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

		if err := encoder.Encode(resp); err != nil {
			slog.Debug("write response failed", "error", err)
			return // Client disconnected or write error
		}
	}
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

	slog.Info("daemon server stopping", "active_connections", connCount)

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
	os.Remove(s.socketPath)

	slog.Info("daemon server stopped")

	return nil
}

// Addr returns the listener address, or empty string if not started.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener != nil {
		return s.listener.Addr().String()
	}
	return ""
}

// Attach registers a connection for streaming events.
// The connection must already be established and tracked.
func (s *Server) Attach(conn net.Conn, projects []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.attached[conn] = &attachedClient{
		encoder:  json.NewEncoder(conn),
		projects: projects,
	}
}

// Detach removes a connection from streaming events.
func (s *Server) Detach(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.attached, conn)
}

// Broadcast sends a stream event to all attached clients.
// Clients are filtered by their project subscriptions.
func (s *Server) Broadcast(event *StreamEvent) {
	s.mu.Lock()
	clients := make([]*attachedClient, 0, len(s.attached))
	for _, client := range s.attached {
		clients = append(clients, client)
	}
	s.mu.Unlock()

	for _, client := range clients {
		// Check if client is subscribed to this project
		if len(client.projects) > 0 {
			subscribed := false
			for _, p := range client.projects {
				if p == event.Project {
					subscribed = true
					break
				}
			}
			if !subscribed {
				continue
			}
		}

		// Send event (with per-client lock to prevent interleaving)
		client.mu.Lock()
		client.encoder.Encode(event)
		client.mu.Unlock()
	}
}

// AttachedCount returns the number of attached streaming clients.
func (s *Server) AttachedCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.attached)
}
