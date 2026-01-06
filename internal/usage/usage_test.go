package usage

import (
	"testing"
	"time"
)

func TestGetCurrentBillingWindow(t *testing.T) {
	window := GetCurrentBillingWindow()

	// Window should be 5 hours
	if window.End.Sub(window.Start) != 5*time.Hour {
		t.Errorf("expected 5-hour window, got %v", window.End.Sub(window.Start))
	}

	// Start should be floored to the hour (minutes/seconds/nanos are zero)
	if window.Start.Minute() != 0 || window.Start.Second() != 0 || window.Start.Nanosecond() != 0 {
		t.Errorf("window start should be floored to hour, got %v", window.Start)
	}
}

func TestFloorToHour(t *testing.T) {
	tests := []struct {
		input    time.Time
		expected time.Time
	}{
		{
			time.Date(2026, 1, 3, 14, 30, 45, 123, time.UTC),
			time.Date(2026, 1, 3, 14, 0, 0, 0, time.UTC),
		},
		{
			time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
			time.Date(2026, 1, 3, 0, 0, 0, 0, time.UTC),
		},
		{
			time.Date(2026, 1, 3, 23, 59, 59, 999999999, time.UTC),
			time.Date(2026, 1, 3, 23, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.input.String(), func(t *testing.T) {
			got := floorToHour(tt.input)
			if !got.Equal(tt.expected) {
				t.Errorf("floorToHour(%v) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSortTimestamps(t *testing.T) {
	timestamps := []time.Time{
		time.Date(2026, 1, 3, 14, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 3, 16, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC),
	}

	sortTimestamps(timestamps)

	// Check they're in chronological order
	for i := 1; i < len(timestamps); i++ {
		if timestamps[i].Before(timestamps[i-1]) {
			t.Errorf("timestamps not sorted: %v before %v", timestamps[i], timestamps[i-1])
		}
	}

	// Check specific values
	if timestamps[0].Hour() != 10 {
		t.Errorf("first timestamp should be hour 10, got %d", timestamps[0].Hour())
	}
	if timestamps[3].Hour() != 16 {
		t.Errorf("last timestamp should be hour 16, got %d", timestamps[3].Hour())
	}
}

func TestUsagePercent(t *testing.T) {
	tests := []struct {
		name         string
		usage        Usage
		limits       Limits
		wantPercent  float64
		wantPercentI int
	}{
		{
			name:         "zero usage",
			usage:        Usage{OutputTokens: 0},
			limits:       Limits{OutputTokens: 500_000},
			wantPercent:  0,
			wantPercentI: 0,
		},
		{
			name:         "50% usage",
			usage:        Usage{OutputTokens: 250_000},
			limits:       Limits{OutputTokens: 500_000},
			wantPercent:  0.5,
			wantPercentI: 50,
		},
		{
			name:         "100% usage",
			usage:        Usage{OutputTokens: 500_000},
			limits:       Limits{OutputTokens: 500_000},
			wantPercent:  1.0,
			wantPercentI: 100,
		},
		{
			name:         "over limit",
			usage:        Usage{OutputTokens: 750_000},
			limits:       Limits{OutputTokens: 500_000},
			wantPercent:  1.5,
			wantPercentI: 150,
		},
		{
			name:         "zero limit returns zero",
			usage:        Usage{OutputTokens: 100_000},
			limits:       Limits{OutputTokens: 0},
			wantPercent:  0,
			wantPercentI: 0,
		},
		{
			name:         "67% usage",
			usage:        Usage{OutputTokens: 335_000},
			limits:       Limits{OutputTokens: 500_000},
			wantPercent:  0.67,
			wantPercentI: 67,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.usage.Percent(tt.limits)
			if got != tt.wantPercent {
				t.Errorf("Percent() = %v, want %v", got, tt.wantPercent)
			}

			gotI := tt.usage.PercentInt(tt.limits)
			if gotI != tt.wantPercentI {
				t.Errorf("PercentInt() = %v, want %v", gotI, tt.wantPercentI)
			}
		})
	}
}

func TestDefaultLimits(t *testing.T) {
	pro := DefaultProLimits()
	if pro.OutputTokens != 500_000 {
		t.Errorf("Pro limits: expected 500000, got %d", pro.OutputTokens)
	}

	max := DefaultMaxLimits()
	if max.OutputTokens != 2_000_000 {
		t.Errorf("Max limits: expected 2000000, got %d", max.OutputTokens)
	}

	// Max should be greater than Pro
	if max.OutputTokens <= pro.OutputTokens {
		t.Error("Max limits should be greater than Pro limits")
	}
}

func TestBillingWindowTimeRemaining(t *testing.T) {
	now := time.Now().UTC()

	// Window that ends in the future
	futureWindow := BillingWindow{
		Start: now.Add(-1 * time.Hour),
		End:   now.Add(4 * time.Hour),
	}
	remaining := futureWindow.TimeRemaining()
	if remaining < 3*time.Hour || remaining > 4*time.Hour {
		t.Errorf("expected ~4 hours remaining, got %v", remaining)
	}

	// Window that has ended
	pastWindow := BillingWindow{
		Start: now.Add(-6 * time.Hour),
		End:   now.Add(-1 * time.Hour),
	}
	remaining = pastWindow.TimeRemaining()
	if remaining != 0 {
		t.Errorf("expected 0 remaining for past window, got %v", remaining)
	}
}

func TestUsageAdd(t *testing.T) {
	u1 := Usage{
		InputTokens:         100,
		OutputTokens:        200,
		CacheCreationTokens: 50,
		CacheReadTokens:     25,
		MessageCount:        3,
		FirstMessageAt:      time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC),
		LastMessageAt:       time.Date(2026, 1, 3, 11, 0, 0, 0, time.UTC),
	}

	u2 := Usage{
		InputTokens:         150,
		OutputTokens:        300,
		CacheCreationTokens: 75,
		CacheReadTokens:     30,
		MessageCount:        5,
		FirstMessageAt:      time.Date(2026, 1, 3, 9, 0, 0, 0, time.UTC),
		LastMessageAt:       time.Date(2026, 1, 3, 12, 0, 0, 0, time.UTC),
	}

	u1.Add(u2)

	if u1.InputTokens != 250 {
		t.Errorf("InputTokens: expected 250, got %d", u1.InputTokens)
	}
	if u1.OutputTokens != 500 {
		t.Errorf("OutputTokens: expected 500, got %d", u1.OutputTokens)
	}
	if u1.CacheCreationTokens != 125 {
		t.Errorf("CacheCreationTokens: expected 125, got %d", u1.CacheCreationTokens)
	}
	if u1.CacheReadTokens != 55 {
		t.Errorf("CacheReadTokens: expected 55, got %d", u1.CacheReadTokens)
	}
	if u1.MessageCount != 8 {
		t.Errorf("MessageCount: expected 8, got %d", u1.MessageCount)
	}
	// FirstMessageAt should be earlier
	if u1.FirstMessageAt.Hour() != 9 {
		t.Errorf("FirstMessageAt: expected hour 9, got %d", u1.FirstMessageAt.Hour())
	}
	// LastMessageAt should be later
	if u1.LastMessageAt.Hour() != 12 {
		t.Errorf("LastMessageAt: expected hour 12, got %d", u1.LastMessageAt.Hour())
	}
}

func TestPathToClaudeName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/home/tess/repos/fab", "-home-tess-repos-fab"},
		{"/Users/john/projects/app", "-Users-john-projects-app"},
		{"/tmp/test", "-tmp-test"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := pathToClaudeName(tt.path)
			if got != tt.want {
				t.Errorf("pathToClaudeName(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}
