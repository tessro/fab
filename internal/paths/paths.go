// Package paths provides a single source of truth for fab file paths.
// All path helpers honor environment variable overrides for isolated testing.
//
// Path resolution precedence:
//  1. Specific env vars (FAB_SOCKET_PATH, FAB_PID_PATH) take highest priority
//  2. FAB_DIR env var sets the base directory (derives socket/pid/config/projects)
//  3. Default behavior (~/.fab, ~/.config/fab) when no env vars are set
package paths

import (
	"os"
	"path/filepath"
)

// Environment variable names for path overrides.
const (
	// EnvFabDir is the base directory override (e.g., /tmp/fab-e2e).
	// When set, socket, PID, and projects paths derive from this directory.
	EnvFabDir = "FAB_DIR"

	// EnvSocketPath overrides the socket path directly.
	EnvSocketPath = "FAB_SOCKET_PATH"

	// EnvPIDPath overrides the PID file path directly.
	EnvPIDPath = "FAB_PID_PATH"
)

// BaseDir returns the fab base directory (~/.fab by default).
// Honors FAB_DIR environment variable.
func BaseDir() (string, error) {
	if dir := os.Getenv(EnvFabDir); dir != "" {
		return dir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".fab"), nil
}

// ConfigDir returns the fab config directory (~/.config/fab by default).
// When FAB_DIR is set, returns FAB_DIR/config instead.
func ConfigDir() (string, error) {
	if dir := os.Getenv(EnvFabDir); dir != "" {
		return filepath.Join(dir, "config"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "fab"), nil
}

// ConfigPath returns the path to the global fab config file.
// (~/.config/fab/config.toml by default, or FAB_DIR/config/config.toml).
func ConfigPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.toml"), nil
}

// PermissionsPath returns the path to the global permissions config file.
// (~/.config/fab/permissions.toml by default, or FAB_DIR/config/permissions.toml).
func PermissionsPath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "permissions.toml"), nil
}

// ProjectsDir returns the projects directory (~/.fab/projects by default).
// When FAB_DIR is set, returns FAB_DIR/projects.
func ProjectsDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "projects"), nil
}

// ProjectDir returns the directory for a specific project.
func ProjectDir(projectName string) (string, error) {
	projects, err := ProjectsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(projects, projectName), nil
}

// ProjectPermissionsPath returns the path to a project's permissions config.
func ProjectPermissionsPath(projectName string) (string, error) {
	projDir, err := ProjectDir(projectName)
	if err != nil {
		return "", err
	}
	return filepath.Join(projDir, "permissions.toml"), nil
}

// SocketPath returns the daemon socket path.
// Precedence: FAB_SOCKET_PATH > FAB_DIR/fab.sock > ~/.fab/fab.sock
func SocketPath() string {
	if path := os.Getenv(EnvSocketPath); path != "" {
		return path
	}
	base, err := BaseDir()
	if err != nil {
		return "/tmp/fab.sock"
	}
	return filepath.Join(base, "fab.sock")
}

// PIDPath returns the daemon PID file path.
// Precedence: FAB_PID_PATH > FAB_DIR/fab.pid > ~/.fab/fab.pid
func PIDPath() string {
	if path := os.Getenv(EnvPIDPath); path != "" {
		return path
	}
	base, err := BaseDir()
	if err != nil {
		return "/tmp/fab.pid"
	}
	return filepath.Join(base, "fab.pid")
}

// PlansDir returns the plans directory (~/.fab/plans by default).
// When FAB_DIR is set, returns FAB_DIR/plans.
func PlansDir() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "plans"), nil
}

// PlanPath returns the path to a specific plan file.
func PlanPath(planID string) (string, error) {
	dir, err := PlansDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, planID+".md"), nil
}
