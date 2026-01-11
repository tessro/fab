package supervisor

import (
	"context"
	"fmt"
	"os"
	"os/exec"

	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/registry"
)

// handleProjectAdd adds a new project.
func (s *Supervisor) handleProjectAdd(ctx context.Context, req *daemon.Request) *daemon.Response {
	var addReq daemon.ProjectAddRequest
	if err := unmarshalPayload(req.Payload, &addReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if addReq.RemoteURL == "" {
		return errorResponse(req, "remote URL required")
	}

	// Register project in config first (validates and generates name)
	proj, err := s.registry.Add(addReq.RemoteURL, addReq.Name, addReq.MaxAgents, addReq.Autostart, addReq.Backend)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("failed to add project: %v", err))
	}

	// Create project directory structure
	projectDir := proj.ProjectDir()
	if err := os.MkdirAll(projectDir, 0755); err != nil {
		_ = s.registry.Remove(proj.Name)
		return errorResponse(req, fmt.Sprintf("failed to create project dir: %v", err))
	}

	// Clone the repository
	repoDir := proj.RepoDir()
	cmd := exec.Command("git", "clone", addReq.RemoteURL, repoDir)
	if output, err := cmd.CombinedOutput(); err != nil {
		_ = s.registry.Remove(proj.Name)
		_ = os.RemoveAll(projectDir)
		return errorResponse(req, fmt.Sprintf("failed to clone: %v\n%s", err, output))
	}

	// Worktrees are created on-demand when agents start

	return successResponse(req, daemon.ProjectAddResponse{
		Name:      proj.Name,
		RemoteURL: proj.RemoteURL,
		RepoDir:   proj.RepoDir(),
		MaxAgents: proj.MaxAgents,
	})
}

// handleProjectRemove removes a project.
func (s *Supervisor) handleProjectRemove(ctx context.Context, req *daemon.Request) *daemon.Response {
	var removeReq daemon.ProjectRemoveRequest
	if err := unmarshalPayload(req.Payload, &removeReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if removeReq.Name == "" {
		return errorResponse(req, "project name required")
	}

	// Stop all agents first
	s.agents.DeleteAll(removeReq.Name)
	s.agents.UnregisterProject(removeReq.Name)

	// Delete worktrees if requested
	if removeReq.DeleteWorktrees {
		proj, err := s.registry.Get(removeReq.Name)
		if err == nil {
			_ = proj.DeleteAllWorktrees()
		}
	}

	if err := s.registry.Remove(removeReq.Name); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to remove project: %v", err))
	}

	return successResponse(req, nil)
}

// handleProjectList lists all projects.
func (s *Supervisor) handleProjectList(ctx context.Context, req *daemon.Request) *daemon.Response {
	projects := s.registry.List()
	infos := make([]daemon.ProjectInfo, 0, len(projects))

	for _, p := range projects {
		infos = append(infos, daemon.ProjectInfo{
			Name:      p.Name,
			RemoteURL: p.RemoteURL,
			MaxAgents: p.MaxAgents,
			Running:   p.IsRunning(),
			Backend:   p.GetAgentBackend(),
		})
	}

	return successResponse(req, daemon.ProjectListResponse{
		Projects: infos,
	})
}

// handleProjectSet updates project settings.
// Deprecated: Use handleProjectConfigSet instead.
func (s *Supervisor) handleProjectSet(ctx context.Context, req *daemon.Request) *daemon.Response {
	var setReq daemon.ProjectSetRequest
	if err := unmarshalPayload(req.Payload, &setReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if setReq.Name == "" {
		return errorResponse(req, "project name required")
	}

	// Update the project settings
	if err := s.registry.Update(setReq.Name, setReq.MaxAgents, setReq.Autostart); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to update project: %v", err))
	}

	// No need to resize worktree pool - worktrees are created/deleted on-demand

	return successResponse(req, nil)
}

// handleProjectConfigShow returns all config for a project.
func (s *Supervisor) handleProjectConfigShow(ctx context.Context, req *daemon.Request) *daemon.Response {
	var showReq daemon.ProjectConfigShowRequest
	if err := unmarshalPayload(req.Payload, &showReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if showReq.Name == "" {
		return errorResponse(req, "project name required")
	}

	config, err := s.registry.GetConfig(showReq.Name)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("failed to get config: %v", err))
	}

	return successResponse(req, daemon.ProjectConfigShowResponse{
		Name:   showReq.Name,
		Config: config,
	})
}

// handleProjectConfigGet returns a single config value for a project.
func (s *Supervisor) handleProjectConfigGet(ctx context.Context, req *daemon.Request) *daemon.Response {
	var getReq daemon.ProjectConfigGetRequest
	if err := unmarshalPayload(req.Payload, &getReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if getReq.Name == "" {
		return errorResponse(req, "project name required")
	}
	if getReq.Key == "" {
		return errorResponse(req, "config key required")
	}

	if !registry.IsValidConfigKey(getReq.Key) {
		return errorResponse(req, fmt.Sprintf("invalid config key: %s (valid keys: max-agents, autostart, issue-backend, permissions-checker, agent-backend)", getReq.Key))
	}

	value, err := s.registry.GetConfigValue(getReq.Name, registry.ConfigKey(getReq.Key))
	if err != nil {
		return errorResponse(req, fmt.Sprintf("failed to get config value: %v", err))
	}

	return successResponse(req, daemon.ProjectConfigGetResponse{
		Name:  getReq.Name,
		Key:   getReq.Key,
		Value: value,
	})
}

// handleProjectConfigSet sets a single config value for a project.
func (s *Supervisor) handleProjectConfigSet(ctx context.Context, req *daemon.Request) *daemon.Response {
	var setReq daemon.ProjectConfigSetRequest
	if err := unmarshalPayload(req.Payload, &setReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if setReq.Name == "" {
		return errorResponse(req, "project name required")
	}
	if setReq.Key == "" {
		return errorResponse(req, "config key required")
	}

	if !registry.IsValidConfigKey(setReq.Key) {
		return errorResponse(req, fmt.Sprintf("invalid config key: %s (valid keys: max-agents, autostart, issue-backend, permissions-checker, agent-backend)", setReq.Key))
	}

	if err := s.registry.SetConfigValue(setReq.Name, registry.ConfigKey(setReq.Key), setReq.Value); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to set config value: %v", err))
	}

	return successResponse(req, nil)
}
