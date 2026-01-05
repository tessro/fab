// Package usage provides parsing of Claude Code JSONL files for token usage tracking.
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// ClaudeDir is the default location of Claude Code data.
const ClaudeDir = ".claude"

// Usage represents aggregated token usage.
type Usage struct {
	InputTokens         int64     `json:"input_tokens"`
	OutputTokens        int64     `json:"output_tokens"`
	CacheCreationTokens int64     `json:"cache_creation_tokens"`
	CacheReadTokens     int64     `json:"cache_read_tokens"`
	TotalInputTokens    int64     `json:"total_input_tokens"` // InputTokens + CacheCreationTokens + CacheReadTokens
	TotalTokens         int64     `json:"total_tokens"`       // TotalInputTokens + OutputTokens
	MessageCount        int       `json:"message_count"`
	FirstMessageAt      time.Time `json:"first_message_at,omitempty"`
	LastMessageAt       time.Time `json:"last_message_at,omitempty"`
}

// Add combines usage from another Usage instance.
func (u *Usage) Add(other Usage) {
	u.InputTokens += other.InputTokens
	u.OutputTokens += other.OutputTokens
	u.CacheCreationTokens += other.CacheCreationTokens
	u.CacheReadTokens += other.CacheReadTokens
	u.TotalInputTokens += other.TotalInputTokens
	u.TotalTokens += other.TotalTokens
	u.MessageCount += other.MessageCount

	if other.FirstMessageAt.Before(u.FirstMessageAt) || u.FirstMessageAt.IsZero() {
		u.FirstMessageAt = other.FirstMessageAt
	}
	if other.LastMessageAt.After(u.LastMessageAt) {
		u.LastMessageAt = other.LastMessageAt
	}
}

// jsonlEntry represents a single line in a Claude Code JSONL file.
type jsonlEntry struct {
	Type      string   `json:"type"`
	Timestamp string   `json:"timestamp"`
	Message   *message `json:"message,omitempty"`
}

// message represents the message field in an assistant entry.
type message struct {
	Role  string      `json:"role"`
	Usage *tokenUsage `json:"usage,omitempty"`
}

// tokenUsage represents the usage field in a message.
type tokenUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
}

// ParseFile parses a single JSONL file and returns aggregated usage.
func ParseFile(path string) (Usage, error) {
	f, err := os.Open(path)
	if err != nil {
		return Usage{}, err
	}
	defer func() { _ = f.Close() }()

	var usage Usage
	scanner := bufio.NewScanner(f)
	// Increase buffer for large lines
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var entry jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			// Skip malformed lines
			continue
		}

		// Only process assistant messages with usage data
		if entry.Type != "assistant" || entry.Message == nil || entry.Message.Usage == nil {
			continue
		}

		u := entry.Message.Usage
		usage.InputTokens += u.InputTokens
		usage.OutputTokens += u.OutputTokens
		usage.CacheCreationTokens += u.CacheCreationInputTokens
		usage.CacheReadTokens += u.CacheReadInputTokens
		usage.MessageCount++

		// Parse timestamp
		if ts, err := time.Parse(time.RFC3339, entry.Timestamp); err == nil {
			if usage.FirstMessageAt.IsZero() || ts.Before(usage.FirstMessageAt) {
				usage.FirstMessageAt = ts
			}
			if ts.After(usage.LastMessageAt) {
				usage.LastMessageAt = ts
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return usage, err
	}

	// Calculate totals
	usage.TotalInputTokens = usage.InputTokens + usage.CacheCreationTokens + usage.CacheReadTokens
	usage.TotalTokens = usage.TotalInputTokens + usage.OutputTokens

	return usage, nil
}

// FindSessionFiles finds all JSONL session files for a project.
// projectPath should be the absolute path to the project directory.
func FindSessionFiles(projectPath string) ([]string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	// Claude Code stores projects with path-based names
	// e.g., /home/tess/repos/fab -> -home-tess-repos-fab
	claudeProjectName := pathToClaudeName(projectPath)
	projectDir := filepath.Join(home, ClaudeDir, "projects", claudeProjectName)

	// Find all JSONL files in the project directory
	pattern := filepath.Join(projectDir, "*.jsonl")
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return nil, err
	}

	return matches, nil
}

// pathToClaudeName converts an absolute path to Claude's project name format.
// /home/tess/repos/fab -> -home-tess-repos-fab
func pathToClaudeName(path string) string {
	// Replace path separators with dashes, remove leading slash
	result := make([]byte, 0, len(path))
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			result = append(result, '-')
		} else {
			result = append(result, path[i])
		}
	}
	return string(result)
}

// ParseProject parses all JSONL files for a project and returns aggregated usage.
func ParseProject(projectPath string) (Usage, error) {
	files, err := FindSessionFiles(projectPath)
	if err != nil {
		return Usage{}, err
	}

	var total Usage
	for _, file := range files {
		usage, err := ParseFile(file)
		if err != nil {
			// Log error but continue with other files
			continue
		}
		total.Add(usage)
	}

	return total, nil
}

// ParseAllProjects parses JSONL files for all Claude Code projects.
func ParseAllProjects() (map[string]Usage, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}

	projectsDir := filepath.Join(home, ClaudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]Usage), nil
		}
		return nil, err
	}

	results := make(map[string]Usage)
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectDir := filepath.Join(projectsDir, entry.Name())
		pattern := filepath.Join(projectDir, "*.jsonl")
		files, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}

		var total Usage
		for _, file := range files {
			usage, err := ParseFile(file)
			if err != nil {
				continue
			}
			total.Add(usage)
		}

		if total.MessageCount > 0 {
			results[entry.Name()] = total
		}
	}

	return results, nil
}

