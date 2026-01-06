// Package config provides configuration validation for fab.
package config

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Validation errors.
var (
	ErrEmptyProjectName    = errors.New("project name cannot be empty")
	ErrInvalidProjectName  = errors.New("project name contains invalid characters")
	ErrProjectNameTooLong  = errors.New("project name exceeds maximum length")
	ErrEmptyRemoteURL      = errors.New("remote URL cannot be empty")
	ErrInvalidRemoteURL    = errors.New("remote URL is not a valid git URL")
	ErrInvalidMaxAgents    = errors.New("max_agents must be between 1 and 100")
	ErrEmptyToolName       = errors.New("tool name cannot be empty")
	ErrInvalidToolName     = errors.New("unknown tool name")
	ErrEmptyAction         = errors.New("action cannot be empty")
	ErrInvalidAction       = errors.New("action must be 'allow', 'deny', or 'pass'")
	ErrEmptyPattern        = errors.New("pattern cannot be empty when specified")
	ErrEmptyPatternElement = errors.New("patterns array contains empty element")
	ErrScriptNotExecutable = errors.New("script is not executable")
)

// Maximum project name length.
const MaxProjectNameLength = 64

// Maximum max_agents value.
const MaxMaxAgents = 100

// validProjectNameRegex matches valid project names:
// alphanumeric, dash, underscore, dot, no path separators.
var validProjectNameRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*$`)

// gitHTTPSRegex matches HTTPS git URLs.
var gitHTTPSRegex = regexp.MustCompile(`^https?://[^/]+/[^/]+/.+`)

// gitSSHRegex matches SSH git URLs.
var gitSSHRegex = regexp.MustCompile(`^git@[^:]+:.+/.+`)

// gitFileRegex matches file:// git URLs (used for local testing).
var gitFileRegex = regexp.MustCompile(`^file://.+`)

// knownTools is the list of valid tool names.
// See: https://docs.anthropic.com/en/docs/claude-code/settings#tool-permissions
var knownTools = map[string]bool{
	"AskUserQuestion": true,
	"Bash":            true,
	"Edit":            true,
	"EnterPlanMode":   true,
	"ExitPlanMode":    true,
	"Glob":            true,
	"Grep":            true,
	"KillShell":       true,
	"NotebookEdit":    true,
	"Read":            true,
	"Skill":           true,
	"Task":            true,
	"TaskOutput":      true,
	"TodoWrite":       true,
	"WebFetch":        true,
	"WebSearch":       true,
	"Write":           true,
}

// validActions is the list of valid action values.
var validActions = map[string]bool{
	"allow": true,
	"deny":  true,
	"pass":  true,
}

// ValidationError wraps a validation error with context.
type ValidationError struct {
	Field   string
	Value   string
	Message string
	Err     error
}

func (e *ValidationError) Error() string {
	if e.Value != "" {
		return fmt.Sprintf("%s: %s (got %q)", e.Field, e.Message, e.Value)
	}
	return fmt.Sprintf("%s: %s", e.Field, e.Message)
}

func (e *ValidationError) Unwrap() error {
	return e.Err
}

// ValidateProjectName validates a project name.
func ValidateProjectName(name string) error {
	if name == "" {
		return &ValidationError{
			Field:   "name",
			Message: "cannot be empty",
			Err:     ErrEmptyProjectName,
		}
	}

	if len(name) > MaxProjectNameLength {
		return &ValidationError{
			Field:   "name",
			Value:   name,
			Message: fmt.Sprintf("exceeds maximum length of %d characters", MaxProjectNameLength),
			Err:     ErrProjectNameTooLong,
		}
	}

	if !validProjectNameRegex.MatchString(name) {
		return &ValidationError{
			Field:   "name",
			Value:   name,
			Message: "must start with alphanumeric and contain only alphanumeric, dash, underscore, or dot",
			Err:     ErrInvalidProjectName,
		}
	}

	return nil
}

