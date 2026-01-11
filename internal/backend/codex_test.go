package backend_test

import (
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

	t.Run("thread.started", func(t *testing.T) {
		line := `{"type":"thread.started","thread_id":"019bac20-11a2-7061-9708-dda3b7642ac3"}`
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

	t.Run("turn.started", func(t *testing.T) {
		line := `{"type":"turn.started"}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		// turn.started returns nil (no specific message needed)
		if msg != nil {
			t.Errorf("ParseStreamMessage() = %v, want nil for turn.started", msg)
		}
	})

	t.Run("turn.completed with usage", func(t *testing.T) {
		line := `{"type":"turn.completed","usage":{"input_tokens":8202,"cached_input_tokens":6400,"output_tokens":55}}`
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
		if msg.Message.Usage.InputTokens != 8202 {
			t.Errorf("InputTokens = %d, want 8202", msg.Message.Usage.InputTokens)
		}
		if msg.Message.Usage.OutputTokens != 55 {
			t.Errorf("OutputTokens = %d, want 55", msg.Message.Usage.OutputTokens)
		}
		if msg.Message.Usage.CacheReadInputTokens != 6400 {
			t.Errorf("CacheReadInputTokens = %d, want 6400", msg.Message.Usage.CacheReadInputTokens)
		}
	})

	t.Run("item.completed agent_message", func(t *testing.T) {
		line := `{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Created hello.txt with Hello World."}}`
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
		if msg.Message.Content[0].Text != "Created hello.txt with Hello World." {
			t.Errorf("Text = %q, want %q", msg.Message.Content[0].Text, "Created hello.txt with Hello World.")
		}
	})

	t.Run("item.completed reasoning", func(t *testing.T) {
		line := `{"type":"item.completed","item":{"id":"item_0","type":"reasoning","text":"**Creating a new file using shell command**"}}`
		msg, err := b.ParseStreamMessage([]byte(line))
		if err != nil {
			t.Fatalf("ParseStreamMessage() error = %v", err)
		}
		// Reasoning items are skipped (return nil)
		if msg != nil {
			t.Errorf("ParseStreamMessage() = %v, want nil for reasoning item", msg)
		}
	})

	t.Run("item.started command_execution", func(t *testing.T) {
		line := `{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"/bin/zsh -lc \"printf '%s' 'Hello World' > hello.txt\"","aggregated_output":"","exit_code":null,"status":"in_progress"}}`
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
		if block.ID != "item_1" {
			t.Errorf("block.ID = %q, want %q", block.ID, "item_1")
		}

		// Check input contains command
		var input map[string]any
		if err := json.Unmarshal(block.Input, &input); err != nil {
			t.Fatalf("failed to unmarshal input: %v", err)
		}
		cmd, ok := input["command"].(string)
		if !ok {
			t.Fatalf("input command is not string: %T", input["command"])
		}
		if cmd != "/bin/zsh -lc \"printf '%s' 'Hello World' > hello.txt\"" {
			t.Errorf("input command = %q, want /bin/zsh -lc \"printf '%%s' 'Hello World' > hello.txt\"", cmd)
		}
	})

	t.Run("item.completed command_execution success", func(t *testing.T) {
		line := `{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"/bin/zsh -lc 'cat hello.txt'","aggregated_output":"Hello World","exit_code":0,"status":"completed"}}`
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
		if block.ToolUseID != "item_1" {
			t.Errorf("block.ToolUseID = %q, want %q", block.ToolUseID, "item_1")
		}
		if string(block.Content) != "Hello World" {
			t.Errorf("block.Content = %q, want %q", block.Content, "Hello World")
		}
		if block.IsError {
			t.Error("block.IsError should be false for exit_code 0")
		}
	})

	t.Run("item.completed command_execution failed", func(t *testing.T) {
		line := `{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"/bin/zsh -lc 'cat nonexistent.txt'","aggregated_output":"cat: nonexistent.txt: No such file or directory\n","exit_code":1,"status":"failed"}}`
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

	t.Run("error", func(t *testing.T) {
		line := `{"type":"error","message":"API rate limit exceeded"}`
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
		line := `{"type":"warning","message":"Context window is 80% full"}`
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
		line := `{"type":"unknown_event","data":"something"}`
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

	t.Run("returns error for stdin messages", func(t *testing.T) {
		data, err := b.FormatInputMessage("hello world", "session-123")
		if err == nil {
			t.Fatal("FormatInputMessage() should return error")
		}
		if data != nil {
			t.Errorf("FormatInputMessage() data = %v, want nil", data)
		}
		// Verify error message indicates why
		if !strings.Contains(err.Error(), "exec resume") {
			t.Errorf("error message should mention exec resume, got: %v", err)
		}
	})
}

func TestCodexBackend_BuildResumeCommand(t *testing.T) {
	b := &backend.CodexBackend{}

	t.Run("basic resume command", func(t *testing.T) {
		cfg := backend.CommandConfig{
			WorkDir:       "/tmp/test",
			AgentID:       "test-agent",
			InitialPrompt: "follow-up message",
		}
		cmd, err := b.BuildResumeCommand(cfg, "thread-123")
		if err != nil {
			t.Fatalf("BuildResumeCommand() error = %v", err)
		}
		if cmd == nil {
			t.Fatal("BuildResumeCommand() returned nil command")
		}
		if cmd.Dir != "/tmp/test" {
			t.Errorf("BuildResumeCommand() Dir = %q, want %q", cmd.Dir, "/tmp/test")
		}

		// Check args include resume and thread ID
		args := strings.Join(cmd.Args, " ")
		if !strings.Contains(args, "exec") {
			t.Errorf("BuildResumeCommand() args should contain 'exec', got %v", cmd.Args)
		}
		if !strings.Contains(args, "resume") {
			t.Errorf("BuildResumeCommand() args should contain 'resume', got %v", cmd.Args)
		}
		if !strings.Contains(args, "--json") {
			t.Errorf("BuildResumeCommand() args should contain '--json', got %v", cmd.Args)
		}
		if !strings.Contains(args, "--full-auto") {
			t.Errorf("BuildResumeCommand() args should contain '--full-auto', got %v", cmd.Args)
		}
		if !strings.Contains(args, "thread-123") {
			t.Errorf("BuildResumeCommand() args should contain thread ID, got %v", cmd.Args)
		}
		if !strings.Contains(args, "follow-up message") {
			t.Errorf("BuildResumeCommand() args should contain prompt, got %v", cmd.Args)
		}
	})

	t.Run("resume without prompt", func(t *testing.T) {
		cfg := backend.CommandConfig{
			WorkDir: "/tmp/test",
			AgentID: "test-agent",
		}
		cmd, err := b.BuildResumeCommand(cfg, "thread-456")
		if err != nil {
			t.Fatalf("BuildResumeCommand() error = %v", err)
		}
		if cmd == nil {
			t.Fatal("BuildResumeCommand() returned nil command")
		}

		// Verify thread ID is in args but no trailing prompt
		found := false
		for i, arg := range cmd.Args {
			if arg == "thread-456" {
				found = true
				// Thread ID should be the last arg when no prompt
				if i != len(cmd.Args)-1 {
					t.Errorf("thread ID should be last arg when no prompt, got args: %v", cmd.Args)
				}
				break
			}
		}
		if !found {
			t.Errorf("BuildResumeCommand() args should contain thread ID, got %v", cmd.Args)
		}
	})

	t.Run("empty thread ID returns error", func(t *testing.T) {
		cfg := backend.CommandConfig{
			WorkDir: "/tmp/test",
			AgentID: "test-agent",
		}
		_, err := b.BuildResumeCommand(cfg, "")
		if err == nil {
			t.Fatal("BuildResumeCommand() should return error for empty thread ID")
		}
	})

	t.Run("environment includes FAB_AGENT_ID", func(t *testing.T) {
		cfg := backend.CommandConfig{
			WorkDir: "/tmp/test",
			AgentID: "my-agent-123",
		}
		cmd, err := b.BuildResumeCommand(cfg, "thread-789")
		if err != nil {
			t.Fatalf("BuildResumeCommand() error = %v", err)
		}

		found := false
		for _, env := range cmd.Env {
			if env == "FAB_AGENT_ID=my-agent-123" {
				found = true
				break
			}
		}
		if !found {
			t.Error("BuildResumeCommand() env should include FAB_AGENT_ID")
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
