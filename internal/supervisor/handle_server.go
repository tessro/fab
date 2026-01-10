package supervisor

import (
	"context"
	"time"

	"github.com/tessro/fab/internal/daemon"
)

// handlePing responds to ping requests.
func (s *Supervisor) handlePing(ctx context.Context, req *daemon.Request) *daemon.Response {
	uptime := time.Since(s.startedAt)
	return successResponse(req, daemon.PingResponse{
		Version:   Version,
		Uptime:    uptime.Round(time.Second).String(),
		StartedAt: s.startedAt,
	})
}

// handleShutdown initiates daemon shutdown.
func (s *Supervisor) handleShutdown(ctx context.Context, req *daemon.Request) *daemon.Response {
	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()

	select {
	case <-s.shutdownCh:
		// Already shutting down
	default:
		close(s.shutdownCh)
	}

	return successResponse(req, nil)
}
