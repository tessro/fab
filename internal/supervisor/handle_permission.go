package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/llmauth"
	"github.com/tessro/fab/internal/logging"
	"github.com/tessro/fab/internal/project"
)

// handlePermissionRequest handles a permission request from the hook command.
// This blocks until a TUI client responds via permission.respond, or if LLM auth
// is enabled for the project, uses LLM to make the decision automatically.
func (s *Supervisor) handlePermissionRequest(ctx context.Context, req *daemon.Request) *daemon.Response {
	var permReq daemon.PermissionRequestPayload
	if err := unmarshalPayload(req.Payload, &permReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if permReq.ToolName == "" {
		return errorResponse(req, "tool_name is required")
	}

	// Find the project and agent for this request
	var projectName string
	var agentTask string
	var conversationCtx []string
	var proj *project.Project

	if permReq.AgentID != "" {
		// Check if this is a planner (agent ID starts with "plan:")
		if strings.HasPrefix(permReq.AgentID, "plan:") {
			plannerID := strings.TrimPrefix(permReq.AgentID, "plan:")
			if p, err := s.planners.Get(plannerID); err == nil {
				info := p.Info()
				projectName = info.Project
				agentTask = "Planning agent"

				// Get recent conversation history for context
				entries := p.History().Entries(10) // Last 10 entries
				for _, e := range entries {
					if e.Role == "assistant" && e.Content != "" {
						conversationCtx = append(conversationCtx, fmt.Sprintf("Assistant: %s", truncate(e.Content, 500)))
					} else if e.Role == "user" && e.Content != "" {
						conversationCtx = append(conversationCtx, fmt.Sprintf("User: %s", truncate(e.Content, 500)))
					}
				}

				// Look up project to check LLM auth setting
				if projectName != "" {
					proj, _ = s.registry.Get(projectName)
				}
			}
		} else if a, err := s.agents.Get(permReq.AgentID); err == nil {
			info := a.Info()
			projectName = info.Project
			agentTask = info.Description
			if agentTask == "" {
				agentTask = info.Task
			}

			// Get recent conversation history for context
			entries := a.History().Entries(10) // Last 10 entries
			for _, e := range entries {
				if e.Role == "assistant" && e.Content != "" {
					conversationCtx = append(conversationCtx, fmt.Sprintf("Assistant: %s", truncate(e.Content, 500)))
				} else if e.Role == "user" && e.Content != "" {
					conversationCtx = append(conversationCtx, fmt.Sprintf("User: %s", truncate(e.Content, 500)))
				}
			}

			// Look up project to check LLM auth setting
			proj, _ = s.registry.Get(projectName)
		}
	}

	// Create a scoped logger with agent and project context
	log := slog.With("agent", permReq.AgentID, "project", projectName)

	log.Info("permission request received",
		"tool", permReq.ToolName,
		"input", logging.TruncateForLog(string(permReq.ToolInput), 200),
	)

	// Check if LLM permissions checker is enabled for this project
	// Uses config precedence: project -> global defaults -> internal defaults
	if proj != nil && proj.GetPermissionsChecker() == "llm" {
		resp := s.handleLLMAuth(ctx, permReq, projectName, agentTask, conversationCtx, log)
		if resp != nil {
			return successResponse(req, resp)
		}
		// LLM auth failed (e.g., no API key, API error) - block instead of falling back to TUI
		// In LLM auth mode, permission prompts should never be shown in TUI
		log.Warn("LLM auth failed, blocking operation")
		return successResponse(req, &daemon.PermissionResponse{
			Behavior: "deny",
			Message:  "LLM authorization failed - operation blocked",
		})
	}

	// Create the permission request for TUI
	permissionReq := &daemon.PermissionRequest{
		AgentID:     permReq.AgentID,
		Project:     projectName,
		ToolName:    permReq.ToolName,
		ToolInput:   permReq.ToolInput,
		ToolUseID:   permReq.ToolUseID,
		RequestedAt: time.Now(),
	}

	// Add to the permission manager and get the response channel
	id, respCh := s.permissions.Add(permissionReq)
	permissionReq.ID = id

	// Broadcast the permission request to attached TUI clients
	s.broadcastPermissionRequest(permissionReq)

	// Block waiting for a response from the TUI
	resp := <-respCh
	if resp == nil {
		log.Warn("permission request timed out",
			"id", id,
			"tool", permReq.ToolName,
		)
		// Channel was closed without a response (timeout or cancellation)
		return errorResponse(req, "permission request cancelled or timed out")
	}

	log.Info("permission response sent",
		"id", id,
		"tool", permReq.ToolName,
		"input", logging.TruncateForLog(string(permReq.ToolInput), 200),
		"behavior", resp.Behavior,
		"message", logging.TruncateForLog(resp.Message, 200),
	)

	return successResponse(req, resp)
}

// handleLLMAuth uses the LLM to authorize a permission request.
// Returns the response if successful, nil if authorization failed and should fall back to TUI.
func (s *Supervisor) handleLLMAuth(ctx context.Context, permReq daemon.PermissionRequestPayload, projectName, agentTask string, conversationCtx []string, log *slog.Logger) *daemon.PermissionResponse {
	// Get the API key for the configured provider
	provider := s.globalConfig.GetLLMAuthProvider()
	apiKey := s.globalConfig.GetAPIKey(provider)

	// Also check environment variables as fallback
	if apiKey == "" {
		switch provider {
		case "anthropic":
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		case "openai":
			apiKey = os.Getenv("OPENAI_API_KEY")
		}
	}

	if apiKey == "" {
		log.Warn("LLM auth enabled but no API key configured", "provider", provider)
		return nil
	}

	// Create the authorizer
	auth := llmauth.New(llmauth.Config{
		Provider: llmauth.Provider(provider),
		Model:    s.globalConfig.GetLLMAuthModel(),
		APIKey:   apiKey,
	})

	// Build the authorization request
	authReq := llmauth.Request{
		ToolName:        permReq.ToolName,
		ToolInput:       string(permReq.ToolInput),
		AgentTask:       agentTask,
		ConversationCtx: conversationCtx,
	}

	// Call the LLM
	result, err := auth.Authorize(ctx, authReq)
	if err != nil {
		log.Error("LLM authorization failed",
			"error", err,
			"tool", permReq.ToolName,
		)
		return nil
	}

	// Log the decision (excluding conversation history for brevity)
	log.Info("LLM permission decision",
		"tool", permReq.ToolName,
		"input", truncate(string(permReq.ToolInput), 200),
		"decision", result.Decision,
		"rationale", result.Rationale,
	)

	// Convert decision to response
	switch result.Decision {
	case llmauth.DecisionSafe:
		return &daemon.PermissionResponse{
			Behavior: "allow",
		}
	case llmauth.DecisionUnsafe:
		return &daemon.PermissionResponse{
			Behavior: "deny",
			Message:  "Blocked by LLM authorization: operation deemed unsafe",
		}
	case llmauth.DecisionUnsure:
		// For unsure decisions, we block (fail-safe)
		return &daemon.PermissionResponse{
			Behavior: "deny",
			Message:  "Blocked by LLM authorization: unable to determine safety",
		}
	default:
		return nil
	}
}

// handlePermissionRespond handles a permission response from the TUI.
func (s *Supervisor) handlePermissionRespond(_ context.Context, req *daemon.Request) *daemon.Response {
	var respPayload daemon.PermissionRespondPayload
	if err := unmarshalPayload(req.Payload, &respPayload); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if respPayload.ID == "" {
		return errorResponse(req, "permission request ID required")
	}

	// Get the original request for logging
	origReq := s.permissions.Get(respPayload.ID)
	if origReq != nil {
		slog.Info("permission response from TUI",
			"id", respPayload.ID,
			"agent", origReq.AgentID,
			"tool", origReq.ToolName,
			"input", logging.TruncateForLog(string(origReq.ToolInput), 200),
			"behavior", respPayload.Behavior,
			"message", logging.TruncateForLog(respPayload.Message, 200),
		)
	} else {
		slog.Info("permission response from TUI",
			"id", respPayload.ID,
			"behavior", respPayload.Behavior,
			"message", logging.TruncateForLog(respPayload.Message, 200),
		)
	}

	resp := &daemon.PermissionResponse{
		ID:        respPayload.ID,
		Behavior:  respPayload.Behavior,
		Message:   respPayload.Message,
		Interrupt: respPayload.Interrupt,
	}

	if err := s.permissions.Respond(respPayload.ID, resp); err != nil {
		return errorResponse(req, fmt.Sprintf("failed to respond: %v", err))
	}

	return successResponse(req, nil)
}

// handlePermissionList returns pending permission requests.
func (s *Supervisor) handlePermissionList(_ context.Context, req *daemon.Request) *daemon.Response {
	var listReq daemon.PermissionListRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &listReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
		}
	}

	var requests []*daemon.PermissionRequest
	if listReq.Project != "" {
		requests = s.permissions.ListForProject(listReq.Project)
	} else {
		requests = s.permissions.List()
	}

	// Convert to slice of values for response
	result := make([]daemon.PermissionRequest, len(requests))
	for i, r := range requests {
		result[i] = *r
	}

	return successResponse(req, daemon.PermissionListResponse{
		Requests: result,
	})
}
