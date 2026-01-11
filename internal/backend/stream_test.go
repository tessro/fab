package backend

import (
	"encoding/json"
	"testing"
)

func TestStreamMessage_IsToolUse(t *testing.T) {
	tests := []struct {
		name string
		msg  *StreamMessage
		want bool
	}{
		{
			name: "nil message",
			msg:  &StreamMessage{},
			want: false,
		},
		{
			name: "no tool_use blocks",
			msg: &StreamMessage{
				Message: &NestedMessage{
					Content: []ContentBlock{{Type: "text", Text: "hello"}},
				},
			},
			want: false,
		},
		{
			name: "has tool_use block",
			msg: &StreamMessage{
				Message: &NestedMessage{
					Content: []ContentBlock{
						{Type: "text", Text: "hello"},
						{Type: "tool_use", Name: "Bash"},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.IsToolUse(); got != tt.want {
				t.Errorf("IsToolUse() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStreamMessage_IsToolResult(t *testing.T) {
	tests := []struct {
		name string
		msg  *StreamMessage
		want bool
	}{
		{
			name: "nil message",
			msg:  &StreamMessage{},
			want: false,
		},
		{
			name: "no tool_result blocks",
			msg: &StreamMessage{
				Message: &NestedMessage{
					Content: []ContentBlock{{Type: "text", Text: "hello"}},
				},
			},
			want: false,
		},
		{
			name: "has tool_result block",
			msg: &StreamMessage{
				Message: &NestedMessage{
					Content: []ContentBlock{
						{Type: "tool_result", Content: "output"},
					},
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.IsToolResult(); got != tt.want {
				t.Errorf("IsToolResult() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestStreamMessage_GetText(t *testing.T) {
	tests := []struct {
		name string
		msg  *StreamMessage
		want string
	}{
		{
			name: "nil message",
			msg:  &StreamMessage{},
			want: "",
		},
		{
			name: "single text block",
			msg: &StreamMessage{
				Message: &NestedMessage{
					Content: []ContentBlock{{Type: "text", Text: "hello"}},
				},
			},
			want: "hello",
		},
		{
			name: "multiple text blocks",
			msg: &StreamMessage{
				Message: &NestedMessage{
					Content: []ContentBlock{
						{Type: "text", Text: "hello"},
						{Type: "text", Text: "world"},
					},
				},
			},
			want: "hello\nworld",
		},
		{
			name: "mixed blocks",
			msg: &StreamMessage{
				Message: &NestedMessage{
					Content: []ContentBlock{
						{Type: "text", Text: "before"},
						{Type: "tool_use", Name: "Bash"},
						{Type: "text", Text: "after"},
					},
				},
			},
			want: "before\nafter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.msg.GetText(); got != tt.want {
				t.Errorf("GetText() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStreamMessage_GetToolUses(t *testing.T) {
	msg := &StreamMessage{
		Message: &NestedMessage{
			Content: []ContentBlock{
				{Type: "text", Text: "hello"},
				{Type: "tool_use", Name: "Bash", ID: "tool1"},
				{Type: "tool_use", Name: "Read", ID: "tool2"},
			},
		},
	}

	tools := msg.GetToolUses()
	if len(tools) != 2 {
		t.Fatalf("GetToolUses() returned %d blocks, want 2", len(tools))
	}
	if tools[0].Name != "Bash" {
		t.Errorf("GetToolUses()[0].Name = %q, want %q", tools[0].Name, "Bash")
	}
	if tools[1].Name != "Read" {
		t.Errorf("GetToolUses()[1].Name = %q, want %q", tools[1].Name, "Read")
	}
}

func TestStreamMessage_GetToolResults(t *testing.T) {
	msg := &StreamMessage{
		Message: &NestedMessage{
			Content: []ContentBlock{
				{Type: "tool_result", ToolUseID: "tool1", Content: "output1"},
				{Type: "tool_result", ToolUseID: "tool2", Content: "output2"},
			},
		},
	}

	results := msg.GetToolResults()
	if len(results) != 2 {
		t.Fatalf("GetToolResults() returned %d blocks, want 2", len(results))
	}
	if results[0].ToolUseID != "tool1" {
		t.Errorf("GetToolResults()[0].ToolUseID = %q, want %q", results[0].ToolUseID, "tool1")
	}
}

func TestFlexContent_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		name    string
		json    string
		want    FlexContent
		wantErr bool
	}{
		{
			name: "string content",
			json: `"hello world"`,
			want: FlexContent("hello world"),
		},
		{
			name: "array content",
			json: `[{"type":"text","text":"line1"},{"type":"text","text":"line2"}]`,
			want: FlexContent("line1\nline2"),
		},
		{
			name: "empty string",
			json: `""`,
			want: FlexContent(""),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got FlexContent
			if err := json.Unmarshal([]byte(tt.json), &got); (err != nil) != tt.wantErr {
				t.Errorf("UnmarshalJSON() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("UnmarshalJSON() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestStreamMessage_ToChatEntries(t *testing.T) {
	tests := []struct {
		name      string
		msg       *StreamMessage
		wantCount int
		wantRole  string
		wantText  string
	}{
		{
			name:      "nil message",
			msg:       nil,
			wantCount: 0,
		},
		{
			name:      "no nested message",
			msg:       &StreamMessage{Type: "system"},
			wantCount: 0,
		},
		{
			name: "text content",
			msg: &StreamMessage{
				Type: "assistant",
				Message: &NestedMessage{
					Role: "assistant",
					Content: []ContentBlock{
						{Type: "text", Text: "Hello, world!"},
					},
				},
			},
			wantCount: 1,
			wantRole:  "assistant",
			wantText:  "Hello, world!",
		},
		{
			name: "tool use",
			msg: &StreamMessage{
				Type: "assistant",
				Message: &NestedMessage{
					Role: "assistant",
					Content: []ContentBlock{
						{Type: "tool_use", Name: "Bash", Input: json.RawMessage(`{"command":"ls"}`)},
					},
				},
			},
			wantCount: 1,
			wantRole:  "tool",
		},
		{
			name: "tool result",
			msg: &StreamMessage{
				Type: "user",
				Message: &NestedMessage{
					Role: "user",
					Content: []ContentBlock{
						{Type: "tool_result", Content: FlexContent("output here")},
					},
				},
			},
			wantCount: 1,
			wantRole:  "tool",
		},
		{
			name: "multiple blocks",
			msg: &StreamMessage{
				Type: "assistant",
				Message: &NestedMessage{
					Role: "assistant",
					Content: []ContentBlock{
						{Type: "text", Text: "Let me check"},
						{Type: "tool_use", Name: "Bash", Input: json.RawMessage(`{"command":"pwd"}`)},
					},
				},
			},
			wantCount: 2,
			wantRole:  "assistant",
			wantText:  "Let me check",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entries := tt.msg.ToChatEntries()
			if len(entries) != tt.wantCount {
				t.Errorf("ToChatEntries() count = %d, want %d", len(entries), tt.wantCount)
			}
			if tt.wantCount > 0 && entries[0].Role != tt.wantRole {
				t.Errorf("ToChatEntries()[0].Role = %q, want %q", entries[0].Role, tt.wantRole)
			}
			if tt.wantText != "" && entries[0].Content != tt.wantText {
				t.Errorf("ToChatEntries()[0].Content = %q, want %q", entries[0].Content, tt.wantText)
			}
		})
	}
}

func TestFormatToolInput(t *testing.T) {
	tests := []struct {
		name  string
		tool  string
		input string
		want  string
	}{
		{
			name:  "Bash command",
			tool:  "Bash",
			input: `{"command":"ls -la"}`,
			want:  "ls -la",
		},
		{
			name:  "Read file",
			tool:  "Read",
			input: `{"file_path":"/tmp/test.txt"}`,
			want:  "/tmp/test.txt",
		},
		{
			name:  "Write file",
			tool:  "Write",
			input: `{"file_path":"/tmp/out.txt","content":"hello"}`,
			want:  "/tmp/out.txt",
		},
		{
			name:  "Edit file",
			tool:  "Edit",
			input: `{"file_path":"/tmp/edit.txt","old_string":"a","new_string":"b"}`,
			want:  "/tmp/edit.txt",
		},
		{
			name:  "Glob pattern",
			tool:  "Glob",
			input: `{"pattern":"**/*.go"}`,
			want:  "**/*.go",
		},
		{
			name:  "Glob with path",
			tool:  "Glob",
			input: `{"pattern":"*.go","path":"/src"}`,
			want:  "*.go in /src",
		},
		{
			name:  "Grep pattern",
			tool:  "Grep",
			input: `{"pattern":"TODO"}`,
			want:  `"TODO"`,
		},
		{
			name:  "Grep with path",
			tool:  "Grep",
			input: `{"pattern":"TODO","path":"/src"}`,
			want:  `"TODO" in /src`,
		},
		{
			name:  "Unknown tool",
			tool:  "Custom",
			input: `{"key":"value"}`,
			want:  `key="value"`,
		},
		{
			name:  "Long bash command truncated",
			tool:  "Bash",
			input: `{"command":"` + "echo 'this is a very long command that should definitely be truncated because it exceeds one hundred characters'" + `"}`,
			want:  "echo 'this is a very long command that should definitely be truncated because it exceeds one hund...",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FormatToolInput(tt.tool, json.RawMessage(tt.input))
			if got != tt.want {
				t.Errorf("FormatToolInput() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestFormatToolInput_Empty(t *testing.T) {
	got := FormatToolInput("Bash", nil)
	if got != "" {
		t.Errorf("FormatToolInput(nil) = %q, want empty string", got)
	}
}

func TestFormatToolInput_InvalidJSON(t *testing.T) {
	got := FormatToolInput("Bash", json.RawMessage(`{invalid`))
	if got != "{invalid" {
		t.Errorf("FormatToolInput(invalid) = %q, want raw input", got)
	}
}
