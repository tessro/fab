// Package config provides configuration validation and loading for fab.
package config

import (
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// GlobalConfig represents the global fab configuration.
type GlobalConfig struct {
	// Providers contains API provider configurations.
	Providers ProvidersConfig `toml:"providers"`

	// LLMAuth contains LLM authorization settings.
	LLMAuth LLMAuthConfig `toml:"llm_auth"`
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
	// Model is the model to use for authorization (e.g., "claude-haiku-4-5-20250514").
	Model string `toml:"model"`
}

// DefaultLLMAuthProvider is the default provider for LLM authorization.
const DefaultLLMAuthProvider = "anthropic"

// DefaultLLMAuthModel is the default model for LLM authorization.
const DefaultLLMAuthModel = "claude-haiku-4-5-20250514"

// GlobalConfigPath returns the path to the global fab config.
func GlobalConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "fab", "config.toml"), nil
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
