package backend

import (
	"encoding/json"
	"os/exec"
	"testing"
)

func TestClaudeBackend_Name(t *testing.T) {
	b := &ClaudeBackend{}
	if got := b.Name(); got != "claude" {
		t.Errorf("Name() = %q, want %q", got, "claude")
	}
}

func TestClaudeBackend_BuildCommand(t *testing.T) {
	b := &ClaudeBackend{}

	cfg := CommandConfig{
		WorkDir:   "/tmp/test",
		AgentID:   "abc123",
		PluginDir: "/tmp/plugins",
	}

	cmd, err := b.BuildCommand(cfg)
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	// Verify command is for claude
	if cmd.Path == "" {
		t.Error("BuildCommand() returned command with empty Path")
	}

	// Verify working directory is set
	if cmd.Dir != cfg.WorkDir {
		t.Errorf("BuildCommand() Dir = %q, want %q", cmd.Dir, cfg.WorkDir)
	}

	// Verify agent ID is set in environment
	found := false
	for _, env := range cmd.Env {
		if env == "FAB_AGENT_ID=abc123" {
			found = true
			break
		}
	}
	if !found {
		t.Error("BuildCommand() did not set FAB_AGENT_ID in environment")
	}

	// Verify required arguments are present
	args := cmd.Args
	checkArg := func(flag, value string) {
		for i := 0; i < len(args)-1; i++ {
			if args[i] == flag && args[i+1] == value {
				return
			}
		}
		t.Errorf("BuildCommand() missing argument %s %s", flag, value)
	}

	checkArg("--output-format", "stream-json")
	checkArg("--input-format", "stream-json")
	checkArg("--permission-mode", "default")
	checkArg("--plugin-dir", cfg.PluginDir)
}

func TestClaudeBackend_ParseStreamMessage(t *testing.T) {
	b := &ClaudeBackend{}

	tests := []struct {
		name    string
		input   string
		wantNil bool
		wantErr bool
	}{
		{
			name:    "empty line",
			input:   "",
			wantNil: true,
			wantErr: false,
		},
		{
			name:    "system message",
			input:   `{"type":"system","subtype":"init"}`,
			wantNil: false,
			wantErr: false,
		},
		{
			name:    "assistant message with text",
			input:   `{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"Hello"}]}}`,
			wantNil: false,
			wantErr: false,
		},
		{
			name:    "invalid json",
			input:   `{invalid json}`,
			wantNil: true,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg, err := b.ParseStreamMessage([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseStreamMessage() error = %v, wantErr %v", err, tt.wantErr)
			}
			if (msg == nil) != tt.wantNil {
				t.Errorf("ParseStreamMessage() msg = %v, wantNil %v", msg, tt.wantNil)
			}
		})
	}
}

func TestClaudeBackend_FormatInputMessage(t *testing.T) {
	b := &ClaudeBackend{}

	// Test with default session ID
	data, err := b.FormatInputMessage("Hello, Claude!", "")
	if err != nil {
		t.Fatalf("FormatInputMessage() error = %v", err)
	}

	// Verify it's valid JSON
	var msg InputMessage
	if err := json.Unmarshal(data[:len(data)-1], &msg); err != nil { // -1 to remove newline
		t.Fatalf("FormatInputMessage() produced invalid JSON: %v", err)
	}

	// Verify fields
	if msg.Type != "user" {
		t.Errorf("FormatInputMessage() type = %q, want %q", msg.Type, "user")
	}
	if msg.Message.Role != "user" {
		t.Errorf("FormatInputMessage() role = %q, want %q", msg.Message.Role, "user")
	}
	if msg.Message.Content != "Hello, Claude!" {
		t.Errorf("FormatInputMessage() content = %q, want %q", msg.Message.Content, "Hello, Claude!")
	}
	if msg.SessionID != "default" {
		t.Errorf("FormatInputMessage() session_id = %q, want %q", msg.SessionID, "default")
	}

	// Verify ends with newline
	if data[len(data)-1] != '\n' {
		t.Error("FormatInputMessage() result does not end with newline")
	}

	// Test with custom session ID
	data2, err := b.FormatInputMessage("Test", "custom-session")
	if err != nil {
		t.Fatalf("FormatInputMessage() with custom session error = %v", err)
	}

	var msg2 InputMessage
	if err := json.Unmarshal(data2[:len(data2)-1], &msg2); err != nil {
		t.Fatalf("FormatInputMessage() produced invalid JSON: %v", err)
	}
	if msg2.SessionID != "custom-session" {
		t.Errorf("FormatInputMessage() session_id = %q, want %q", msg2.SessionID, "custom-session")
	}
}

