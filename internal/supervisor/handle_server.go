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
	// Parse the shutdown request to get stopHost flag
	var shutdownReq daemon.ShutdownRequest
	if err := unmarshalPayload(req.Payload, &shutdownReq); err != nil {
		// Ignore decode errors - treat as default (preserve host)
		shutdownReq = daemon.ShutdownRequest{}
	}

	s.shutdownMu.Lock()
	defer s.shutdownMu.Unlock()

	// Store the stopHost flag for use during shutdown
	s.stopHost = shutdownReq.StopHost

	select {
	case <-s.shutdownCh:
		// Already shutting down
	default:
		close(s.shutdownCh)
	}

	return successResponse(req, nil)
}
