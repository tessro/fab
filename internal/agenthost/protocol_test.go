package agenthost

import (
	"encoding/json"
	"testing"
	"time"
)

func TestRequestEncodeDecode(t *testing.T) {
	tests := []struct {
		name string
		req  Request
	}{
		{
			name: "ping request without payload",
			req: Request{
				Type: MsgHostPing,
				ID:   "req-1",
			},
		},
		{
			name: "attach request with payload",
			req: Request{
				Type: MsgHostAttach,
				ID:   "req-2",
				Payload: AttachRequest{
					Offset: 1024,
				},
			},
		},
		{
			name: "send request with input",
			req: Request{
				Type: MsgHostSend,
				ID:   "req-3",
				Payload: SendRequest{
					Input: "hello world",
				},
			},
		},
		{
			name: "stop request with options",
			req: Request{
				Type: MsgHostStop,
				ID:   "req-4",
				Payload: StopRequest{
					Force:   true,
					Timeout: 60,
					Reason:  "user requested",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			data, err := json.Marshal(tt.req)
			if err != nil {
				t.Fatalf("failed to marshal request: %v", err)
			}

			// Decode
			var decoded Request
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal request: %v", err)
			}

			// Verify type and ID
			if decoded.Type != tt.req.Type {
				t.Errorf("type mismatch: got %q, want %q", decoded.Type, tt.req.Type)
			}
			if decoded.ID != tt.req.ID {
				t.Errorf("ID mismatch: got %q, want %q", decoded.ID, tt.req.ID)
			}
		})
	}
}

func TestResponseEncodeDecode(t *testing.T) {
	startTime := time.Now().UTC().Truncate(time.Second)

	tests := []struct {
		name string
		resp Response
	}{
		{
			name: "success ping response",
			resp: Response{
				Type:    MsgHostPing,
				ID:      "req-1",
				Success: true,
				Payload: PingResponse{
					Version:         "1.0.0",
					ProtocolVersion: ProtocolVersion,
					Uptime:          "1h30m",
					StartedAt:       startTime,
				},
			},
		},
		{
			name: "error response",
			resp: Response{
				Type:    MsgHostStatus,
				ID:      "req-2",
				Success: false,
				Error:   "agent not found",
			},
		},
		{
			name: "status response with full data",
			resp: Response{
				Type:    MsgHostStatus,
				ID:      "req-3",
				Success: true,
				Payload: StatusResponse{
					Host: HostInfo{
						PID:             12345,
						Version:         "1.0.0",
						ProtocolVersion: ProtocolVersion,
						StartedAt:       startTime,
						SocketPath:      "/tmp/test.sock",
					},
					Agent: AgentInfo{
						ID:          "abc123",
						Project:     "myproject",
						State:       "running",
						PID:         12346,
						Worktree:    "/path/to/worktree",
						StartedAt:   startTime,
						Task:        "issue-42",
						Description: "Implementing feature X",
						Backend:     "claude",
					},
				},
			},
		},
		{
			name: "list response",
			resp: Response{
				Type:    MsgHostList,
				ID:      "req-4",
				Success: true,
				Payload: ListResponse{
					Agents: []AgentInfo{
						{
							ID:        "agent-1",
							Project:   "proj1",
							State:     "running",
							StartedAt: startTime,
						},
						{
							ID:        "agent-2",
							Project:   "proj2",
							State:     "idle",
							StartedAt: startTime,
						},
					},
				},
			},
		},
		{
			name: "attach response",
			resp: Response{
				Type:    MsgHostAttach,
				ID:      "req-5",
				Success: true,
				Payload: AttachResponse{
					AgentID:      "abc123",
					StreamOffset: 2048,
				},
			},
		},
		{
			name: "stop response",
			resp: Response{
				Type:    MsgHostStop,
				ID:      "req-6",
				Success: true,
				Payload: StopResponse{
					Stopped:    true,
					ExitCode:   0,
					Graceful:   true,
					Duration:   "5s",
					FinalState: "done",
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			data, err := json.Marshal(tt.resp)
			if err != nil {
				t.Fatalf("failed to marshal response: %v", err)
			}

			// Decode
			var decoded Response
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal response: %v", err)
			}

			// Verify envelope fields
			if decoded.Type != tt.resp.Type {
				t.Errorf("type mismatch: got %q, want %q", decoded.Type, tt.resp.Type)
			}
			if decoded.ID != tt.resp.ID {
				t.Errorf("ID mismatch: got %q, want %q", decoded.ID, tt.resp.ID)
			}
			if decoded.Success != tt.resp.Success {
				t.Errorf("success mismatch: got %v, want %v", decoded.Success, tt.resp.Success)
			}
			if decoded.Error != tt.resp.Error {
				t.Errorf("error mismatch: got %q, want %q", decoded.Error, tt.resp.Error)
			}
		})
	}
}