func TestClaudeBackend_HookSettings(t *testing.T) {
	b := &ClaudeBackend{}

	settings := b.HookSettings("/usr/local/bin/fab")

	// Verify hooks structure exists
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		t.Fatal("HookSettings() missing 'hooks' key")
	}

	// Verify required hooks are present
	for _, hookName := range []string{"PreToolUse", "PermissionRequest", "Stop"} {
		if _, ok := hooks[hookName]; !ok {
			t.Errorf("HookSettings() missing hook %q", hookName)
		}
	}

	// Verify PreToolUse hook has correct command
	preToolUse, ok := hooks["PreToolUse"].([]any)
	if !ok || len(preToolUse) == 0 {
		t.Fatal("HookSettings() PreToolUse is not a non-empty array")
	}

	entry, ok := preToolUse[0].(map[string]any)
	if !ok {
		t.Fatal("HookSettings() PreToolUse entry is not a map")
	}

	hookArray, ok := entry["hooks"].([]any)
	if !ok || len(hookArray) == 0 {
		t.Fatal("HookSettings() PreToolUse hooks is not a non-empty array")
	}

	hookDef, ok := hookArray[0].(map[string]any)
	if !ok {
		t.Fatal("HookSettings() PreToolUse hook definition is not a map")
	}

	command, ok := hookDef["command"].(string)
	if !ok {
		t.Fatal("HookSettings() PreToolUse hook missing command")
	}
	if command != "/usr/local/bin/fab hook PreToolUse" {
		t.Errorf("HookSettings() PreToolUse command = %q, want %q", command, "/usr/local/bin/fab hook PreToolUse")
	}
}

func TestClaudeBackend_ImplementsBackend(t *testing.T) {
	// Compile-time check that ClaudeBackend implements Backend
	var _ Backend = (*ClaudeBackend)(nil)
}

func TestClaudeBackend_BuildCommand_EmptyWorkDir(t *testing.T) {
	b := &ClaudeBackend{}

	cfg := CommandConfig{
		WorkDir: "", // Empty work dir
		AgentID: "test123",
	}

	cmd, err := b.BuildCommand(cfg)
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	// When work dir is empty, cmd.Dir should be empty (uses current directory)
	if cmd.Dir != "" {
		t.Errorf("BuildCommand() Dir = %q, want empty string", cmd.Dir)
	}
}

func TestClaudeBackend_BuildCommand_DefaultPluginDir(t *testing.T) {
	b := &ClaudeBackend{}

	cfg := CommandConfig{
		WorkDir:   "/tmp/test",
		AgentID:   "test123",
		PluginDir: "", // Empty plugin dir should use default
	}

	cmd, err := b.BuildCommand(cfg)
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	// Verify plugin dir argument exists (should use default)
	var pluginDir string
	for i := 0; i < len(cmd.Args)-1; i++ {
		if cmd.Args[i] == "--plugin-dir" {
			pluginDir = cmd.Args[i+1]
			break
		}
	}
	if pluginDir == "" {
		t.Error("BuildCommand() missing --plugin-dir argument")
	}
}

func TestClaudeBackend_BuildCommand_ReturnsExecCmd(t *testing.T) {
	b := &ClaudeBackend{}

	cmd, err := b.BuildCommand(CommandConfig{AgentID: "test"})
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	// Verify it's an *exec.Cmd
	if _, ok := interface{}(cmd).(*exec.Cmd); !ok {
		t.Error("BuildCommand() did not return *exec.Cmd")
	}
}
