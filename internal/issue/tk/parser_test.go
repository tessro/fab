package tk

import (
	"slices"
	"testing"
	"time"

	"github.com/tessro/fab/internal/issue"
)

func TestParseIssue(t *testing.T) {
	created := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		input   string
		want    *issue.Issue
		wantErr bool
	}{
		{
			name: "valid issue with all fields",
			input: `---
id: fa-123
status: open
type: feature
priority: 2
deps: [fa-100]
labels: [backend, api]
links: [https://example.com]
created: 2024-01-15T10:00:00Z
---
# Add user authentication

Implement OAuth2 login flow.
`,
			want: &issue.Issue{
				ID:           "fa-123",
				Title:        "Add user authentication",
				Description:  "Implement OAuth2 login flow.",
				Status:       issue.StatusOpen,
				Type:         "feature",
				Priority:     2,
				Dependencies: []string{"fa-100"},
				Labels:       []string{"backend", "api"},
				Links:        []string{"https://example.com"},
				Created:      created,
			},
		},
		{
			name: "minimal fields",
			input: `---
id: fa-1
status: closed
---
# Simple task
`,
			want: &issue.Issue{
				ID:     "fa-1",
				Title:  "Simple task",
				Status: issue.StatusClosed,
			},
		},
		{
			name: "empty description",
			input: `---
id: fa-2
status: open
---
# Title only
`,
			want: &issue.Issue{
				ID:     "fa-2",
				Title:  "Title only",
				Status: issue.StatusOpen,
			},
		},
		{
			name: "multi-line description",
			input: `---
id: fa-3
status: open
---
# Feature request

This is the first paragraph.

This is the second paragraph with more details.

- Bullet point 1
- Bullet point 2
`,
			want: &issue.Issue{
				ID:          "fa-3",
				Title:       "Feature request",
				Description: "This is the first paragraph.\n\nThis is the second paragraph with more details.\n\n- Bullet point 1\n- Bullet point 2",
				Status:      issue.StatusOpen,
			},
		},
		{
			name: "description with markdown formatting",
			input: `---
id: fa-4
status: open
---
# Code task

Here is some **bold** and *italic* text.

` + "```go" + `
func main() {}
` + "```" + `
`,
			want: &issue.Issue{
				ID:          "fa-4",
				Title:       "Code task",
				Description: "Here is some **bold** and *italic* text.\n\n```go\nfunc main() {}\n```",
				Status:      issue.StatusOpen,
			},
		},
		{
			name: "multiple dependencies",
			input: `---
id: fa-5
status: blocked
deps: [fa-1, fa-2, fa-3]
---
# Blocked task

Waiting on dependencies.
`,
			want: &issue.Issue{
				ID:           "fa-5",
				Title:        "Blocked task",
				Description:  "Waiting on dependencies.",
				Status:       issue.StatusBlocked,
				Dependencies: []string{"fa-1", "fa-2", "fa-3"},
			},
		},
		{
			name: "unicode content",
			input: `---
id: fa-6
status: open
labels: [日本語, émoji]
---
# 日本語のタイトル

Description with émojis 🎉 and ünïcödé.
`,
			want: &issue.Issue{
				ID:          "fa-6",
				Title:       "日本語のタイトル",
				Description: "Description with émojis 🎉 and ünïcödé.",
				Status:      issue.StatusOpen,
				Labels:      []string{"日本語", "émoji"},
			},
		},
		{
			name: "title with special characters",
			input: `---
id: fa-7
status: open
---
# Fix bug in foo() -> bar() conversion [urgent]

Details here.
`,
			want: &issue.Issue{
				ID:          "fa-7",
				Title:       "Fix bug in foo() -> bar() conversion [urgent]",
				Description: "Details here.",
				Status:      issue.StatusOpen,
			},
		},
		{
			name:    "missing opening delimiter",
			input:   "id: fa-123\n---\n# Title",
			wantErr: true,
		},
		{
			name:    "missing closing delimiter",
			input:   "---\nid: fa-123\n# Title",
			wantErr: true,
		},
		{
			name:    "empty file",
			input:   "",
			wantErr: true,
		},
		{
			name:    "only whitespace",
			input:   "   \n\t\n  ",
			wantErr: true,
		},
		{
			name:    "invalid yaml syntax",
			input:   "---\nid: [unclosed\n---\n# Title",
			wantErr: true,
		},
		{
			name: "extra dashes in body",
			input: `---
id: fa-8
status: open
---
# Task with dashes

Here is some content.

---

More content after horizontal rule.
`,
			want: &issue.Issue{
				ID:          "fa-8",
				Title:       "Task with dashes",
				Description: "Here is some content.\n\n---\n\nMore content after horizontal rule.",
				Status:      issue.StatusOpen,
			},
		},
		{
			name: "empty frontmatter",
			input: `---
---
# No metadata
`,
			want: &issue.Issue{
				Title: "No metadata",
			},
		},
		{
			name: "leading whitespace in body",
			input: `---
id: fa-9
status: open
---


# Whitespace before title

Description.
`,
			want: &issue.Issue{
				ID:          "fa-9",
				Title:       "Whitespace before title",
				Description: "Description.",
				Status:      issue.StatusOpen,
			},
		},
		{
			name: "no body at all",
			input: `---
id: fa-10
status: open
---
`,
			want: &issue.Issue{
				ID:     "fa-10",
				Status: issue.StatusOpen,
			},
		},
		{
			name: "only whitespace in body",
			input: `---
id: fa-11
status: open
---



`,
			want: &issue.Issue{
				ID:     "fa-11",
				Status: issue.StatusOpen,
			},
		},
		{
			name: "missing title heading",
			input: `---
id: fa-12
status: open
---
No heading here, just text.
`,
			want: &issue.Issue{
				ID:          "fa-12",
				Description: "No heading here, just text.",
				Status:      issue.StatusOpen,
			},
		},
		{
			name: "all priority values",
			input: `---
id: fa-13
status: open
priority: 0
---
# Low priority
`,
			want: &issue.Issue{
				ID:       "fa-13",
				Title:    "Low priority",
				Status:   issue.StatusOpen,
				Priority: 0,
			},
		},
		{
			name: "high priority",
			input: `---
id: fa-14
status: open
priority: 2
---
# High priority
`,
			want: &issue.Issue{
				ID:       "fa-14",
				Title:    "High priority",
				Status:   issue.StatusOpen,
				Priority: 2,
			},
		},
		{
			name: "empty arrays",
			input: `---
id: fa-15
status: open
deps: []
labels: []
links: []
---
# Empty arrays
`,
			want: &issue.Issue{
				ID:           "fa-15",
				Title:        "Empty arrays",
				Status:       issue.StatusOpen,
				Dependencies: []string{},
				Labels:       []string{},
				Links:        []string{},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIssue([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("parseIssue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if got.ID != tt.want.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.want.ID)
			}
			if got.Title != tt.want.Title {
				t.Errorf("Title = %q, want %q", got.Title, tt.want.Title)
			}
			if got.Description != tt.want.Description {
				t.Errorf("Description = %q, want %q", got.Description, tt.want.Description)
			}
			if got.Status != tt.want.Status {
				t.Errorf("Status = %q, want %q", got.Status, tt.want.Status)
			}
			if got.Priority != tt.want.Priority {
				t.Errorf("Priority = %d, want %d", got.Priority, tt.want.Priority)
			}
			if got.Type != tt.want.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.want.Type)
			}
			if !slices.Equal(got.Dependencies, tt.want.Dependencies) {
				t.Errorf("Dependencies = %v, want %v", got.Dependencies, tt.want.Dependencies)
			}
			if !slices.Equal(got.Labels, tt.want.Labels) {
				t.Errorf("Labels = %v, want %v", got.Labels, tt.want.Labels)
			}
			if !slices.Equal(got.Links, tt.want.Links) {
				t.Errorf("Links = %v, want %v", got.Links, tt.want.Links)
			}
			if !tt.want.Created.IsZero() && !got.Created.Equal(tt.want.Created) {
				t.Errorf("Created = %v, want %v", got.Created, tt.want.Created)
			}
		})
	}
}

