package agenthost

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/tessro/fab/internal/paths"
)

// DiscoveredHost contains information about a discovered agent host.
type DiscoveredHost struct {
	AgentID    string         // Agent ID extracted from socket filename
	SocketPath string         // Full path to the socket file
	Status     *StatusResponse // Status from the host (nil if probe failed)
}

// DiscoverActiveHosts scans the hosts directory for socket files and probes each one.
// Returns a list of hosts that successfully responded to a status request.
func DiscoverActiveHosts() ([]DiscoveredHost, error) {
	hostsDir, err := paths.AgentHostsDir()
	if err != nil {
		return nil, err
	}

	// List socket files in hosts directory
	entries, err := os.ReadDir(hostsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No hosts directory means no hosts
			return nil, nil
		}
		return nil, err
	}

	var hosts []DiscoveredHost

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasSuffix(name, ".sock") {
			continue
		}

		// Extract agent ID from filename (remove .sock suffix)
		agentID := strings.TrimSuffix(name, ".sock")
		socketPath := filepath.Join(hostsDir, name)

		// Probe the host to see if it's alive
		client := NewClientWithSocket(agentID, socketPath)
		if err := client.Connect(); err != nil {
			slog.Debug("host socket exists but connect failed",
				"agent_id", agentID,
				"error", err,
			)
			// Socket file exists but not responding - clean up stale socket
			_ = os.Remove(socketPath)
			continue
		}

		status, err := client.Status()
		client.Close()

		if err != nil {
			slog.Debug("host connected but status failed",
				"agent_id", agentID,
				"error", err,
			)
			continue
		}

		hosts = append(hosts, DiscoveredHost{
			AgentID:    agentID,
			SocketPath: socketPath,
			Status:     status,
		})

		slog.Debug("discovered active host",
			"agent_id", agentID,
			"state", status.Agent.State,
			"project", status.Agent.Project,
		)
	}

	return hosts, nil
}

// DiscoverHostsForProject returns active hosts that belong to a specific project.
func DiscoverHostsForProject(projectName string) ([]DiscoveredHost, error) {
	allHosts, err := DiscoverActiveHosts()
	if err != nil {
		return nil, err
	}

	var projectHosts []DiscoveredHost
	for _, host := range allHosts {
		if host.Status != nil && host.Status.Agent.Project == projectName {
			projectHosts = append(projectHosts, host)
		}
	}

	return projectHosts, nil
}
