package rules

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	// DefaultScriptTimeout is the maximum time a script can run.
	DefaultScriptTimeout = 5 * time.Second
)

// RewritePattern rewrites path patterns based on prefix:
//   - "~" or "~/path" → "<home>" or "<home>/path" (home directory expansion)
//   - "/path" → "<cwd>/path" (worktree-scoped)
//   - "//path" → "/path" (absolute path, strip one /)
//   - other patterns pass through unchanged
//
// This allows rules like pattern = "/:*" to match files within the
// current working directory while pattern = "//:*" matches absolute paths.
// Pattern "~/:*" matches files within the user's home directory.
func RewritePattern(pattern, cwd string) string {
	if len(pattern) == 0 {
		return pattern
	}

	// Check for home directory expansion (~ → home)
	if strings.HasPrefix(pattern, "~") {
		home, err := os.UserHomeDir()
		if err != nil {
			return pattern // Return unchanged if we can't get home dir
		}
		if pattern == "~" {
			return home
		}
		if strings.HasPrefix(pattern, "~/") {
			return filepath.Join(home, pattern[2:])
		}
		// ~user syntax not supported, return unchanged
		return pattern
	}

	// Check for absolute path escape (// → /)
	if strings.HasPrefix(pattern, "//") {
		return pattern[1:] // Strip one leading /
	}

	// Check for worktree-scoped path (/ → cwd/)
	if strings.HasPrefix(pattern, "/") && cwd != "" {
		return cwd + pattern
	}

	return pattern
}

// MatchPattern checks if value matches the pattern.
// If pattern ends with ":*", it's a prefix match.
// Otherwise, it's an exact match.
// An empty pattern or ":*" alone matches everything.
func MatchPattern(pattern, value string) bool {
	// Empty pattern or wildcard matches everything
	if pattern == "" || pattern == ":*" {
		return true
	}

	// Check for prefix match (ends with :*)
	if strings.HasSuffix(pattern, ":*") {
		prefix := strings.TrimSuffix(pattern, ":*")
		return strings.HasPrefix(value, prefix)
	}

	// Exact match
	return pattern == value
}

// ResolvePrimaryField extracts the primary field value for matching based on tool type.
// Returns empty string if the field cannot be extracted.
func ResolvePrimaryField(toolName string, toolInput json.RawMessage) string {
	if len(toolInput) == 0 {
		return ""
	}

	var input map[string]any
	if err := json.Unmarshal(toolInput, &input); err != nil {
		return ""
	}

	switch toolName {
	case "Bash":
		if cmd, ok := input["command"].(string); ok {
			return cmd
		}
	case "Read", "Write", "Edit":
		if path, ok := input["file_path"].(string); ok {
			return path
		}
	case "Glob":
		if pattern, ok := input["pattern"].(string); ok {
			return pattern
		}
	case "Grep":
		if pattern, ok := input["pattern"].(string); ok {
			return pattern
		}
	case "WebFetch":
		if url, ok := input["url"].(string); ok {
			return url
		}
	case "Task":
		if prompt, ok := input["prompt"].(string); ok {
			return prompt
		}
	case "Skill":
		if skill, ok := input["skill"].(string); ok {
			return skill
		}
	case "WebSearch":
		if query, ok := input["query"].(string); ok {
			return query
		}
	case "NotebookEdit":
		if path, ok := input["notebook_path"].(string); ok {
			return path
		}
	}

	return ""
}

// ExpandHomePath expands ~ to the user's home directory in a path string.
// Returns the path unchanged if it doesn't start with ~ or if there's an error.
func ExpandHomePath(path string) string {
	if !strings.HasPrefix(path, "~") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(home, path[2:])
	}
	// ~user syntax not supported
	return path
}

// ScriptMatch executes a validation script and returns its decision.
// The script receives the tool name as its first argument and tool input JSON on stdin.
// Script output should be "allow", "deny", or "pass" (default on error or other output).
// The scriptPath supports ~ expansion (e.g., ~/scripts/check.sh).
func ScriptMatch(ctx context.Context, scriptPath, toolName string, toolInput json.RawMessage) (Action, error) {
	ctx, cancel := context.WithTimeout(ctx, DefaultScriptTimeout)
	defer cancel()

	// Expand ~ in script path
	expandedPath := ExpandHomePath(scriptPath)
	cmd := exec.CommandContext(ctx, expandedPath, toolName)
	cmd.Stdin = bytes.NewReader(toolInput)

	output, err := cmd.Output()
	if err != nil {
		// On error (including timeout), pass to next rule
		return ActionPass, err
	}

	// Parse output
	result := strings.TrimSpace(string(output))
	switch strings.ToLower(result) {
	case "allow":
		return ActionAllow, nil
	case "deny":
		return ActionDeny, nil
	default:
		return ActionPass, nil
	}
}