func TestSplitFrontmatter(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantFM   string
		wantBody string
		wantErr  bool
	}{
		{
			name:     "standard frontmatter",
			input:    "---\nkey: value\n---\nbody content",
			wantFM:   "key: value",
			wantBody: "body content",
		},
		{
			name:     "empty frontmatter",
			input:    "---\n---\nbody",
			wantFM:   "",
			wantBody: "body",
		},
		{
			name:     "multiline frontmatter",
			input:    "---\nkey1: value1\nkey2: value2\n---\nbody",
			wantFM:   "key1: value1\nkey2: value2",
			wantBody: "body",
		},
		{
			name:     "multiline body",
			input:    "---\nkey: value\n---\nline1\nline2\nline3",
			wantFM:   "key: value",
			wantBody: "line1\nline2\nline3",
		},
		{
			name:     "empty body",
			input:    "---\nkey: value\n---\n",
			wantFM:   "key: value",
			wantBody: "",
		},
		{
			name:     "no body after delimiter",
			input:    "---\nkey: value\n---",
			wantFM:   "key: value",
			wantBody: "",
		},
		{
			name:     "frontmatter with spaces around delimiters",
			input:    "  ---  \nkey: value\n  ---  \nbody",
			wantFM:   "key: value",
			wantBody: "body",
		},
		{
			name:     "dashes in body",
			input:    "---\nkey: value\n---\nbody\n---\nmore body",
			wantFM:   "key: value",
			wantBody: "body\n---\nmore body",
		},
		{
			name:    "missing opening delimiter",
			input:   "key: value\n---\nbody",
			wantErr: true,
		},
		{
			name:    "missing closing delimiter",
			input:   "---\nkey: value\nbody",
			wantErr: true,
		},
		{
			name:    "empty file",
			input:   "",
			wantErr: true,
		},
		{
			name:    "only opening delimiter",
			input:   "---\n",
			wantErr: true,
		},
		{
			name:    "non-delimiter first line",
			input:   "not-dashes\n---\n---",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fm, body, err := splitFrontmatter([]byte(tt.input))
			if (err != nil) != tt.wantErr {
				t.Errorf("splitFrontmatter() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if string(fm) != tt.wantFM {
				t.Errorf("frontmatter = %q, want %q", string(fm), tt.wantFM)
			}
			if string(body) != tt.wantBody {
				t.Errorf("body = %q, want %q", string(body), tt.wantBody)
			}
		})
	}
}

