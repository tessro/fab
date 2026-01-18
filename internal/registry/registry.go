// Package registry provides persistent storage for registered projects.
package registry

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/BurntSushi/toml"
	configPkg "github.com/tessro/fab/internal/config"
	"github.com/tessro/fab/internal/paths"
	"github.com/tessro/fab/internal/project"
)

// Default paths for config storage.
const (
	DefaultConfigDir  = ".config/fab"
	DefaultConfigFile = "config.toml"
)

// Errors returned by registry operations.
var (
	ErrProjectExists    = errors.New("project already exists")
	ErrProjectNotFound  = errors.New("project not found")
	ErrInvalidRemoteURL = errors.New("invalid remote URL")
	ErrOldConfigFormat  = errors.New("old config format detected: please re-add projects with 'fab project add <url>'")
)

// ProjectEntry represents a project in the config file.
// Note: TOML tags use hyphens to match CLI config key names (e.g., "max-agents").
type ProjectEntry struct {
	Name               string   `toml:"name"`
	RemoteURL          string   `toml:"remote-url"`
	MaxAgents          int      `toml:"max-agents,omitempty"`
	IssueBackend       string   `toml:"issue-backend,omitempty"`       // "tk" (default), "github", "gh"
	AllowedAuthors     []string `toml:"allowed-authors,omitempty"`     // GitHub usernames allowed to create issues
	Autostart          bool     `toml:"autostart,omitempty"`           // Start orchestration when daemon starts
	PermissionsChecker string   `toml:"permissions-checker,omitempty"` // Permission checker: "manual" (default), "llm"
	AgentBackend       string   `toml:"agent-backend,omitempty"`       // Agent CLI backend: "claude" (default), "codex" - used as fallback
	PlannerBackend     string   `toml:"planner-backend,omitempty"`     // Planner CLI backend: "claude" (default), "codex"
	CodingBackend      string   `toml:"coding-backend,omitempty"`      // Coding agent CLI backend: "claude" (default), "codex"
	MergeStrategy      string   `toml:"merge-strategy,omitempty"`      // Merge strategy: "direct" (default), "pull-request"
	// Deprecated: Path is only used to detect old config format
	Path string `toml:"path,omitempty"`

	// Legacy fields (underscore format) - for backwards compatibility during loading.
	// These are read during load() and merged with hyphen-format fields.
	// When saving, only the hyphen-format fields are written.
	LegacyRemoteURL          string   `toml:"remote_url,omitempty"`
	LegacyMaxAgents          int      `toml:"max_agents,omitempty"`
	LegacyIssueBackend       string   `toml:"issue_backend,omitempty"`
	LegacyAllowedAuthors     []string `toml:"allowed_authors,omitempty"`
	LegacyPermissionsChecker string   `toml:"permissions_checker,omitempty"`
	LegacyAgentBackend       string   `toml:"agent_backend,omitempty"`
}

