package gh

import (
	"testing"

	"github.com/tessro/fab/internal/issue"
)

// TestBackendImplementsInterface verifies that Backend implements issue.Backend.
func TestBackendImplementsInterface(t *testing.T) {
	var _ issue.Backend = (*Backend)(nil)
}

func TestParseNWO(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    string
		wantErr bool
	}{
		{
			name: "ssh format",
			url:  "git@github.com:owner/repo.git",
			want: "owner/repo",
		},
		{
			name: "https format with .git",
			url:  "https://github.com/owner/repo.git",
			want: "owner/repo",
		},
		{
			name: "https format without .git",
			url:  "https://github.com/owner/repo",
			want: "owner/repo",
		},
		{
			name:    "non-github url",
			url:     "git@gitlab.com:owner/repo.git",
			wantErr: true,
		},
		{
			name:    "invalid url",
			url:     "not-a-url",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseNWO(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseNWO() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseNWO() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseIssueNumberFromURL(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		want    int
		wantErr bool
	}{
		{
			name: "valid issue url",
			url:  "https://github.com/owner/repo/issues/123",
			want: 123,
		},
		{
			name: "issue number 1",
			url:  "https://github.com/owner/repo/issues/1",
			want: 1,
		},
		{
			name:    "invalid url",
			url:     "https://github.com/owner/repo/pulls/123",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseIssueNumberFromURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseIssueNumberFromURL() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseIssueNumberFromURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestOwnerFromNWO(t *testing.T) {
	tests := []struct {
		name string
		nwo  string
		want string
	}{
		{
			name: "standard format",
			nwo:  "owner/repo",
			want: "owner",
		},
		{
			name: "org with hyphen",
			nwo:  "my-org/my-repo",
			want: "my-org",
		},
		{
			name: "empty string",
			nwo:  "",
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ownerFromNWO(tt.nwo)
			if got != tt.want {
				t.Errorf("ownerFromNWO() = %v, want %v", got, tt.want)
			}
		})
	}
}
