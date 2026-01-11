package tui

import "testing"

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