// coalesce returns the first non-empty string from the provided values.
func coalesce(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// coalesceInt returns the first non-zero int from the provided values.
func coalesceInt(values ...int) int {
	for _, v := range values {
		if v != 0 {
			return v
		}
	}
	return 0
}

// coalesceStringSlice returns the first non-empty slice from the provided values.
func coalesceStringSlice(values ...[]string) []string {
	for _, v := range values {
		if len(v) > 0 {
			return v
		}
	}
	return nil
}

// Config represents the fab configuration file.
// This includes all known fields to preserve them when saving.
type Config struct {
	// LogLevel is preserved from global config.
	LogLevel string `toml:"log_level,omitempty"`

	// Providers is preserved from global config.
	Providers map[string]any `toml:"providers,omitempty"`

	// LLMAuth is preserved from global config.
	LLMAuth map[string]any `toml:"llm_auth,omitempty"`

	// Projects is the list of registered projects.
	Projects []ProjectEntry `toml:"projects"`
}

// Registry manages the persistent collection of projects.
type Registry struct {
	configPath     string // Immutable after creation
	projectBaseDir string // Base directory for project storage (derived from FAB_DIR)
	// +checklocks:mu
	projects map[string]*project.Project
	// globalConfig holds non-project config fields to preserve when saving.
	// +checklocks:mu
	globalConfig *Config
	mu           sync.RWMutex
}

// New creates a new Registry with the default config path.
// Uses paths.ConfigPath() which honors FAB_DIR env var.
func New() (*Registry, error) {
	configPath, err := paths.ConfigPath()
	if err != nil {
		return nil, err
	}
	return NewWithPath(configPath)
}

// NewWithPath creates a new Registry with a custom config path.
func NewWithPath(configPath string) (*Registry, error) {
	projectsDir, err := paths.ProjectsDir()
	if err != nil {
		// Fallback: use direct env var check
		if fabDir := os.Getenv("FAB_DIR"); fabDir != "" {
			projectsDir = filepath.Join(fabDir, "projects")
		} else {
			home, homeErr := os.UserHomeDir()
			if homeErr != nil {
				return nil, homeErr
			}
			projectsDir = filepath.Join(home, ".fab", "projects")
		}
	}

	r := &Registry{
		configPath:     configPath,
		projectBaseDir: projectsDir,
		projects:       make(map[string]*project.Project),
	}

	if err := r.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return r, nil
}

// SetProjectBaseDir sets the base directory for project storage.
// This overrides the FAB_DIR-derived default. Primarily used for testing.
func (r *Registry) SetProjectBaseDir(baseDir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projectBaseDir = baseDir
	// Update BaseDir on all existing projects
	for _, p := range r.projects {
		p.BaseDir = baseDir
	}
}

// load reads the config file and populates the registry.
// It supports both hyphen-format keys (e.g., "remote-url") and legacy underscore-format
// keys (e.g., "remote_url") for backwards compatibility. Hyphen-format takes precedence.
// It also preserves non-project config fields (log_level, providers, llm_auth) for saving.
func (r *Registry) load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var config Config
	if _, err := toml.DecodeFile(r.configPath, &config); err != nil {
		return err
	}

	// Preserve global config fields for saving
	r.globalConfig = &Config{
		LogLevel:  config.LogLevel,
		Providers: config.Providers,
		LLMAuth:   config.LLMAuth,
	}

	for _, entry := range config.Projects {
		// Coalesce hyphen-format and underscore-format fields (hyphen takes precedence)
		remoteURL := coalesce(entry.RemoteURL, entry.LegacyRemoteURL)
		maxAgents := coalesceInt(entry.MaxAgents, entry.LegacyMaxAgents)
		issueBackend := coalesce(entry.IssueBackend, entry.LegacyIssueBackend)
		allowedAuthors := coalesceStringSlice(entry.AllowedAuthors, entry.LegacyAllowedAuthors)
		permissionsChecker := coalesce(entry.PermissionsChecker, entry.LegacyPermissionsChecker)
		agentBackend := coalesce(entry.AgentBackend, entry.LegacyAgentBackend)

		// Detect old config format (has Path but no RemoteURL)
		if entry.Path != "" && remoteURL == "" {
			return ErrOldConfigFormat
		}

		p := project.NewProject(entry.Name, remoteURL)
		p.BaseDir = r.projectBaseDir
		if maxAgents > 0 {
			p.MaxAgents = maxAgents
		}
		if issueBackend != "" {
			p.IssueBackend = issueBackend
		}
		if len(allowedAuthors) > 0 {
			p.AllowedAuthors = allowedAuthors
		}
		p.Autostart = entry.Autostart
		p.PermissionsChecker = permissionsChecker
		p.AgentBackend = agentBackend
		p.PlannerBackend = entry.PlannerBackend
		p.CodingBackend = entry.CodingBackend
		p.MergeStrategy = entry.MergeStrategy
		r.projects[entry.Name] = p
	}

	return nil
}

