package rules

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
)

// Evaluator loads and evaluates permission rules.
type Evaluator struct {
	mu    sync.RWMutex
	cache map[string]*cachedConfig // path -> cached config
}

type cachedConfig struct {
	config   *Config
	modTime  time.Time
	loadedAt time.Time
}

// NewEvaluator creates a new rule evaluator.
func NewEvaluator() *Evaluator {
	return &Evaluator{
		cache: make(map[string]*cachedConfig),
	}
}

// Evaluate checks permission rules for a tool invocation.
// projectName is optional; if empty, only global rules are checked.
// Returns (effect, matched, error) where matched indicates if any rule applied.
func (e *Evaluator) Evaluate(ctx context.Context, projectName, toolName string, toolInput json.RawMessage) (Effect, bool, error) {
	// Load rules: project first, then global
	var allRules []Rule

	// Load project-specific rules if project name is provided
	if projectName != "" {
		projectPath, err := ProjectConfigPath(projectName)
		if err == nil {
			config, err := e.loadCached(projectPath)
			if err != nil {
				slog.Debug("failed to load project rules", "path", projectPath, "error", err)
				return EffectPass, false, err
			}
			if config != nil {
				slog.Debug("loaded project rules", "path", projectPath, "count", len(config.Rules))
				allRules = append(allRules, config.Rules...)
			}
		}
	}

	// Load global rules
	globalPath, err := GlobalConfigPath()
	if err != nil {
		return EffectPass, false, err
	}
	globalConfig, err := e.loadCached(globalPath)
	if err != nil {
		slog.Debug("failed to load global rules", "path", globalPath, "error", err)
		return EffectPass, false, err
	}
	if globalConfig != nil {
		slog.Debug("loaded global rules", "path", globalPath, "count", len(globalConfig.Rules))
		allRules = append(allRules, globalConfig.Rules...)
	}

	// Evaluate rules in order
	primaryField := ResolvePrimaryField(toolName, toolInput)
	slog.Debug("evaluating rules", "tool", toolName, "primaryField", primaryField, "ruleCount", len(allRules))

	for _, rule := range allRules {
		// Check if rule applies to this tool
		if rule.Tool != toolName {
			continue
		}

		// Check matcher
		matched := false
		if rule.Script != "" {
			// Script matcher
			effect, err := ScriptMatch(ctx, rule.Script, toolName, toolInput)
			if err != nil {
				// Script error, skip to next rule
				continue
			}
			if effect != EffectPass {
				return effect, true, nil
			}
			// Script returned pass, continue to next rule
			continue
		} else if rule.Pattern != "" {
			// Single pattern matcher
			matched = MatchPattern(rule.Pattern, primaryField)
		} else if len(rule.Patterns) > 0 {
			// Multiple patterns - any match counts
			for _, p := range rule.Patterns {
				if MatchPattern(p, primaryField) {
					matched = true
					break
				}
			}
		} else {
			// No matcher specified - matches all
			matched = true
		}

		if matched {
			slog.Debug("rule matched", "tool", rule.Tool, "effect", rule.Effect, "pattern", rule.Pattern, "patterns", rule.Patterns)
			if rule.Effect == EffectPass {
				// Explicit pass, continue to next rule
				continue
			}
			return rule.Effect, true, nil
		}
	}

	// No rule matched
	slog.Debug("no rule matched", "tool", toolName, "primaryField", primaryField)
	return EffectPass, false, nil
}

// loadCached loads a config with caching based on file modification time.
func (e *Evaluator) loadCached(path string) (*Config, error) {
	// Check file stat
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	e.mu.RLock()
	cached, exists := e.cache[path]
	e.mu.RUnlock()

	// Return cached if still valid
	if exists && cached.modTime.Equal(info.ModTime()) {
		return cached.config, nil
	}

	// Load fresh
	config, err := LoadConfig(path)
	if err != nil {
		return nil, err
	}

	// Update cache
	e.mu.Lock()
	e.cache[path] = &cachedConfig{
		config:   config,
		modTime:  info.ModTime(),
		loadedAt: time.Now(),
	}
	e.mu.Unlock()

	return config, nil
}

// InvalidateCache clears all cached configurations.
func (e *Evaluator) InvalidateCache() {
	e.mu.Lock()
	e.cache = make(map[string]*cachedConfig)
	e.mu.Unlock()
}

// projectConfig is the structure of the fab config file for looking up projects.
type projectConfig struct {
	Projects []projectEntry `toml:"projects"`
}

type projectEntry struct {
	Name string `toml:"name"`
	Path string `toml:"path"`
}

// FindProjectName attempts to find the project name for a given working directory
// by reading the fab config and checking if any project path contains the cwd.
func FindProjectName(cwd string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	configPath := filepath.Join(home, ".config", "fab", "config.toml")

	var config projectConfig
	if _, err := toml.DecodeFile(configPath, &config); err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}

	// Normalize cwd
	cwd, err = filepath.Abs(cwd)
	if err != nil {
		return "", err
	}

	// Find project that contains cwd
	for _, p := range config.Projects {
		projectPath, err := filepath.Abs(p.Path)
		if err != nil {
			continue
		}

		// Check if cwd is within project path
		if strings.HasPrefix(cwd, projectPath) {
			// Verify it's actually within (not just prefix match on different dir)
			rel, err := filepath.Rel(projectPath, cwd)
			if err == nil && !strings.HasPrefix(rel, "..") {
				return p.Name, nil
			}
		}
	}

	return "", nil
}
