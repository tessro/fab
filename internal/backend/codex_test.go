package backend_test

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/tessro/fab/internal/backend"
)

func TestCodexBackend_Name(t *testing.T) {
	b := &backend.CodexBackend{}
	if got := b.Name(); got != "codex" {
		t.Errorf("Name() = %q, want %q", got, "codex")
	}
}

func TestCodexBackend_BuildCommand(t *testing.T) {
	b := &backend.CodexBackend{}

	t.Run("basic command", func(t *testing.T) {
		cfg := backend.CommandConfig{
			WorkDir: "/tmp/test",
			AgentID: "test-agent",
		}
		cmd, err := b.BuildCommand(cfg)
		if err != nil {
			t.Fatalf("BuildCommand() error = %v", err)
		}
		if cmd == nil {
			t.Fatal("BuildCommand() returned nil command")
		}
		if cmd.Dir != "/tmp/test" {
			t.Errorf("BuildCommand() Dir = %q, want %q", cmd.Dir, "/tmp/test")
		}

		// Check that codex is the command
		if cmd.Path == "" || !strings.Contains(cmd.Path, "codex") {
			// Path might be resolved or just "codex"
			if cmd.Args[0] != "codex" {
				t.Errorf("BuildCommand() command = %q, want codex", cmd.Args[0])
			}
		}

		// Check args include exec and --json
		args := strings.Join(cmd.Args, " ")
		if !strings.Contains(args, "exec") {
			t.Errorf("BuildCommand() args should contain 'exec', got %v", cmd.Args)
		}
		if !strings.Contains(args, "--json") {
			t.Errorf("BuildCommand() args should contain '--json', got %v", cmd.Args)
		}
		if !strings.Contains(args, "--full-auto") {
			t.Errorf("BuildCommand() args should contain '--full-auto', got %v", cmd.Args)
		}
	})

	t.Run("with initial prompt", func(t *testing.T) {
		cfg := backend.CommandConfig{
			WorkDir:       "/tmp/test",
			AgentID:       "test-agent",
			InitialPrompt: "write hello world",
		}
		cmd, err := b.BuildCommand(cfg)
		if err != nil {
			t.Fatalf("BuildCommand() error = %v", err)
		}

		args := strings.Join(cmd.Args, " ")
		if !strings.Contains(args, "write hello world") {
			t.Errorf("BuildCommand() args should contain initial prompt, got %v", cmd.Args)
		}
	})

	t.Run("environment includes FAB_AGENT_ID", func(t *testing.T) {
		cfg := backend.CommandConfig{
			WorkDir: "/tmp/test",
			AgentID: "my-agent-123",
		}
		cmd, err := b.BuildCommand(cfg)
		if err != nil {
			t.Fatalf("BuildCommand() error = %v", err)
		}

		found := false
		for _, env := range cmd.Env {
			if env == "FAB_AGENT_ID=my-agent-123" {
				found = true
				break
			}
		}
		if !found {
			t.Error("BuildCommand() env should include FAB_AGENT_ID")
		}
	})
}