func TestParseBody(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantTitle       string
		wantDescription string
	}{
		{
			name:            "standard title and description",
			input:           "# Title\n\nDescription text",
			wantTitle:       "Title",
			wantDescription: "Description text",
		},
		{
			name:            "title only",
			input:           "# Just a title",
			wantTitle:       "Just a title",
			wantDescription: "",
		},
		{
			name:            "leading empty lines",
			input:           "\n\n\n# Title\n\nDescription",
			wantTitle:       "Title",
			wantDescription: "Description",
		},
		{
			name:            "leading whitespace lines",
			input:           "   \n\t\n  \n# Title\nDescription",
			wantTitle:       "Title",
			wantDescription: "Description",
		},
		{
			name:            "no title heading",
			input:           "Just some text without heading",
			wantTitle:       "",
			wantDescription: "Just some text without heading",
		},
		{
			name:            "empty input",
			input:           "",
			wantTitle:       "",
			wantDescription: "",
		},
		{
			name:            "only whitespace",
			input:           "   \n\t\n   ",
			wantTitle:       "",
			wantDescription: "",
		},
		{
			name:            "multiline description",
			input:           "# Title\n\nParagraph 1\n\nParagraph 2",
			wantTitle:       "Title",
			wantDescription: "Paragraph 1\n\nParagraph 2",
		},
		{
			name:            "description with trailing newlines",
			input:           "# Title\n\nDescription\n\n\n",
			wantTitle:       "Title",
			wantDescription: "Description",
		},
		{
			name:            "description with leading newlines",
			input:           "# Title\n\n\n\nDescription",
			wantTitle:       "Title",
			wantDescription: "Description",
		},
		{
			name:            "title with special characters",
			input:           "# Fix: bug in foo() [critical]\n\nDetails",
			wantTitle:       "Fix: bug in foo() [critical]",
			wantDescription: "Details",
		},
		{
			name:            "title with unicode",
			input:           "# 日本語タイトル\n\n説明文",
			wantTitle:       "日本語タイトル",
			wantDescription: "説明文",
		},
		{
			name:            "hash not at start of line",
			input:           "Text with # hash\nmore text",
			wantTitle:       "",
			wantDescription: "Text with # hash\nmore text",
		},
		{
			name:            "multiple hash symbols",
			input:           "# Title\n\n## Subtitle\n\nContent",
			wantTitle:       "Title",
			wantDescription: "## Subtitle\n\nContent",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title, description := parseBody([]byte(tt.input))
			if title != tt.wantTitle {
				t.Errorf("title = %q, want %q", title, tt.wantTitle)
			}
			if description != tt.wantDescription {
				t.Errorf("description = %q, want %q", description, tt.wantDescription)
			}
		})
	}
}

