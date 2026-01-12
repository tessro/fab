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
	LogLevel string `toml:"log_level"`

	// Providers contains API provider configurations.
	Providers ProvidersConfig `toml:"providers"`

	// LLMAuth contains LLM authorization settings.
	LLMAuth LLMAuthConfig `toml:"llm_auth"`

	// Defaults contains default values for project configuration.
	Defaults DefaultsConfig `toml:"defaults"`
}

// DefaultsConfig contains default values for project configuration.
type DefaultsConfig struct {
	// AgentBackend is the default agent CLI backend ("claude" or "codex").
	AgentBackend string `toml:"agent-backend"`
}

// ProvidersConfig contains API provider configurations.
type ProvidersConfig struct {
	Anthropic *ProviderConfig `toml:"anthropic"`
	OpenAI    *ProviderConfig `toml:"openai"`
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