func TestStreamEventEncodeDecode(t *testing.T) {
	timestamp := time.Now().UTC().Format(time.RFC3339)

	tests := []struct {
		name  string
		event StreamEvent
	}{
		{
			name: "output event",
			event: StreamEvent{
				Type:      "output",
				AgentID:   "abc123",
				Offset:    1024,
				Timestamp: timestamp,
				Data:      "Hello from Claude Code",
			},
		},
		{
			name: "state event",
			event: StreamEvent{
				Type:      "state",
				AgentID:   "abc123",
				Offset:    2048,
				Timestamp: timestamp,
				State:     "idle",
			},
		},
		{
			name: "error event",
			event: StreamEvent{
				Type:      "error",
				AgentID:   "abc123",
				Offset:    3072,
				Timestamp: timestamp,
				Error:     "connection lost",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Encode
			data, err := json.Marshal(tt.event)
			if err != nil {
				t.Fatalf("failed to marshal event: %v", err)
			}

			// Decode
			var decoded StreamEvent
			if err := json.Unmarshal(data, &decoded); err != nil {
				t.Fatalf("failed to unmarshal event: %v", err)
			}

			// Verify fields
			if decoded.Type != tt.event.Type {
				t.Errorf("type mismatch: got %q, want %q", decoded.Type, tt.event.Type)
			}
			if decoded.AgentID != tt.event.AgentID {
				t.Errorf("agent_id mismatch: got %q, want %q", decoded.AgentID, tt.event.AgentID)
			}
			if decoded.Offset != tt.event.Offset {
				t.Errorf("offset mismatch: got %d, want %d", decoded.Offset, tt.event.Offset)
			}
			if decoded.Timestamp != tt.event.Timestamp {
				t.Errorf("timestamp mismatch: got %q, want %q", decoded.Timestamp, tt.event.Timestamp)
			}
			if decoded.Data != tt.event.Data {
				t.Errorf("data mismatch: got %q, want %q", decoded.Data, tt.event.Data)
			}
			if decoded.State != tt.event.State {
				t.Errorf("state mismatch: got %q, want %q", decoded.State, tt.event.State)
			}
			if decoded.Error != tt.event.Error {
				t.Errorf("error mismatch: got %q, want %q", decoded.Error, tt.event.Error)
			}
		})
	}
}

func TestUnmarshalPayload(t *testing.T) {
	tests := []struct {
		name    string
		payload any
		want    AttachRequest
		wantErr bool
	}{
		{
			name:    "nil payload",
			payload: nil,
			want:    AttachRequest{},
		},
		{
			name: "map payload",
			payload: map[string]any{
				"offset": float64(1024), // JSON numbers decode as float64
			},
			want: AttachRequest{Offset: 1024},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got AttachRequest
			err := UnmarshalPayload(tt.payload, &got)
			if (err != nil) != tt.wantErr {
				t.Fatalf("UnmarshalPayload() error = %v, wantErr %v", err, tt.wantErr)
			}
			if got.Offset != tt.want.Offset {
				t.Errorf("UnmarshalPayload() offset = %d, want %d", got.Offset, tt.want.Offset)
			}
		})
	}
}

func TestDecodePayload(t *testing.T) {
	payload := map[string]any{
		"agent_id":      "abc123",
		"stream_offset": float64(2048),
	}

	result, err := DecodePayload[AttachResponse](payload)
	if err != nil {
		t.Fatalf("DecodePayload() error = %v", err)
	}

	if result.AgentID != "abc123" {
		t.Errorf("agent_id = %q, want %q", result.AgentID, "abc123")
	}
	if result.StreamOffset != 2048 {
		t.Errorf("stream_offset = %d, want %d", result.StreamOffset, 2048)
	}
}

func TestSuccessResponse(t *testing.T) {
	req := &Request{
		Type: MsgHostPing,
		ID:   "test-123",
	}
	payload := PingResponse{
		Version:         "1.0.0",
		ProtocolVersion: ProtocolVersion,
	}

	resp := SuccessResponse(req, payload)

	if resp.Type != req.Type {
		t.Errorf("Type = %q, want %q", resp.Type, req.Type)
	}
	if resp.ID != req.ID {
		t.Errorf("ID = %q, want %q", resp.ID, req.ID)
	}
	if !resp.Success {
		t.Error("Success = false, want true")
	}
	if resp.Error != "" {
		t.Errorf("Error = %q, want empty", resp.Error)
	}
	if resp.Payload == nil {
		t.Error("Payload = nil, want non-nil")
	}
}

func TestErrorResponse(t *testing.T) {
	req := &Request{
		Type: MsgHostStatus,
		ID:   "test-456",
	}
	errMsg := "something went wrong"

	resp := ErrorResponse(req, errMsg)

	if resp.Type != req.Type {
		t.Errorf("Type = %q, want %q", resp.Type, req.Type)
	}
	if resp.ID != req.ID {
		t.Errorf("ID = %q, want %q", resp.ID, req.ID)
	}
	if resp.Success {
		t.Error("Success = true, want false")
	}
	if resp.Error != errMsg {
		t.Errorf("Error = %q, want %q", resp.Error, errMsg)
	}
	if resp.Payload != nil {
		t.Errorf("Payload = %v, want nil", resp.Payload)
	}
}

func TestMessageTypeConstants(t *testing.T) {
	// Verify message types use consistent naming pattern
	types := []struct {
		mt   MessageType
		want string
	}{
		{MsgHostPing, "host.ping"},
		{MsgHostStatus, "host.status"},
		{MsgHostList, "host.list"},
		{MsgHostAttach, "host.attach"},
		{MsgHostDetach, "host.detach"},
		{MsgHostSend, "host.send"},
		{MsgHostStop, "host.stop"},
	}

	for _, tt := range types {
		if string(tt.mt) != tt.want {
			t.Errorf("MessageType %q, want %q", tt.mt, tt.want)
		}
	}
}

func TestProtocolVersionConstants(t *testing.T) {
	if ProtocolVersion == "" {
		t.Error("ProtocolVersion is empty")
	}
	if MinProtocolVersion == "" {
		t.Error("MinProtocolVersion is empty")
	}
	// MinProtocolVersion should be <= ProtocolVersion
	if MinProtocolVersion > ProtocolVersion {
		t.Errorf("MinProtocolVersion %q > ProtocolVersion %q", MinProtocolVersion, ProtocolVersion)
	}
}