// ValidateRemoteURL validates a git remote URL.
func ValidateRemoteURL(url string) error {
	if url == "" {
		return &ValidationError{
			Field:   "remote_url",
			Message: "cannot be empty",
			Err:     ErrEmptyRemoteURL,
		}
	}

	url = strings.TrimSpace(url)

	// Check for HTTPS URL
	if gitHTTPSRegex.MatchString(url) {
		return nil
	}

	// Check for SSH URL
	if gitSSHRegex.MatchString(url) {
		return nil
	}

	// Check for file:// URL (used for local testing)
	if gitFileRegex.MatchString(url) {
		return nil
	}

	return &ValidationError{
		Field:   "remote_url",
		Value:   url,
		Message: "must be a valid git URL (https://, git@, or file://)",
		Err:     ErrInvalidRemoteURL,
	}
}

// ValidateMaxAgents validates the max_agents value.
func ValidateMaxAgents(maxAgents int) error {
	if maxAgents < 1 || maxAgents > MaxMaxAgents {
		return &ValidationError{
			Field:   "max_agents",
			Value:   fmt.Sprintf("%d", maxAgents),
			Message: fmt.Sprintf("must be between 1 and %d", MaxMaxAgents),
			Err:     ErrInvalidMaxAgents,
		}
	}
	return nil
}

// ValidateProjectEntry validates a complete project entry.
func ValidateProjectEntry(name, remoteURL string, maxAgents int) error {
	if err := ValidateProjectName(name); err != nil {
		return err
	}

	if err := ValidateRemoteURL(remoteURL); err != nil {
		return err
	}

	if err := ValidateMaxAgents(maxAgents); err != nil {
		return err
	}

	return nil
}

// ValidateToolName validates a tool name.
func ValidateToolName(tool string) error {
	if tool == "" {
		return &ValidationError{
			Field:   "tool",
			Message: "cannot be empty",
			Err:     ErrEmptyToolName,
		}
	}

	if !knownTools[tool] {
		return &ValidationError{
			Field:   "tool",
			Value:   tool,
			Message: "unknown tool name",
			Err:     ErrInvalidToolName,
		}
	}

	return nil
}

// ValidateAction validates a rule action.
func ValidateAction(action string) error {
	if action == "" {
		return &ValidationError{
			Field:   "action",
			Message: "cannot be empty",
			Err:     ErrEmptyAction,
		}
	}

	if !validActions[action] {
		return &ValidationError{
			Field:   "action",
			Value:   action,
			Message: "must be 'allow', 'deny', or 'pass'",
			Err:     ErrInvalidAction,
		}
	}

	return nil
}

// ValidatePattern validates a single pattern.
func ValidatePattern(pattern string) error {
	// Empty pattern is valid (matches all)
	if pattern == "" {
		return nil
	}

	// Patterns with only whitespace are invalid
	if strings.TrimSpace(pattern) == "" {
		return &ValidationError{
			Field:   "pattern",
			Message: "cannot be empty when specified",
			Err:     ErrEmptyPattern,
		}
	}

	return nil
}

// ValidatePatterns validates a patterns array.
func ValidatePatterns(patterns []string) error {
	for i, p := range patterns {
		if p == "" || strings.TrimSpace(p) == "" {
			return &ValidationError{
				Field:   fmt.Sprintf("patterns[%d]", i),
				Message: "cannot be empty",
				Err:     ErrEmptyPatternElement,
			}
		}
	}
	return nil
}

// ValidateRule validates a complete rule entry.
func ValidateRule(tool, action, pattern string, patterns []string, script string) error {
	if err := ValidateToolName(tool); err != nil {
		return err
	}

	if err := ValidateAction(action); err != nil {
		return err
	}

	if pattern != "" {
		if err := ValidatePattern(pattern); err != nil {
			return err
		}
	}

	if len(patterns) > 0 {
		if err := ValidatePatterns(patterns); err != nil {
			return err
		}
	}

	// Script validation is deferred to runtime since we can't check
	// file existence/permissions during config parsing

	return nil
}