func TestCodexBackend_ParseStreamMessage(t *testing.T) {
	b := &backend.CodexBackend{}

	t.Run("empty line", func(t *testing.T) {
		msg, err := b.ParseStreamMessage([]byte{})
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg != nil {
			t.Errorf("ParseStreamMessage() = %v, want nil for empty line", msg)
		}
	})

	t.Run("task_started", func(t *testing.T) {
		line := `{"id":"1","msg":{"type":"task_started","model_context_window":128000}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil")
		}
		if msg.Type != "system" {
			t.Errorf("Type = %q, want %q", msg.Type, "system")
		}
		if msg.Subtype != "init" {
			t.Errorf("Subtype = %q, want %q", msg.Subtype, "init")
		}
	})

	t.Run("task_complete", func(t *testing.T) {
		line := `{"id":"1","msg":{"type":"task_complete","last_agent_message":"Done!"}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil")
		}
		if msg.Type != "result" {
			t.Errorf("Type = %q, want %q", msg.Type, "result")
		}
		if msg.Result != "Done!" {
			t.Errorf("Result = %q, want %q", msg.Result, "Done!")
		}
	})

	t.Run("agent_message", func(t *testing.T) {
		line := `{"id":"1","msg":{"type":"agent_message","message":"Hello, I can help you."}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil")
		}
		if msg.Type != "assistant" {
			t.Errorf("Type = %q, want %q", msg.Type, "assistant")
		}
		if msg.Message == nil {
			t.Fatal("Message is nil")
		}
		if msg.Message.Role != "assistant" {
			t.Errorf("Role = %q, want %q", msg.Message.Role, "assistant")
		}
		if len(msg.Message.Content) != 1 {
			t.Fatalf("Content length = %d, want 1", len(msg.Message.Content))
		}
		if msg.Message.Content[0].Text != "Hello, I can help you." {
			t.Errorf("Text = %q, want %q", msg.Message.Content[0].Text, "Hello, I can help you.")
		}
	})

	t.Run("agent_message_delta", func(t *testing.T) {
		line := `{"id":"1","msg":{"type":"agent_message_delta","delta":"Hello"}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil")
		}
		if msg.Type != "assistant" {
			t.Errorf("Type = %q, want %q", msg.Type, "assistant")
		}
		if msg.Message.Content[0].Text != "Hello" {
			t.Errorf("Text = %q, want %q", msg.Message.Content[0].Text, "Hello")
		}
	})

	t.Run("exec_command_begin", func(t *testing.T) {
		line := `{"id":"1","msg":{"type":"exec_command_begin","call_id":"call-123","command":["ls","-la"],"cwd":"/tmp"}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil")
		}
		if msg.Type != "assistant" {
			t.Errorf("Type = %q, want %q", msg.Type, "assistant")
		}
		if len(msg.Message.Content) != 1 {
			t.Fatalf("Content length = %d, want 1", len(msg.Message.Content))
		}
		block := msg.Message.Content[0]
		if block.Type != "tool_use" {
			t.Errorf("block.Type = %q, want %q", block.Type, "tool_use")
		}
		if block.Name != "Bash" {
			t.Errorf("block.Name = %q, want %q", block.Name, "Bash")
		}
		if block.ID != "call-123" {
			t.Errorf("block.ID = %q, want %q", block.ID, "call-123")
		}

		// Check input contains command
		var input map[string]any
		if err := json.Unmarshal(block.Input, &input); err != nil {
			t.Fatalf("failed to unmarshal input: %v", err)
		}
		cmd, ok := input["command"].([]any)
		if !ok {
			t.Fatalf("input command is not []any: %T", input["command"])
		}
		if len(cmd) != 2 || cmd[0] != "ls" || cmd[1] != "-la" {
			t.Errorf("input command = %v, want [ls -la]", cmd)
		}
	})

	t.Run("exec_command_output_delta", func(t *testing.T) {
		// Base64 encode "hello\n"
		chunk := base64.StdEncoding.EncodeToString([]byte("hello\n"))
		line := `{"id":"1","msg":{"type":"exec_command_output_delta","call_id":"call-123","stream":"stdout","chunk":"` + chunk + `"}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil")
		}
		if msg.Type != "user" {
			t.Errorf("Type = %q, want %q", msg.Type, "user")
		}
		if len(msg.Message.Content) != 1 {
			t.Fatalf("Content length = %d, want 1", len(msg.Message.Content))
		}
		block := msg.Message.Content[0]
		if block.Type != "tool_result" {
			t.Errorf("block.Type = %q, want %q", block.Type, "tool_result")
		}
		if block.ToolUseID != "call-123" {
			t.Errorf("block.ToolUseID = %q, want %q", block.ToolUseID, "call-123")
		}
		if string(block.Content) != "hello\n" {
			t.Errorf("block.Content = %q, want %q", block.Content, "hello\n")
		}
	})

	t.Run("exec_command_end", func(t *testing.T) {
		line := `{"id":"1","msg":{"type":"exec_command_end","call_id":"call-123","aggregated_output":"file1.txt\nfile2.txt","exit_code":0}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil")
		}
		if msg.Type != "user" {
			t.Errorf("Type = %q, want %q", msg.Type, "user")
		}
		block := msg.Message.Content[0]
		if block.Type != "tool_result" {
			t.Errorf("block.Type = %q, want %q", block.Type, "tool_result")
		}
		if string(block.Content) != "file1.txt\nfile2.txt" {
			t.Errorf("block.Content = %q, want %q", block.Content, "file1.txt\nfile2.txt")
		}
		if block.IsError {
			t.Error("block.IsError should be false for exit_code 0")
		}
	})

	t.Run("exec_command_end with error", func(t *testing.T) {
		line := `{"id":"1","msg":{"type":"exec_command_end","call_id":"call-123","aggregated_output":"command not found","exit_code":127}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil")
		}
		block := msg.Message.Content[0]
		if !block.IsError {
			t.Error("block.IsError should be true for non-zero exit_code")
		}
	})

	t.Run("token_count", func(t *testing.T) {
		line := `{"id":"1","msg":{"type":"token_count","info":{"last_token_usage":{"input_tokens":100,"cached_input_tokens":50,"output_tokens":200,"total_tokens":300}}}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil")
		}
		if msg.Type != "assistant" {
			t.Errorf("Type = %q, want %q", msg.Type, "assistant")
		}
		if msg.Message.Usage == nil {
			t.Fatal("Usage is nil")
		}
		if msg.Message.Usage.InputTokens != 100 {
			t.Errorf("InputTokens = %d, want 100", msg.Message.Usage.InputTokens)
		}
		if msg.Message.Usage.OutputTokens != 200 {
			t.Errorf("OutputTokens = %d, want 200", msg.Message.Usage.OutputTokens)
		}
		if msg.Message.Usage.CacheReadInputTokens != 50 {
			t.Errorf("CacheReadInputTokens = %d, want 50", msg.Message.Usage.CacheReadInputTokens)
		}
	})

	t.Run("error", func(t *testing.T) {
		line := `{"id":"1","msg":{"type":"error","message":"API rate limit exceeded"}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil")
		}
		if msg.Type != "result" {
			t.Errorf("Type = %q, want %q", msg.Type, "result")
		}
		if !msg.IsError {
			t.Error("IsError should be true")
		}
		if msg.Result != "API rate limit exceeded" {
			t.Errorf("Result = %q, want %q", msg.Result, "API rate limit exceeded")
		}
	})

	t.Run("warning", func(t *testing.T) {
		line := `{"id":"1","msg":{"type":"warning","message":"Context window is 80% full"}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		if msg == nil {
			t.Fatal("ParseStreamMessage() returned nil")
		}
		if msg.Type != "system" {
			t.Errorf("Type = %q, want %q", msg.Type, "system")
		}
		if msg.Subtype != "warning" {
			t.Errorf("Subtype = %q, want %q", msg.Subtype, "warning")
		}
	})

	t.Run("unknown event type", func(t *testing.T) {
		line := `{"id":"1","msg":{"type":"unknown_event","data":"something"}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		// Unknown events should return nil, not an error
		if msg != nil {
			t.Errorf("ParseStreamMessage() = %v, want nil for unknown event", msg)
		}
	})

	t.Run("invalid JSON", func(t *testing.T) {
		line := `{invalid json`
		_, err := b.ParseStreamMessage([]byte(line))
		if err == nil {
			t.Fatal("ParseStreamMessage() should return error for invalid JSON")
		}
	})
}

func TestCodexBackend_FormatInputMessage(t *testing.T) {
	b := &backend.CodexBackend{}

	t.Run("basic message", func(t *testing.T) {
		data, err := b.FormatInputMessage("hello world", "session-123")
		if err != nil {
			t.Fatalf("FormatInputMessage() error = %v", err)
		}

		// Should end with newline
		if !strings.HasSuffix(string(data), "\n") {
			t.Error("FormatInputMessage() should end with newline")
		}

		// Parse and verify structure
		var submission struct {
			ID string `json:"id"`
			Op struct {
				Type  string `json:"type"`
				Items []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"items"`
			} `json:"op"`
		}
		if err := json.Unmarshal(data, &submission); err != nil {
			t.Fatalf("failed to unmarshal: %v", err)
		}

		if submission.ID != "session-123" {
			t.Errorf("ID = %q, want %q", submission.ID, "session-123")
		}
		if submission.Op.Type != "user_input" {
			t.Errorf("Op.Type = %q, want %q", submission.Op.Type, "user_input")
		}
		if len(submission.Op.Items) != 1 {
			t.Fatalf("Items length = %d, want 1", len(submission.Op.Items))
		}
		if submission.Op.Items[0].Type != "text" {
			t.Errorf("Items[0].Type = %q, want %q", submission.Op.Items[0].Type, "text")
		}
		if submission.Op.Items[0].Text != "hello world" {
			t.Errorf("Items[0].Text = %q, want %q", submission.Op.Items[0].Text, "hello world")
		}
	})
}

func TestCodexBackend_HookSettings(t *testing.T) {
	b := &backend.CodexBackend{}

	settings := b.HookSettings("/usr/bin/fab")
	// Codex doesn't use fab-style hooks
	if settings != nil {
		t.Errorf("HookSettings() = %v, want nil", settings)
	}
}

// Compile-time check that CodexBackend implements Backend.
var _ backend.Backend = (*backend.CodexBackend)(nil)

func TestCodexBackend_RegisteredInRegistry(t *testing.T) {
	// Verify that codex backend is registered via init()
	b, err := backend.Get("codex")
	if err != nil {
		t.Fatalf("Get(\"codex\") error = %v", err)
	}
	if b == nil {
		t.Fatal("Get(\"codex\") returned nil")
	}
	if b.Name() != "codex" {
		t.Errorf("Name() = %q, want %q", b.Name(), "codex")
	}

	// Verify codex appears in List()
	names := backend.List()
	found := false
	for _, name := range names {
		if name == "codex" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("List() = %v, want to include \"codex\"", names)
	}
}
