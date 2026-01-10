package supervisor

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/tessro/fab/internal/daemon"
)

// handleAgentClaim handles ticket claim requests from agents.
func (s *Supervisor) handleAgentClaim(_ context.Context, req *daemon.Request) *daemon.Response {
	var claimReq daemon.AgentClaimRequest
	if err := unmarshalPayload(req.Payload, &claimReq); err != nil {
		return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
	}

	if claimReq.AgentID == "" {
		return errorResponse(req, "agent_id is required")
	}
	if claimReq.TicketID == "" {
		return errorResponse(req, "ticket_id is required")
	}

	// Find the agent to get its project
	a, err := s.agents.Get(claimReq.AgentID)
	if err != nil {
		return errorResponse(req, fmt.Sprintf("agent not found: %s", claimReq.AgentID))
	}

	// Get the orchestrator for the agent's project
	orch := s.getOrchestrator(a.Info().Project)
	if orch == nil {
		return errorResponse(req, "orchestrator not running for project")
	}

	// Attempt to claim the ticket
	if err := orch.Claims().Claim(claimReq.TicketID, claimReq.AgentID); err != nil {
		return errorResponse(req, fmt.Sprintf("claim failed: %v", err))
	}

	// Update the agent's task field
	a.SetTask(claimReq.TicketID)

	slog.Info("ticket claimed",
		"ticket", claimReq.TicketID,
		"agent", claimReq.AgentID,
		"project", a.Info().Project,
	)

	return successResponse(req, nil)
}

// handleClaimList returns all active ticket claims.
func (s *Supervisor) handleClaimList(_ context.Context, req *daemon.Request) *daemon.Response {
	var listReq daemon.ClaimListRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &listReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
		}
	}

	var claims []daemon.ClaimInfo

	s.mu.RLock()
	for name, orch := range s.orchestrators {
		if listReq.Project != "" && listReq.Project != name {
			continue
		}

		for ticketID, agentID := range orch.Claims().List() {
			claims = append(claims, daemon.ClaimInfo{
				TicketID: ticketID,
				AgentID:  agentID,
				Project:  name,
			})
		}
	}
	s.mu.RUnlock()

	return successResponse(req, daemon.ClaimListResponse{
		Claims: claims,
	})
}
