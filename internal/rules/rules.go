// Package rules provides permission rule evaluation for tool invocations.
package rules

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
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
	Tool   string   `toml:"tool"`               // Tool name to match (e.g., "Bash", "Read")
	Action Action   `toml:"action"`             // allow, deny, or pass
	Pattern  string   `toml:"pattern,omitempty"`  // Pattern to match (":*" suffix = prefix match)
	Patterns []string `toml:"patterns,omitempty"` // Multiple patterns (any match counts)
	Script   string   `toml:"script,omitempty"`   // Path to validation script
}

// Config represents a permissions configuration file.
type Config struct {
	Rules []Rule `toml:"rules"`
}

// LoadConfig loads a permissions configuration from the given path.
// Returns nil config and nil error if the file doesn't exist.
func LoadConfig(path string) (*Config, error) {
	var config Config
	if _, err := toml.DecodeFile(path, &config); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &config, nil
}

// GlobalConfigPath returns the path to the global permissions config.
func GlobalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "fab", "permissions.toml"), nil
}

// ProjectConfigPath returns the path to a project's permissions config.
func ProjectConfigPath(projectName string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".fab", "projects", projectName, "permissions.toml"), nil
}
