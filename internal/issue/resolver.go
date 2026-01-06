package issue

import (
	"fmt"
	"os"
	"strings"

	"github.com/tessro/fab/internal/registry"
)

// ResolveProject determines which project to use for issue operations.
// Priority: explicit flag > FAB_PROJECT env > cwd detection
func ResolveProject(explicit string) (string, error) {
	// 1. Explicit flag takes precedence
	if explicit != "" {
		return explicit, nil
	}

	// 2. Check FAB_PROJECT env var (set for agents)
	if project := os.Getenv("FAB_PROJECT"); project != "" {
		return project, nil
	}

	// 3. Detect from cwd
	return detectProjectFromCwd()
}

// detectProjectFromCwd checks if cwd is within a registered project.
func detectProjectFromCwd() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("get cwd: %w", err)
	}

	reg, err := registry.New()
	if err != nil {
		return "", fmt.Errorf("load registry: %w", err)
	}

	projects := reg.List()
	for _, p := range projects {
		// Check main repo
		if strings.HasPrefix(cwd, p.RepoDir()) {
			return p.Name, nil
		}
		// Check worktrees
		if strings.HasPrefix(cwd, p.WorktreesDir()) {
			return p.Name, nil
		}
	}

	return "", fmt.Errorf("not in a registered project directory")
}