// BillingWindow represents a 5-hour billing window for Claude Pro/Max.
type BillingWindow struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
	Usage Usage     `json:"usage"`
}

// Limits represents usage caps for a billing window.
// Claude Pro/Max has rolling 5-hour windows with output token limits.
type Limits struct {
	OutputTokens int64 `json:"output_tokens"`
}

// DefaultProLimits returns conservative defaults for Claude Pro plan.
// Actual limits vary by model and subscription; configure as needed.
func DefaultProLimits() Limits {
	return Limits{
		OutputTokens: 500_000, // ~500K output tokens per 5-hour window
	}
}

// DefaultMaxLimits returns defaults for Claude Max plan.
func DefaultMaxLimits() Limits {
	return Limits{
		OutputTokens: 2_000_000, // ~2M output tokens per 5-hour window
	}
}

// Percent calculates usage as a percentage of limits (0.0 to 1.0+).
// Returns ratio of output tokens used vs limit.
func (u Usage) Percent(limits Limits) float64 {
	if limits.OutputTokens == 0 {
		return 0
	}
	return float64(u.OutputTokens) / float64(limits.OutputTokens)
}

// PercentInt returns usage percentage as an integer (0-100+).
func (u Usage) PercentInt(limits Limits) int {
	return int(u.Percent(limits) * 100)
}

// GetCurrentBillingWindow returns the current 5-hour billing window.
// Windows start at midnight UTC and repeat every 5 hours.
func GetCurrentBillingWindow() BillingWindow {
	now := time.Now().UTC()

	// Calculate hours since midnight
	hoursSinceMidnight := now.Hour()
	windowIndex := hoursSinceMidnight / 5
	windowStartHour := windowIndex * 5

	start := time.Date(now.Year(), now.Month(), now.Day(), windowStartHour, 0, 0, 0, time.UTC)
	end := start.Add(5 * time.Hour)

	return BillingWindow{
		Start: start,
		End:   end,
	}
}

// ParseProjectInWindow parses usage for a project within a specific time window.
func ParseProjectInWindow(projectPath string, window BillingWindow) (Usage, error) {
	files, err := FindSessionFiles(projectPath)
	if err != nil {
		return Usage{}, err
	}

	var total Usage
	for _, file := range files {
		usage, err := parseFileInWindow(file, window)
		if err != nil {
			continue
		}
		total.Add(usage)
	}

	return total, nil
}

// parseFileInWindow parses a file but only counts messages within the window.
func parseFileInWindow(path string, window BillingWindow) (Usage, error) {
	f, err := os.Open(path)
	if err != nil {
		return Usage{}, err
	}
	defer func() { _ = f.Close() }()

	var usage Usage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)

	for scanner.Scan() {
		var entry jsonlEntry
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		if entry.Type != "assistant" || entry.Message == nil || entry.Message.Usage == nil {
			continue
		}

		// Parse timestamp and check if within window
		ts, err := time.Parse(time.RFC3339, entry.Timestamp)
		if err != nil {
			continue
		}

		if ts.Before(window.Start) || !ts.Before(window.End) {
			continue
		}

		u := entry.Message.Usage
		usage.InputTokens += u.InputTokens
		usage.OutputTokens += u.OutputTokens
		usage.CacheCreationTokens += u.CacheCreationInputTokens
		usage.CacheReadTokens += u.CacheReadInputTokens
		usage.MessageCount++

		if usage.FirstMessageAt.IsZero() || ts.Before(usage.FirstMessageAt) {
			usage.FirstMessageAt = ts
		}
		if ts.After(usage.LastMessageAt) {
			usage.LastMessageAt = ts
		}
	}

	if err := scanner.Err(); err != nil {
		return usage, err
	}

	usage.TotalInputTokens = usage.InputTokens + usage.CacheCreationTokens + usage.CacheReadTokens
	usage.TotalTokens = usage.TotalInputTokens + usage.OutputTokens

	return usage, nil
}

// ParseAllProjectsInWindow aggregates usage across all Claude Code projects within a billing window.
func ParseAllProjectsInWindow(window BillingWindow) (Usage, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Usage{}, err
	}

	projectsDir := filepath.Join(home, ClaudeDir, "projects")
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return Usage{}, nil
		}
		return Usage{}, err
	}

	var total Usage
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		projectDir := filepath.Join(projectsDir, entry.Name())
		pattern := filepath.Join(projectDir, "*.jsonl")
		files, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}

		for _, file := range files {
			usage, err := parseFileInWindow(file, window)
			if err != nil {
				continue
			}
			total.Add(usage)
		}
	}

	return total, nil
}

// GetCurrentBillingWindowWithUsage returns the current billing window with aggregated usage.
func GetCurrentBillingWindowWithUsage() (BillingWindow, error) {
	window := GetCurrentBillingWindow()
	usage, err := ParseAllProjectsInWindow(window)
	if err != nil {
		return window, err
	}
	window.Usage = usage
	return window, nil
}

// GetDayBillingWindows returns all 5 billing windows for a given day.
func GetDayBillingWindows(date time.Time) []BillingWindow {
	date = date.UTC()
	midnight := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, time.UTC)

	windows := make([]BillingWindow, 5)
	for i := 0; i < 5; i++ {
		start := midnight.Add(time.Duration(i*5) * time.Hour)
		windows[i] = BillingWindow{
			Start: start,
			End:   start.Add(5 * time.Hour),
		}
	}
	return windows
}

// TimeRemaining returns the duration remaining in this billing window.
func (bw BillingWindow) TimeRemaining() time.Duration {
	remaining := bw.End.Sub(time.Now().UTC())
	if remaining < 0 {
		return 0
	}
	return remaining
}
