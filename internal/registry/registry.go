// Package registry provides persistent storage for registered projects.
package registry

import (
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/BurntSushi/toml"
	"github.com/tessro/fab/internal/config"
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
type ProjectEntry struct {
	Name      string `toml:"name"`
	RemoteURL string `toml:"remote_url"`
	MaxAgents int    `toml:"max_agents,omitempty"`
	// Deprecated: Path is only used to detect old config format
	Path string `toml:"path,omitempty"`
}

// Config represents the fab configuration file.
type Config struct {
	Projects []ProjectEntry `toml:"projects"`
}

// Registry manages the persistent collection of projects.
type Registry struct {
	configPath     string // Immutable after creation
	projectBaseDir string // Base directory for project storage (testing only)
	// +checklocks:mu
	projects map[string]*project.Project
	mu       sync.RWMutex
}

// New creates a new Registry with the default config path.
func New() (*Registry, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	configPath := filepath.Join(home, DefaultConfigDir, DefaultConfigFile)
	return NewWithPath(configPath)
}

// NewWithPath creates a new Registry with a custom config path.
func NewWithPath(configPath string) (*Registry, error) {
	r := &Registry{
		configPath: configPath,
		projects:   make(map[string]*project.Project),
	}

	if err := r.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return r, nil
}

// SetProjectBaseDir sets the base directory for project storage.
// This is intended for testing to redirect project directories to temp locations.
func (r *Registry) SetProjectBaseDir(baseDir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.projectBaseDir = baseDir
}

// load reads the config file and populates the registry.
func (r *Registry) load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	var config Config
	if _, err := toml.DecodeFile(r.configPath, &config); err != nil {
		return err
	}

	for _, entry := range config.Projects {
		// Detect old config format (has Path but no RemoteURL)
		if entry.Path != "" && entry.RemoteURL == "" {
			return ErrOldConfigFormat
		}

		p := project.NewProject(entry.Name, entry.RemoteURL)
		if entry.MaxAgents > 0 {
			p.MaxAgents = entry.MaxAgents
		}
		r.projects[entry.Name] = p
	}

	return nil
}

// save writes the current registry state to the config file.
//
// +checklocks:r.mu
func (r *Registry) save() error {
	// Ensure directory exists
	dir := filepath.Dir(r.configPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	config := Config{
		Projects: make([]ProjectEntry, 0, len(r.projects)),
	}

	for _, p := range r.projects {
		config.Projects = append(config.Projects, ProjectEntry{
			Name:      p.Name,
			RemoteURL: p.RemoteURL,
			MaxAgents: p.MaxAgents,
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
func (r *Registry) Add(remoteURL, name string, maxAgents int) (*project.Project, error) {
	// Validate remote URL
	if err := config.ValidateRemoteURL(remoteURL); err != nil {
		return nil, ErrInvalidRemoteURL
	}

	// Default name to repo name from URL
	if name == "" {
		name = repoNameFromURL(remoteURL)
	}

	// Validate project name
	if err := config.ValidateProjectName(name); err != nil {
		return nil, err
	}

	// Default max agents
	if maxAgents <= 0 {
		maxAgents = project.DefaultMaxAgents
	}

	// Validate max agents
	if err := config.ValidateMaxAgents(maxAgents); err != nil {
		return nil, err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	// Check for duplicates
	if _, exists := r.projects[name]; exists {
		return nil, ErrProjectExists
	}

	p := project.NewProject(name, remoteURL)
	p.MaxAgents = maxAgents
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
func (r *Registry) Update(name string, maxAgents *int) error {
	// Validate max agents if provided
	if maxAgents != nil {
		if err := config.ValidateMaxAgents(*maxAgents); err != nil {
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
