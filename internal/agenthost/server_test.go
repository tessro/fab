package agenthost

import (
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tessro/fab/internal/agent"
)

// shortTempDir creates a temp directory with a short path for socket tests.
// Unix sockets have a path limit (~104 chars on macOS), and t.TempDir()
// includes the full test name which can exceed this limit.
func shortTempDir(t *testing.T) (string, func()) {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "fab-ah-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	return dir, func() { os.RemoveAll(dir) }
}

func TestServer_StartStop(t *testing.T) {
	tmpDir, cleanup := shortTempDir(t)
	defer cleanup()
	socketPath := filepath.Join(tmpDir, "test.sock")

	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)
	srv := NewServer(socketPath, m)
	m.SetServer(srv)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	// Verify socket exists
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		t.Fatal("socket file not created")
	}

	// Verify we can connect
	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	conn.Close()

	if err := srv.Stop(); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}

	// Verify socket removed
	if _, err := os.Stat(socketPath); !os.IsNotExist(err) {
		t.Fatal("socket file not removed after Stop()")
	}
}

func TestServer_DoubleStart(t *testing.T) {
	tmpDir, cleanup := shortTempDir(t)
	defer cleanup()
	socketPath := filepath.Join(tmpDir, "test.sock")

	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)
	srv := NewServer(socketPath, m)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	// Second start should fail
	if err := srv.Start(); err == nil {
		t.Error("expected error on double Start(), got nil")
	}
}

func TestServer_Ping(t *testing.T) {
	tmpDir, cleanup := shortTempDir(t)
	defer cleanup()
	socketPath := filepath.Join(tmpDir, "test.sock")

	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)
	srv := NewServer(socketPath, m)
	m.SetServer(srv)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Send ping request
	req := &Request{
		Type: MsgHostPing,
		ID:   "test-ping",
	}
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if !resp.Success {
		t.Errorf("expected Success=true, got false with error: %s", resp.Error)
	}
	if resp.Type != MsgHostPing {
		t.Errorf("expected Type=%s, got %s", MsgHostPing, resp.Type)
	}
	if resp.ID != "test-ping" {
		t.Errorf("expected ID=test-ping, got %s", resp.ID)
	}

	// Decode payload
	pingResp, err := DecodePayload[PingResponse](resp.Payload)
	if err != nil {
		t.Fatalf("DecodePayload() error = %v", err)
	}
	if pingResp.ProtocolVersion != ProtocolVersion {
		t.Errorf("ProtocolVersion = %q, want %q", pingResp.ProtocolVersion, ProtocolVersion)
	}
}

func TestServer_Status(t *testing.T) {
	tmpDir, cleanup := shortTempDir(t)
	defer cleanup()
	socketPath := filepath.Join(tmpDir, "test.sock")

	ag := agent.New("test-agent", nil, nil)
	ag.SetTask("issue-42")
	ag.SetDescription("Working on tests")

	m := NewManager(ag)
	srv := NewServer(socketPath, m)
	m.SetServer(srv)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	req := &Request{
		Type: MsgHostStatus,
		ID:   "test-status",
	}
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected Success=true, got false with error: %s", resp.Error)
	}

	statusResp, err := DecodePayload[StatusResponse](resp.Payload)
	if err != nil {
		t.Fatalf("DecodePayload() error = %v", err)
	}

	if statusResp.Host.SocketPath != socketPath {
		t.Errorf("Host.SocketPath = %q, want %q", statusResp.Host.SocketPath, socketPath)
	}
	if statusResp.Agent.ID != "test-agent" {
		t.Errorf("Agent.ID = %q, want %q", statusResp.Agent.ID, "test-agent")
	}
	if statusResp.Agent.Task != "issue-42" {
		t.Errorf("Agent.Task = %q, want %q", statusResp.Agent.Task, "issue-42")
	}
	if statusResp.Agent.Description != "Working on tests" {
		t.Errorf("Agent.Description = %q, want %q", statusResp.Agent.Description, "Working on tests")
	}
}

func TestServer_List(t *testing.T) {
	tmpDir, cleanup := shortTempDir(t)
	defer cleanup()
	socketPath := filepath.Join(tmpDir, "test.sock")

	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)
	srv := NewServer(socketPath, m)
	m.SetServer(srv)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	req := &Request{
		Type: MsgHostList,
		ID:   "test-list",
	}
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected Success=true, got false with error: %s", resp.Error)
	}

	listResp, err := DecodePayload[ListResponse](resp.Payload)
	if err != nil {
		t.Fatalf("DecodePayload() error = %v", err)
	}

	if len(listResp.Agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(listResp.Agents))
	}
	if listResp.Agents[0].ID != "test-agent" {
		t.Errorf("Agents[0].ID = %q, want %q", listResp.Agents[0].ID, "test-agent")
	}
}

