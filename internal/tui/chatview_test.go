package tui

import (
	"testing"

	"github.com/tessro/fab/internal/daemon"
)

func TestSummarizeToolResult(t *testing.T) {
	tests := []struct {
		name     string
		toolName string
		result   string
		maxWidth int
		want     string
	}{
		{
			name:     "Read single line",
			toolName: "Read",
			result:   "hello world",
			maxWidth: 80,
			want:     "Read 1 line",
		},
		{
			name:     "Read multiple lines",
			toolName: "Read",
			result:   "line1\nline2\nline3",
			maxWidth: 80,
			want:     "Read 3 lines",
		},
		{
			name:     "Read with trailing newline",
			toolName: "Read",
			result:   "line1\nline2\n",
			maxWidth: 80,
			want:     "Read 2 lines",
		},
		{
			name:     "Read 152 lines",
			toolName: "Read",
			result:   generateLines(152),
			maxWidth: 80,
			want:     "Read 152 lines",
		},
		{
			name:     "Read empty",
			toolName: "Read",
			result:   "",
			maxWidth: 80,
			want:     "Read 0 lines",
		},
		{
			name:     "Grep single match",
			toolName: "Grep",
			result:   "file.go:10: match here",
			maxWidth: 80,
			want:     "1 match",
		},
		{
			name:     "Grep multiple matches",
			toolName: "Grep",
			result:   "file1.go:10: match1\nfile2.go:20: match2\nfile3.go:30: match3",
			maxWidth: 80,
			want:     "3 matches",
		},
		{
			name:     "Grep no matches",
			toolName: "Grep",
			result:   "",
			maxWidth: 80,
			want:     "No matches",
		},
		{
			name:     "Grep with empty lines",
			toolName: "Grep",
			result:   "file.go:10: match\n\n",
			maxWidth: 80,
			want:     "1 match",
		},
		{
			name:     "Other tool falls back to truncateResult",
			toolName: "Bash",
			result:   "output line 1",
			maxWidth: 80,
			want:     "output line 1",
		},
		{
			name:     "Unknown tool falls back to truncateResult",
			toolName: "",
			result:   "some result",
			maxWidth: 80,
			want:     "some result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := summarizeToolResult(tt.toolName, tt.result, tt.maxWidth)
			if got != tt.want {
				t.Errorf("summarizeToolResult(%q, result, %d) = %q, want %q", tt.toolName, tt.maxWidth, got, tt.want)
			}
		})
	}
}

// generateLines creates a string with n lines.
func generateLines(n int) string {
	s := ""
	for i := 0; i < n; i++ {
		if i > 0 {
			s += "\n"
		}
		s += "line"
	}
	return s
}

func TestFormatLineCount(t *testing.T) {
	tests := []struct {
		count int
		want  string
	}{
		{0, "0 lines"},
		{1, "1 line"},
		{2, "2 lines"},
		{100, "100 lines"},
		{1000, "1000 lines"},
	}

	for _, tt := range tests {
		got := formatLineCount(tt.count)
		if got != tt.want {
			t.Errorf("formatLineCount(%d) = %q, want %q", tt.count, got, tt.want)
		}
	}
}

func TestFormatMatchCount(t *testing.T) {
	tests := []struct {
		count int
		want  string
	}{
		{0, "0 matches"},
		{1, "1 match"},
		{2, "2 matches"},
		{100, "100 matches"},
	}

	for _, tt := range tests {
		got := formatMatchCount(tt.count)
		if got != tt.want {
			t.Errorf("formatMatchCount(%d) = %q, want %q", tt.count, got, tt.want)
		}
	}
}

func TestFormatTime(t *testing.T) {
	tests := []struct {
		name      string
		timestamp string
		want      string
	}{
		{
			name:      "valid RFC3339 morning",
			timestamp: "2024-01-15T09:30:00Z",
			want:      "9:30 AM",
		},
		{
			name:      "valid RFC3339 afternoon",
			timestamp: "2024-01-15T14:45:00Z",
			want:      "2:45 PM",
		},
		{
			name:      "valid RFC3339 midnight",
			timestamp: "2024-01-15T00:00:00Z",
			want:      "12:00 AM",
		},
		{
			name:      "valid RFC3339 noon",
			timestamp: "2024-01-15T12:00:00Z",
			want:      "12:00 PM",
		},
		{
			name:      "valid RFC3339 with timezone",
			timestamp: "2024-01-15T14:45:00-05:00",
			want:      "2:45 PM",
		},
		{
			name:      "empty timestamp",
			timestamp: "",
			want:      "",
		},
		{
			name:      "invalid timestamp",
			timestamp: "not-a-date",
			want:      "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatTime(tt.timestamp)
			if got != tt.want {
				t.Errorf("formatTime(%q) = %q, want %q", tt.timestamp, got, tt.want)
			}
		})
	}
}

