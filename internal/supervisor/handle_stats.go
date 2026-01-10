package supervisor

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/usage"
)

// handleStats returns aggregated session statistics.
func (s *Supervisor) handleStats(_ context.Context, req *daemon.Request) *daemon.Response {
	var statsReq daemon.StatsRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &statsReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
		}
	}

	// Count commits across projects
	commitCount := 0
	s.mu.RLock()
	for name, orch := range s.orchestrators {
		if statsReq.Project != "" && statsReq.Project != name {
			continue
		}
		commitCount += orch.Commits().Len()
	}
	s.mu.RUnlock()

	// Get current billing window usage
	window, err := usage.GetCurrentBillingWindowWithUsage()
	if err != nil {
		slog.Debug("failed to get usage stats", "error", err)
		// Return response with zero usage on error
		return successResponse(req, daemon.StatsResponse{
			CommitCount: commitCount,
			Usage: daemon.UsageStats{
				Plan: "pro",
			},
		})
	}

	// Use Pro limits by default (can be made configurable later)
	limits := usage.DefaultProLimits()
	percent := window.Usage.PercentInt(limits)
	timeLeft := window.TimeRemaining()

	return successResponse(req, daemon.StatsResponse{
		CommitCount: commitCount,
		Usage: daemon.UsageStats{
			OutputTokens: window.Usage.OutputTokens,
			Percent:      percent,
			WindowEnd:    window.End.Format(time.RFC3339),
			TimeLeft:     formatDuration(timeLeft),
			PlanLimit:    limits.OutputTokens,
			Plan:         "pro",
		},
	})
}