func TestServer_AttachDetach(t *testing.T) {
	tmpDir, cleanup := shortTempDir(t)
	defer cleanup()
	socketPath := filepath.Join(tmpDir, "test.sock")

	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)
	srv := NewServer(socketPath, m)
	m.SetServer(srv)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Attach
	attachReq := &Request{
		Type:    MsgHostAttach,
		ID:      "test-attach",
		Payload: AttachRequest{Offset: 0},
	}
	if err := encoder.Encode(attachReq); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected Success=true, got false with error: %s", resp.Error)
	}

	if srv.AttachedCount() != 1 {
		t.Errorf("AttachedCount() = %d, want 1", srv.AttachedCount())
	}

	attachResp, err := DecodePayload[AttachResponse](resp.Payload)
	if err != nil {
		t.Fatalf("DecodePayload() error = %v", err)
	}
	if attachResp.AgentID != "test-agent" {
		t.Errorf("AgentID = %q, want %q", attachResp.AgentID, "test-agent")
	}

	// Detach
	detachReq := &Request{
		Type: MsgHostDetach,
		ID:   "test-detach",
	}
	if err := encoder.Encode(detachReq); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if !resp.Success {
		t.Fatalf("expected Success=true, got false with error: %s", resp.Error)
	}

	if srv.AttachedCount() != 0 {
		t.Errorf("AttachedCount() = %d, want 0 after detach", srv.AttachedCount())
	}
}

func TestServer_Broadcast(t *testing.T) {
	tmpDir, cleanup := shortTempDir(t)
	defer cleanup()
	socketPath := filepath.Join(tmpDir, "test.sock")

	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)
	srv := NewServer(socketPath, m)
	m.SetServer(srv)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	// Attach first
	attachReq := &Request{
		Type:    MsgHostAttach,
		ID:      "test-attach",
		Payload: AttachRequest{Offset: 0},
	}
	if err := encoder.Encode(attachReq); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	// Broadcast an event
	event := &StreamEvent{
		Type:      "output",
		AgentID:   "test-agent",
		Offset:    1,
		Timestamp: time.Now().Format(time.RFC3339),
		Data:      "Hello, world!",
	}
	srv.Broadcast(event)

	// Read the broadcast event
	_ = conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
	var receivedEvent StreamEvent
	if err := decoder.Decode(&receivedEvent); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if receivedEvent.Type != "output" {
		t.Errorf("event.Type = %q, want %q", receivedEvent.Type, "output")
	}
	if receivedEvent.Data != "Hello, world!" {
		t.Errorf("event.Data = %q, want %q", receivedEvent.Data, "Hello, world!")
	}
}

func TestServer_UnknownMessageType(t *testing.T) {
	tmpDir, cleanup := shortTempDir(t)
	defer cleanup()
	socketPath := filepath.Join(tmpDir, "test.sock")

	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)
	srv := NewServer(socketPath, m)

	if err := srv.Start(); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	defer func() { _ = srv.Stop() }()

	conn, err := net.Dial("unix", socketPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer conn.Close()

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	req := &Request{
		Type: "unknown.type",
		ID:   "test-unknown",
	}
	if err := encoder.Encode(req); err != nil {
		t.Fatalf("Encode() error = %v", err)
	}

	var resp Response
	if err := decoder.Decode(&resp); err != nil {
		t.Fatalf("Decode() error = %v", err)
	}

	if resp.Success {
		t.Error("expected Success=false for unknown message type")
	}
	if resp.Error == "" {
		t.Error("expected error message for unknown message type")
	}
}

func TestServer_SocketPath(t *testing.T) {
	tmpDir, cleanup := shortTempDir(t)
	defer cleanup()
	socketPath := filepath.Join(tmpDir, "test.sock")

	ag := agent.New("test-agent", nil, nil)
	m := NewManager(ag)
	srv := NewServer(socketPath, m)

	if srv.SocketPath() != socketPath {
		t.Errorf("SocketPath() = %q, want %q", srv.SocketPath(), socketPath)
	}
}
