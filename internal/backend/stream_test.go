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
