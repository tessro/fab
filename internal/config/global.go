// Package config provides configuration validation and loading for fab.
package config

import (
	"os"

	"github.com/BurntSushi/toml"

	"github.com/tessro/fab/internal/paths"
)

// GlobalConfig represents the global fab configuration.
type GlobalConfig struct {
	// LogLevel controls logging verbosity ("debug", "info", "warn", "error").
	// Defaults to "info" if not specified.
	LogLevel string `toml:"log-level"`

	// Providers contains API provider configurations.
	Providers ProvidersConfig `toml:"providers"`

	// LLMAuth contains LLM authorization settings.
	LLMAuth LLMAuthConfig `toml:"llm-auth"`

	// Defaults contains default values for project configuration.
	Defaults DefaultsConfig `toml:"defaults"`
}

// DefaultsConfig contains default values for project configuration.
// These values are used when a project doesn't specify its own value.
// The config precedence stack is: project -> global defaults -> internal defaults.
type DefaultsConfig struct {
	// AgentBackend is the default agent CLI backend ("claude" or "codex").
	AgentBackend string `toml:"agent-backend"`
	// PlannerBackend is the default planner CLI backend ("claude" or "codex").
	// Falls back to AgentBackend if not set.
	PlannerBackend string `toml:"planner-backend"`
	// CodingBackend is the default coding agent CLI backend ("claude" or "codex").
	// Falls back to AgentBackend if not set.
	CodingBackend string `toml:"coding-backend"`
	// MergeStrategy is the default merge strategy ("direct" or "pull-request").
	MergeStrategy string `toml:"merge-strategy"`
	// IssueBackend is the default issue backend ("tk", "github", "gh", or "linear").
	IssueBackend string `toml:"issue-backend"`
	// PermissionsChecker is the default permission checker ("manual" or "llm").
	PermissionsChecker string `toml:"permissions-checker"`
	// Autostart determines whether new projects should auto-start by default.
	Autostart *bool `toml:"autostart"`
	// MaxAgents is the default max concurrent agents per project.
	MaxAgents int `toml:"max-agents"`
}

// ProvidersConfig contains API provider configurations.
type ProvidersConfig struct {
	Anthropic *ProviderConfig `toml:"anthropic"`
	OpenAI    *ProviderConfig `toml:"openai"`
	Linear    *ProviderConfig `toml:"linear"`
	GitHub    *ProviderConfig `toml:"github"`
}

// ProviderConfig contains configuration for a single API provider.
type ProviderConfig struct {
	APIKey string `toml:"api-key"`
}

// LLMAuthConfig contains LLM authorization settings.
type LLMAuthConfig struct {
	// Provider is which provider to use for authorization ("anthropic" or "openai").
	Provider string `toml:"provider"`
	// Model is the model to use for authorization (e.g., "claude-haiku-4-5").
	Model string `toml:"model"`
}

// DefaultLLMAuthProvider is the default provider for LLM authorization.
const DefaultLLMAuthProvider = "anthropic"

// DefaultLLMAuthModel is the default model for LLM authorization.
const DefaultLLMAuthModel = "claude-haiku-4-5"

// DefaultLogLevel is the default logging level.
const DefaultLogLevel = "info"

// GlobalConfigPath returns the path to the global fab config.
func GlobalConfigPath() (string, error) {
	return paths.ConfigPath()
}

// LoadGlobalConfig loads the global fab configuration.
// Returns nil config and nil error if the file doesn't exist.
func LoadGlobalConfig() (*GlobalConfig, error) {
	path, err := GlobalConfigPath()
	if err != nil {
		return nil, err
	}
	return LoadGlobalConfigFromPath(path)
}

// LoadGlobalConfigFromPath loads the global config from a specific path.
// Returns nil config and nil error if the file doesn't exist.
func LoadGlobalConfigFromPath(path string) (*GlobalConfig, error) {
	var cfg GlobalConfig
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	return &cfg, nil
}