func TestFormatIssue(t *testing.T) {
	created := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		issue   *issue.Issue
		wantErr bool
	}{
		{
			name: "full issue",
			issue: &issue.Issue{
				ID:           "fa-1",
				Title:        "Test issue",
				Description:  "Full description here.",
				Status:       issue.StatusOpen,
				Type:         "feature",
				Priority:     2,
				Dependencies: []string{"fa-0"},
				Labels:       []string{"backend"},
				Links:        []string{"https://example.com"},
				Created:      created,
			},
		},
		{
			name: "minimal issue",
			issue: &issue.Issue{
				ID:     "fa-2",
				Title:  "Minimal",
				Status: issue.StatusClosed,
			},
		},
		{
			name: "issue with nil slices",
			issue: &issue.Issue{
				ID:           "fa-3",
				Title:        "Nil slices",
				Status:       issue.StatusOpen,
				Dependencies: nil,
				Labels:       nil,
				Links:        nil,
			},
		},
		{
			name: "issue with empty slices",
			issue: &issue.Issue{
				ID:           "fa-4",
				Title:        "Empty slices",
				Status:       issue.StatusOpen,
				Dependencies: []string{},
				Labels:       []string{},
				Links:        []string{},
			},
		},
		{
			name: "issue without description",
			issue: &issue.Issue{
				ID:     "fa-5",
				Title:  "No description",
				Status: issue.StatusOpen,
			},
		},
		{
			name: "issue with unicode",
			issue: &issue.Issue{
				ID:          "fa-6",
				Title:       "日本語タイトル",
				Description: "Unicode content 🎉",
				Status:      issue.StatusOpen,
			},
		},
		{
			name: "issue with multiline description",
			issue: &issue.Issue{
				ID:          "fa-7",
				Title:       "Multiline",
				Description: "Line 1\n\nLine 2\n\nLine 3",
				Status:      issue.StatusOpen,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data, err := formatIssue(tt.issue)
			if (err != nil) != tt.wantErr {
				t.Errorf("formatIssue() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}
			if len(data) == 0 {
				t.Error("formatIssue() returned empty data")
			}
		})
	}
}

func TestFormatParseRoundtrip(t *testing.T) {
	created := time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC)

	tests := []struct {
		name  string
		issue *issue.Issue
	}{
		{
			name: "full issue roundtrip",
			issue: &issue.Issue{
				ID:           "fa-1",
				Title:        "Test roundtrip",
				Description:  "This should survive the roundtrip.",
				Status:       issue.StatusOpen,
				Type:         "feature",
				Priority:     2,
				Dependencies: []string{"fa-0", "fa-1"},
				Labels:       []string{"backend", "api"},
				Links:        []string{"https://example.com"},
				Created:      created,
			},
		},
		{
			name: "minimal issue roundtrip",
			issue: &issue.Issue{
				ID:     "fa-2",
				Title:  "Minimal",
				Status: issue.StatusClosed,
			},
		},
		{
			name: "issue with empty description",
			issue: &issue.Issue{
				ID:     "fa-3",
				Title:  "No description",
				Status: issue.StatusOpen,
			},
		},
		{
			name: "unicode roundtrip",
			issue: &issue.Issue{
				ID:          "fa-4",
				Title:       "日本語のタイトル",
				Description: "Émojis 🚌 and ünïcödé characters.",
				Status:      issue.StatusOpen,
				Labels:      []string{"日本語"},
			},
		},
		{
			name: "multiline description roundtrip",
			issue: &issue.Issue{
				ID:          "fa-5",
				Title:       "Multiline",
				Description: "First paragraph.\n\nSecond paragraph.\n\n- Item 1\n- Item 2",
				Status:      issue.StatusBlocked,
			},
		},
		{
			name: "all status types",
			issue: &issue.Issue{
				ID:     "fa-6",
				Title:  "Blocked status",
				Status: issue.StatusBlocked,
			},
		},
		{
			name: "special characters in title",
			issue: &issue.Issue{
				ID:     "fa-7",
				Title:  "Fix: foo() -> bar() [urgent]",
				Status: issue.StatusOpen,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Format the issue
			data, err := formatIssue(tt.issue)
			if err != nil {
				t.Fatalf("formatIssue() error = %v", err)
			}

			// Parse it back
			got, err := parseIssue(data)
			if err != nil {
				t.Fatalf("parseIssue() error = %v", err)
			}

			// Compare fields
			if got.ID != tt.issue.ID {
				t.Errorf("ID = %q, want %q", got.ID, tt.issue.ID)
			}
			if got.Title != tt.issue.Title {
				t.Errorf("Title = %q, want %q", got.Title, tt.issue.Title)
			}
			if got.Description != tt.issue.Description {
				t.Errorf("Description = %q, want %q", got.Description, tt.issue.Description)
			}
			if got.Status != tt.issue.Status {
				t.Errorf("Status = %q, want %q", got.Status, tt.issue.Status)
			}
			if got.Priority != tt.issue.Priority {
				t.Errorf("Priority = %d, want %d", got.Priority, tt.issue.Priority)
			}
			if got.Type != tt.issue.Type {
				t.Errorf("Type = %q, want %q", got.Type, tt.issue.Type)
			}
			// For roundtrip, nil becomes empty slice in formatIssue
			wantDeps := tt.issue.Dependencies
			if wantDeps == nil {
				wantDeps = []string{}
			}
			if !slices.Equal(got.Dependencies, wantDeps) {
				t.Errorf("Dependencies = %v, want %v", got.Dependencies, wantDeps)
			}
			// Labels are omitempty, so nil stays nil
			if !slices.Equal(got.Labels, tt.issue.Labels) {
				t.Errorf("Labels = %v, want %v", got.Labels, tt.issue.Labels)
			}
			wantLinks := tt.issue.Links
			if wantLinks == nil {
				wantLinks = []string{}
			}
			if !slices.Equal(got.Links, wantLinks) {
				t.Errorf("Links = %v, want %v", got.Links, wantLinks)
			}
			if !tt.issue.Created.IsZero() && !got.Created.Equal(tt.issue.Created) {
				t.Errorf("Created = %v, want %v", got.Created, tt.issue.Created)
			}
		})
	}
}
