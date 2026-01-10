package tui

import (
	"testing"
	"time"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		name     string
		duration time.Duration
		want     string
	}{
		{"zero", 0, "0s"},
		{"seconds", 45 * time.Second, "45s"},
		{"one minute", time.Minute, "1m"},
		{"minutes", 5 * time.Minute, "5m"},
		{"59 minutes", 59 * time.Minute, "59m"},
		{"one hour", time.Hour, "1h0m"},
		{"hours and minutes", 2*time.Hour + 30*time.Minute, "2h30m"},
		{"23 hours", 23*time.Hour + 59*time.Minute, "23h59m"},
		{"one day", 24 * time.Hour, "1d0h"},
		{"days and hours", 48*time.Hour + 12*time.Hour, "2d12h"},
		{"many days", 7*24*time.Hour + 5*time.Hour, "7d5h"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatDuration(tt.duration)
			if got != tt.want {
				t.Errorf("formatDuration(%v) = %q, want %q", tt.duration, got, tt.want)
			}
		})
	}
}

func TestFormatDurationMaxLength(t *testing.T) {
	// Verify that even very long durations produce concise output
	// This is important to prevent line wrapping in the agent list
	longDuration := 365 * 24 * time.Hour // 1 year
	result := formatDuration(longDuration)
	if len(result) > 7 { // "365d24h" is 7 chars, the max we'd expect
		t.Errorf("formatDuration for long duration produced unexpectedly long output: %q (len=%d)", result, len(result))
	}
}