// save writes the current registry state to the config file.
// It preserves non-project config fields (log_level, providers, llm_auth) that were
// loaded from the file, so that saving projects doesn't erase other configuration.
//
// +checklocks:r.mu
func (r *Registry) save() error {
	// Ensure directory exists
	dir := filepath.Dir(r.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Start with preserved global config fields if available
	config := Config{
		Projects: make([]ProjectEntry, 0, len(r.projects)),
	}
	if r.globalConfig != nil {
		config.LogLevel = r.globalConfig.LogLevel
		config.Providers = r.globalConfig.Providers
		config.LLMAuth = r.globalConfig.LLMAuth
	}

	for _, p := range r.projects {
		config.Projects = append(config.Projects, ProjectEntry{
			Name:               p.Name,
			RemoteURL:          p.RemoteURL,
			MaxAgents:          p.MaxAgents,
			IssueBackend:       p.IssueBackend,
			AllowedAuthors:     p.AllowedAuthors,
			Autostart:          p.Autostart,
			PermissionsChecker: p.PermissionsChecker,
			AgentBackend:       p.AgentBackend,
			PlannerBackend:     p.PlannerBackend,
			CodingBackend:      p.CodingBackend,
			MergeStrategy:      p.MergeStrategy,
		})
	}

	f, err := os.Create(r.configPath)
	if err != nil {
		return err
	}
	defer f.Close()

	encoder := toml.NewEncoder(f)
	return encoder.Encode(config)
}

// Add registers a new project.
// If name is empty, it defaults to the repository name from the URL.
func (r *Registry) Add(remoteURL, name string, maxAgents int, autostart bool, backend string) (*project.Project, error) {
	// Validate remote URL
	if err := configPkg.ValidateRemoteURL(remoteURL); err != nil {
		return nil, ErrInvalidRemoteURL
	}

	// Default name to repo name from URL
	if name == "" {
		name = repoNameFromURL(remoteURL)
	}

	// Validate project name
	if err := configPkg.ValidateProjectName(name); err != nil {
		return nil, err
	}

	// Default max agents
	if maxAgents <= 0 {
		maxAgents = project.DefaultMaxAgents
	}

	// Validate max agents
	if err := configPkg.ValidateMaxAgents(maxAgents); err != nil {
		return nil, err
	}

	// Validate and normalize backend
	if backend != "" {
		backend = strings.ToLower(backend)
		if backend != "claude" && backend != "codex" {
			return nil, errors.New("invalid backend: must be 'claude' or 'codex'")
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for duplicates
	if _, exists := r.projects[name]; exists {
		return nil, ErrProjectExists
	}

	p := project.NewProject(name, remoteURL)
	p.MaxAgents = maxAgents
	p.Autostart = autostart
	p.AgentBackend = backend
	p.BaseDir = r.projectBaseDir // Empty unless set for testing
	r.projects[name] = p

	if err := r.save(); err != nil {
		delete(r.projects, name)
		return nil, err
	}

	return p, nil
}

// repoNameFromURL extracts the repository name from a git URL.
// Examples:
//   - git@github.com:user/repo.git -> repo
//   - https://github.com/user/repo.git -> repo
//   - https://github.com/user/repo -> repo
func repoNameFromURL(url string) string {
	base := filepath.Base(url)
	// Remove .git suffix if present
	if len(base) > 4 && base[len(base)-4:] == ".git" {
		return base[:len(base)-4]
	}
	return base
}

// Remove unregisters a project by name.
func (r *Registry) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.projects[name]; !exists {
		return ErrProjectNotFound
	}

	delete(r.projects, name)
	return r.save()
}

// Get returns a project by name.
func (r *Registry) Get(name string) (*project.Project, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.projects[name]
	if !exists {
		return nil, ErrProjectNotFound
	}
	return p, nil
}

// List returns all registered projects.
func (r *Registry) List() []*project.Project {
	r.mu.RLock()
	defer r.mu.RUnlock()

	projects := make([]*project.Project, 0, len(r.projects))
	for _, p := range r.projects {
		projects = append(projects, p)
	}
	return projects
}

// Update modifies a project's settings.
func (r *Registry) Update(name string, maxAgents *int, autostart *bool) error {
	// Validate max agents if provided
	if maxAgents != nil {
		if err := configPkg.ValidateMaxAgents(*maxAgents); err != nil {
			return err
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	p, exists := r.projects[name]
	if !exists {
		return ErrProjectNotFound
	}

	if maxAgents != nil {
		p.MaxAgents = *maxAgents
	}
	if autostart != nil {
		p.Autostart = *autostart
	}

	return r.save()
}

// ConfigKey represents a valid project configuration key.
type ConfigKey string

// Valid configuration keys.
const (
	ConfigKeyMaxAgents          ConfigKey = "max-agents"
	ConfigKeyAutostart          ConfigKey = "autostart"
	ConfigKeyIssueBackend       ConfigKey = "issue-backend"
	ConfigKeyAllowedAuthors     ConfigKey = "allowed-authors"
	ConfigKeyPermissionsChecker ConfigKey = "permissions-checker"
	ConfigKeyAgentBackend       ConfigKey = "agent-backend"
	ConfigKeyPlannerBackend     ConfigKey = "planner-backend"
	ConfigKeyCodingBackend      ConfigKey = "coding-backend"
	ConfigKeyMergeStrategy      ConfigKey = "merge-strategy"
)

// ValidConfigKeys returns all valid configuration keys.
func ValidConfigKeys() []ConfigKey {
	return []ConfigKey{ConfigKeyMaxAgents, ConfigKeyAutostart, ConfigKeyIssueBackend, ConfigKeyAllowedAuthors, ConfigKeyPermissionsChecker, ConfigKeyAgentBackend, ConfigKeyPlannerBackend, ConfigKeyCodingBackend, ConfigKeyMergeStrategy}
}

// IsValidConfigKey returns true if the key is a valid configuration key.
func IsValidConfigKey(key string) bool {
	for _, k := range ValidConfigKeys() {
		if string(k) == key {
			return true
		}
	}
	return false
}

// GetConfigValue returns the value of a single configuration key for a project.
func (r *Registry) GetConfigValue(name string, key ConfigKey) (any, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.projects[name]
	if !exists {
		return nil, ErrProjectNotFound
	}

	switch key {
	case ConfigKeyMaxAgents:
		return p.MaxAgents, nil
	case ConfigKeyAutostart:
		return p.Autostart, nil
	case ConfigKeyIssueBackend:
		backend := p.IssueBackend
		if backend == "" {
			backend = "tk"
		}
		return backend, nil
	case ConfigKeyAllowedAuthors:
		return p.AllowedAuthors, nil
	case ConfigKeyPermissionsChecker:
		checker := p.PermissionsChecker
		if checker == "" {
			checker = "manual"
		}
		return checker, nil
	case ConfigKeyAgentBackend:
		return p.GetAgentBackend(), nil
	case ConfigKeyPlannerBackend:
		return p.GetPlannerBackend(), nil
	case ConfigKeyCodingBackend:
		return p.GetCodingBackend(), nil
	case ConfigKeyMergeStrategy:
		return p.GetMergeStrategy(), nil
	default:
		return nil, errors.New("invalid configuration key")
	}
}

// GetConfig returns all configuration for a project as a map.
func (r *Registry) GetConfig(name string) (map[string]any, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, exists := r.projects[name]
	if !exists {
		return nil, ErrProjectNotFound
	}

	issueBackend := p.IssueBackend
	if issueBackend == "" {
		issueBackend = "tk"
	}

	permissionsChecker := p.PermissionsChecker
	if permissionsChecker == "" {
		permissionsChecker = "manual"
	}

	return map[string]any{
		string(ConfigKeyMaxAgents):          p.MaxAgents,
		string(ConfigKeyAutostart):          p.Autostart,
		string(ConfigKeyIssueBackend):       issueBackend,
		string(ConfigKeyAllowedAuthors):     p.AllowedAuthors,
		string(ConfigKeyPermissionsChecker): permissionsChecker,
		string(ConfigKeyAgentBackend):       p.GetAgentBackend(),
		string(ConfigKeyPlannerBackend):     p.GetPlannerBackend(),
		string(ConfigKeyCodingBackend):      p.GetCodingBackend(),
		string(ConfigKeyMergeStrategy):      p.GetMergeStrategy(),
	}, nil
}

// SetConfigValue sets a single configuration key for a project.
func (r *Registry) SetConfigValue(name string, key ConfigKey, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, exists := r.projects[name]
	if !exists {
		return ErrProjectNotFound
	}

	switch key {
	case ConfigKeyMaxAgents:
		maxAgents, err := strconv.Atoi(value)
		if err != nil {
			return errors.New("invalid value for max-agents: must be a positive integer")
		}
		if err := configPkg.ValidateMaxAgents(maxAgents); err != nil {
			return err
		}
		p.MaxAgents = maxAgents
	case ConfigKeyAutostart:
		autostart, err := strconv.ParseBool(value)
		if err != nil {
			return errors.New("invalid value for autostart: must be true or false")
		}
		p.Autostart = autostart
	case ConfigKeyIssueBackend:
		v := strings.ToLower(value)
		if v != "tk" && v != "github" && v != "gh" {
			return errors.New("invalid value for issue-backend: must be 'tk', 'github', or 'gh'")
		}
		p.IssueBackend = v
	case ConfigKeyAllowedAuthors:
		// Parse comma-separated list of GitHub usernames
		if value == "" {
			p.AllowedAuthors = nil
		} else {
			authors := strings.Split(value, ",")
			for i, a := range authors {
				authors[i] = strings.TrimSpace(a)
			}
			p.AllowedAuthors = authors
		}
	case ConfigKeyPermissionsChecker:
		v := strings.ToLower(value)
		if v != "manual" && v != "llm" {
			return errors.New("invalid value for permissions-checker: must be 'manual' or 'llm'")
		}
		p.PermissionsChecker = v
	case ConfigKeyAgentBackend:
		v := strings.ToLower(value)
		if v != "claude" && v != "codex" {
			return errors.New("invalid value for agent-backend: must be 'claude' or 'codex'")
		}
		p.AgentBackend = v
	case ConfigKeyPlannerBackend:
		v := strings.ToLower(value)
		if v != "claude" && v != "codex" {
			return errors.New("invalid value for planner-backend: must be 'claude' or 'codex'")
		}
		p.PlannerBackend = v
	case ConfigKeyCodingBackend:
		v := strings.ToLower(value)
		if v != "claude" && v != "codex" {
			return errors.New("invalid value for coding-backend: must be 'claude' or 'codex'")
		}
		p.CodingBackend = v
	case ConfigKeyMergeStrategy:
		v := strings.ToLower(value)
		if v != "direct" && v != "pull-request" {
			return errors.New("invalid value for merge-strategy: must be 'direct' or 'pull-request'")
		}
		p.MergeStrategy = v
	default:
		return errors.New("invalid configuration key")
	}

	return r.save()
}

// Count returns the number of registered projects.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.projects)
}

// ConfigPath returns the path to the config file.
func (r *Registry) ConfigPath() string {
	return r.configPath
}
