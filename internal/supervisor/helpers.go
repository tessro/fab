package supervisor

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/tessro/fab/internal/daemon"
)

// successResponse creates a successful response.
func successResponse(req *daemon.Request, payload any) *daemon.Response {
	return &daemon.Response{
		Type:    req.Type,
		ID:      req.ID,
		Success: true,
		Payload: payload,
	}
}

// errorResponse creates an error response.
func errorResponse(req *daemon.Request, msg string) *daemon.Response {
	return &daemon.Response{
		Type:    req.Type,
		ID:      req.ID,
		Success: false,
		Error:   msg,
	}
}

// unmarshalPayload converts an any payload to a specific type.
func unmarshalPayload(payload any, dst any) error {
	if payload == nil {
		return nil
	}

	// If payload is already the right type, use it directly
	if m, ok := payload.(map[string]any); ok {
		// Re-marshal and unmarshal to convert
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		return json.Unmarshal(data, dst)
	}

	// Try direct type assertion
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}

// truncate shortens a string to maxLen characters, adding "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// formatDuration formats a duration as a human-readable string (e.g., "2h 15m").
func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}
