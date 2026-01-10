// Package rules provides permission rule evaluation for tool invocations.
package rules

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
	"github.com/tessro/fab/internal/config"
)

// Action is the result of a rule evaluation.
type Action string

const (
	// ActionAllow permits the tool invocation.
	ActionAllow Action = "allow"
	// ActionDeny blocks the tool invocation.
	ActionDeny Action = "deny"
	// ActionPass skips to the next rule.
	ActionPass Action = "pass"
)

// Rule defines a single permission rule.
type Rule struct {
	Tool     string   `toml:"tool"`               // Tool name to match (e.g., "Bash", "Read")
	Action   Action   `toml:"action"`             // allow, deny, or pass
	Pattern  string   `toml:"pattern,omitempty"`  // Pattern to match (":*" suffix = prefix match)
	Patterns []string `toml:"patterns,omitempty"` // Multiple patterns (any match counts)
	Script   string   `toml:"script,omitempty"`   // Path to validation script
}

// ManagerConfig represents the manager agent configuration.
type ManagerConfig struct {
	// AllowedPatterns are Bash command patterns the manager can run without prompting.
	// Uses the same pattern syntax as permissions.toml (e.g., "fab:*" for prefix match).
	// Defaults to ["fab:*"] if not specified.
	AllowedPatterns []string `toml:"allowed_patterns,omitempty"`
}

// Config represents a permissions configuration file.
type Config struct {
	Rules   []Rule         `toml:"rules"`
	Manager *ManagerConfig `toml:"manager,omitempty"`
}

// DefaultManagerAllowedPatterns returns the default allowed patterns for the manager.
var DefaultManagerAllowedPatterns = []string{"fab:*"}

// LoadConfig loads a permissions configuration from the given path.
// Returns nil config and nil error if the file doesn't exist.
func LoadConfig(path string) (*Config, error) {
	var cfg Config
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("decode rules file %s: %w", path, err)
	}

	// Validate each rule
	for i, rule := range cfg.Rules {
		if err := config.ValidateRule(rule.Tool, string(rule.Action), rule.Pattern, rule.Patterns, rule.Script); err != nil {
			return nil, fmt.Errorf("rule %d: %w", i+1, err)
		}
	}

	// Validate manager config if present
	if cfg.Manager != nil && len(cfg.Manager.AllowedPatterns) > 0 {
		if err := config.ValidateManagerAllowedPatterns(cfg.Manager.AllowedPatterns); err != nil {
			return nil, fmt.Errorf("manager: %w", err)
		}
	}

	return &cfg, nil
}

// ManagerAllowedPatterns returns the manager's allowed patterns from the config.
// Returns default patterns (fab:*) if no manager config is specified.
func (c *Config) ManagerAllowedPatterns() []string {
	if c != nil && c.Manager != nil && len(c.Manager.AllowedPatterns) > 0 {
		return c.Manager.AllowedPatterns
	}
	return DefaultManagerAllowedPatterns
}

// GlobalConfigPath returns the path to the global permissions config.
func GlobalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".config", "fab", "permissions.toml"), nil
}

// ProjectConfigPath returns the path to a project's permissions config.
func ProjectConfigPath(projectName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("get home directory: %w", err)
	}
	return filepath.Join(home, ".fab", "projects", projectName, "permissions.toml"), nil
}