func TestChatViewSetEntriesMerge(t *testing.T) {
	// Test that SetEntries properly merges historical entries with streaming entries
	// that arrived during the history fetch. This is critical for reconnection logic.

	tests := []struct {
		name           string
		existing       []daemon.ChatEntryDTO // entries already in chat view (from streaming)
		history        []daemon.ChatEntryDTO // entries fetched from history API
		wantCount      int
		wantLastRole   string
		wantLastTime   string
	}{
		{
			name:     "empty history keeps existing streaming entries",
			existing: []daemon.ChatEntryDTO{
				{Role: "user", Content: "hello", Timestamp: "2024-01-15T10:00:00Z"},
			},
			history:      []daemon.ChatEntryDTO{},
			wantCount:    1,
			wantLastRole: "user",
			wantLastTime: "2024-01-15T10:00:00Z",
		},
		{
			name:     "history replaces older streaming entries",
			existing: []daemon.ChatEntryDTO{
				{Role: "user", Content: "old streaming entry", Timestamp: "2024-01-15T09:00:00Z"},
			},
			history: []daemon.ChatEntryDTO{
				{Role: "user", Content: "history entry 1", Timestamp: "2024-01-15T10:00:00Z"},
				{Role: "assistant", Content: "history entry 2", Timestamp: "2024-01-15T10:01:00Z"},
			},
			wantCount:    2,
			wantLastRole: "assistant",
			wantLastTime: "2024-01-15T10:01:00Z",
		},
		{
			name:     "streaming entries newer than history are preserved",
			existing: []daemon.ChatEntryDTO{
				{Role: "user", Content: "old", Timestamp: "2024-01-15T09:00:00Z"},
				{Role: "assistant", Content: "new streaming", Timestamp: "2024-01-15T10:05:00Z"},
			},
			history: []daemon.ChatEntryDTO{
				{Role: "user", Content: "history entry 1", Timestamp: "2024-01-15T10:00:00Z"},
				{Role: "assistant", Content: "history entry 2", Timestamp: "2024-01-15T10:01:00Z"},
			},
			wantCount:    3, // 2 history + 1 newer streaming
			wantLastRole: "assistant",
			wantLastTime: "2024-01-15T10:05:00Z",
		},
		{
			name:     "multiple newer streaming entries preserved",
			existing: []daemon.ChatEntryDTO{
				{Role: "user", Content: "old", Timestamp: "2024-01-15T09:00:00Z"},
				{Role: "assistant", Content: "streaming 1", Timestamp: "2024-01-15T10:05:00Z"},
				{Role: "user", Content: "streaming 2", Timestamp: "2024-01-15T10:06:00Z"},
			},
			history: []daemon.ChatEntryDTO{
				{Role: "user", Content: "history entry", Timestamp: "2024-01-15T10:00:00Z"},
			},
			wantCount:    3, // 1 history + 2 newer streaming
			wantLastRole: "user",
			wantLastTime: "2024-01-15T10:06:00Z",
		},
		{
			name:         "empty existing with history loads all",
			existing:     []daemon.ChatEntryDTO{},
			history: []daemon.ChatEntryDTO{
				{Role: "user", Content: "history 1", Timestamp: "2024-01-15T10:00:00Z"},
				{Role: "assistant", Content: "history 2", Timestamp: "2024-01-15T10:01:00Z"},
			},
			wantCount:    2,
			wantLastRole: "assistant",
			wantLastTime: "2024-01-15T10:01:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cv := NewChatView()
			// Initialize viewport with a size
			cv.SetSize(80, 24)
			cv.SetAgent("test-agent", "test-project", "claude")

			// Add existing entries (simulating streaming entries)
			for _, e := range tt.existing {
				cv.AppendEntry(e)
			}

			// Set history entries (simulating reconnect fetch)
			cv.SetEntries(tt.history)

			// Verify results
			if len(cv.entries) != tt.wantCount {
				t.Errorf("entry count = %d, want %d", len(cv.entries), tt.wantCount)
			}

			if tt.wantCount > 0 {
				last := cv.entries[len(cv.entries)-1]
				if last.Role != tt.wantLastRole {
					t.Errorf("last entry role = %q, want %q", last.Role, tt.wantLastRole)
				}
				if last.Timestamp != tt.wantLastTime {
					t.Errorf("last entry timestamp = %q, want %q", last.Timestamp, tt.wantLastTime)
				}
			}
		})
	}
}

func TestChatViewSetEntriesPreservesOrder(t *testing.T) {
	// Verify that merged entries maintain chronological order
	cv := NewChatView()
	cv.SetSize(80, 24)
	cv.SetAgent("test-agent", "test-project", "claude")

	// Add streaming entries - one older than history, two newer
	cv.AppendEntry(daemon.ChatEntryDTO{Role: "user", Content: "very old", Timestamp: "2024-01-15T08:00:00Z"})
	cv.AppendEntry(daemon.ChatEntryDTO{Role: "assistant", Content: "streaming new 1", Timestamp: "2024-01-15T10:05:00Z"})
	cv.AppendEntry(daemon.ChatEntryDTO{Role: "user", Content: "streaming new 2", Timestamp: "2024-01-15T10:06:00Z"})

	// Set history
	history := []daemon.ChatEntryDTO{
		{Role: "user", Content: "history 1", Timestamp: "2024-01-15T10:00:00Z"},
		{Role: "assistant", Content: "history 2", Timestamp: "2024-01-15T10:01:00Z"},
		{Role: "user", Content: "history 3", Timestamp: "2024-01-15T10:02:00Z"},
	}
	cv.SetEntries(history)

	// Should have: 3 history + 2 newer streaming = 5 entries
	if len(cv.entries) != 5 {
		t.Errorf("entry count = %d, want 5", len(cv.entries))
	}

	// Verify order by checking content
	expected := []string{"history 1", "history 2", "history 3", "streaming new 1", "streaming new 2"}
	for i, want := range expected {
		if cv.entries[i].Content != want {
			t.Errorf("entry[%d].Content = %q, want %q", i, cv.entries[i].Content, want)
		}
	}
}
