package supervisor

import (
	"context"
	"fmt"
	"time"

	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/orchestrator"
)

// handleCommitList returns recent commits across projects.
func (s *Supervisor) handleCommitList(_ context.Context, req *daemon.Request) *daemon.Response {
	var listReq daemon.CommitListRequest
	if req.Payload != nil {
		if err := unmarshalPayload(req.Payload, &listReq); err != nil {
			return errorResponse(req, fmt.Sprintf("invalid payload: %v", err))
		}
	}

	var commits []daemon.CommitInfo

	s.mu.RLock()
	for name, orch := range s.orchestrators {
		if listReq.Project != "" && listReq.Project != name {
			continue
		}

		var records []orchestrator.CommitRecord
		if listReq.Limit > 0 {
			records = orch.Commits().ListRecent(listReq.Limit)
		} else {
			records = orch.Commits().List()
		}

		for _, r := range records {
			commits = append(commits, daemon.CommitInfo{
				SHA:      r.SHA,
				Branch:   r.Branch,
				AgentID:  r.AgentID,
				TaskID:   r.TaskID,
				Project:  name,
				MergedAt: r.MergedAt.Format(time.RFC3339),
			})
		}
	}
	s.mu.RUnlock()

	return successResponse(req, daemon.CommitListResponse{
		Commits: commits,
	})
}