// GetAPIKey returns the API key for the given provider from the global config.
// Returns empty string if not configured.
func (c *GlobalConfig) GetAPIKey(provider string) string {
	if c == nil {
		return ""
	}
	switch provider {
	case "anthropic":
		if c.Providers.Anthropic != nil {
			return c.Providers.Anthropic.APIKey
		}
	case "openai":
		if c.Providers.OpenAI != nil {
			return c.Providers.OpenAI.APIKey
		}
	case "linear":
		if c.Providers.Linear != nil {
			return c.Providers.Linear.APIKey
		}
	case "github":
		if c.Providers.GitHub != nil {
			return c.Providers.GitHub.APIKey
		}
	}
	return ""
}

// GetLLMAuthProvider returns the configured LLM auth provider or the default.
func (c *GlobalConfig) GetLLMAuthProvider() string {
	if c != nil && c.LLMAuth.Provider != "" {
		return c.LLMAuth.Provider
	}
	return DefaultLLMAuthProvider
}

// GetLLMAuthModel returns the configured LLM auth model or the default.
func (c *GlobalConfig) GetLLMAuthModel() string {
	if c != nil && c.LLMAuth.Model != "" {
		return c.LLMAuth.Model
	}
	return DefaultLLMAuthModel
}

// GetLogLevel returns the configured log level or the default.
func (c *GlobalConfig) GetLogLevel() string {
	if c != nil && c.LogLevel != "" {
		return c.LogLevel
	}
	return DefaultLogLevel
}

// GetDefaultAgentBackend returns the configured default agent backend or "claude".
func (c *GlobalConfig) GetDefaultAgentBackend() string {
	if c != nil && c.Defaults.AgentBackend != "" {
		return c.Defaults.AgentBackend
	}
	return "claude"
}

// DefaultMergeStrategy is the default merge strategy.
const DefaultMergeStrategy = "direct"

// GetDefaultMergeStrategy returns the configured default merge strategy or "direct".
func (c *GlobalConfig) GetDefaultMergeStrategy() string {
	if c != nil && c.Defaults.MergeStrategy != "" {
		return c.Defaults.MergeStrategy
	}
	return DefaultMergeStrategy
}

// GetDefaultPlannerBackend returns the configured default planner backend.
// Falls back to agent backend if not set.
func (c *GlobalConfig) GetDefaultPlannerBackend() string {
	if c != nil && c.Defaults.PlannerBackend != "" {
		return c.Defaults.PlannerBackend
	}
	return c.GetDefaultAgentBackend()
}

// GetDefaultCodingBackend returns the configured default coding backend.
// Falls back to agent backend if not set.
func (c *GlobalConfig) GetDefaultCodingBackend() string {
	if c != nil && c.Defaults.CodingBackend != "" {
		return c.Defaults.CodingBackend
	}
	return c.GetDefaultAgentBackend()
}

// DefaultIssueBackend is the internal default for issue backend.
const DefaultIssueBackend = "tk"

// GetDefaultIssueBackend returns the configured default issue backend or "tk".
func (c *GlobalConfig) GetDefaultIssueBackend() string {
	if c != nil && c.Defaults.IssueBackend != "" {
		return c.Defaults.IssueBackend
	}
	return DefaultIssueBackend
}

// DefaultPermissionsChecker is the internal default for permission checking.
const DefaultPermissionsChecker = "manual"

// GetDefaultPermissionsChecker returns the configured default permissions checker or "manual".
func (c *GlobalConfig) GetDefaultPermissionsChecker() string {
	if c != nil && c.Defaults.PermissionsChecker != "" {
		return c.Defaults.PermissionsChecker
	}
	return DefaultPermissionsChecker
}

// GetDefaultAutostart returns the configured default autostart setting.
// Returns false if not set (internal default).
func (c *GlobalConfig) GetDefaultAutostart() bool {
	if c != nil && c.Defaults.Autostart != nil {
		return *c.Defaults.Autostart
	}
	return false
}

// DefaultMaxAgents is the internal default for max agents per project.
const DefaultMaxAgents = 3

// GetDefaultMaxAgents returns the configured default max agents or 3.
func (c *GlobalConfig) GetDefaultMaxAgents() int {
	if c != nil && c.Defaults.MaxAgents > 0 {
		return c.Defaults.MaxAgents
	}
	return DefaultMaxAgents
}
