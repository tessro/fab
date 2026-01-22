package tui

import (
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/tessro/fab/internal/daemon"
	"github.com/tessro/fab/internal/usage"
)

// tickCmd returns a command that sends a tick message after a delay.
func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

// EventStreamer is the interface for streaming events from the daemon.
type EventStreamer interface {
	StreamEvents(projects []string) (<-chan daemon.EventResult, error)
}

// attachToStreamCmd returns a command that connects to the daemon event stream.
// This is a shared helper used by multiple TUI models.
func attachToStreamCmd(client EventStreamer) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return nil
		}
		eventChan, err := client.StreamEvents(nil)
		if err != nil {
			return streamEventMsg{Err: err}
		}
		return streamStartMsg{EventChan: eventChan}
	}
}

// waitForEventCmd returns a command that waits for the next event from a channel.
// This is a shared helper used by multiple TUI models.
func waitForEventCmd(eventChan <-chan daemon.EventResult) tea.Cmd {
	if eventChan == nil {
		return nil
	}
	return func() tea.Msg {
		result, ok := <-eventChan
		if !ok {
			return streamEventMsg{Err: fmt.Errorf("event stream closed")}
		}
		return streamEventMsg{Event: result.Event, Err: result.Err}
	}
}

// clearErrorCmd returns a command that clears the error after a delay.
func clearErrorCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return clearErrorMsg{}
	})
}

// setError sets an error to display and returns a command to clear it after a timeout.
func (m *Model) setError(err error) tea.Cmd {
	m.err = err
	m.helpBar.SetError(err.Error())
	return clearErrorCmd()
}

// attachToStream connects to the daemon event stream using a dedicated connection.
func (m Model) attachToStream() tea.Cmd {
	return attachToStreamCmd(m.client)
}

// attemptReconnect tries to reconnect to the daemon after a delay.
func (m Model) attemptReconnect() tea.Cmd {
	delay := m.reconnectDelay
	return func() tea.Msg {
		// Wait before attempting reconnection
		time.Sleep(delay)

		// Try to reconnect the main connection first
		if !m.client.IsConnected() {
			if err := m.client.Connect(); err != nil {
				return reconnectMsg{Success: false, Err: err}
			}
		}

		// Try to establish the event stream
		eventChan, err := m.client.StreamEvents(nil)
		if err != nil {
			return reconnectMsg{Success: false, Err: err}
		}

		return reconnectMsg{Success: true, EventChan: eventChan}
	}
}

// waitForEvent waits for the next event from the channel.
func (m Model) waitForEvent() tea.Cmd {
	return waitForEventCmd(m.eventChan)
}

// fetchAgentList retrieves the current agent list (including planners).
func (m Model) fetchAgentList() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			slog.Debug("tui.fetchAgentList: client is nil")
			return nil
		}
		slog.Debug("tui.fetchAgentList: fetching agents")
		resp, err := m.client.AgentList("")
		if err != nil {
			slog.Error("tui.fetchAgentList: AgentList failed", "error", err)
			return agentListMsg{Err: err}
		}
		slog.Debug("tui.fetchAgentList: got agents", "count", len(resp.Agents))

		// Also fetch planners and merge them into the list
		agents := resp.Agents
		plannerResp, err := m.client.PlanList("")
		if err == nil && plannerResp != nil {
			slog.Debug("tui.fetchAgentList: got planners", "count", len(plannerResp.Planners))
			for _, p := range plannerResp.Planners {
				startedAt := time.Now()
				if p.StartedAt != "" {
					if t, err := time.Parse(time.RFC3339, p.StartedAt); err == nil {
						startedAt = t
					}
				}
				agentID := plannerAgentID(p.ID)
				slog.Debug("tui.fetchAgentList: adding planner to list", "planner_id", p.ID, "agent_id", agentID)
				backend := p.Backend
				if backend == "" {
					backend = "claude" // Default if not set
				}
				agents = append(agents, daemon.AgentStatus{
					ID:          agentID,
					Project:     p.Project,
					State:       p.State,
					Worktree:    p.WorkDir,
					StartedAt:   startedAt,
					Description: "Planner",
					Backend:     backend,
				})
			}
		} else if err != nil {
			slog.Warn("tui.fetchAgentList: PlanList failed", "error", err)
		}

		slog.Debug("tui.fetchAgentList: returning", "total_agents", len(agents))
		return agentListMsg{Agents: agents}
	}
}

// sendAgentMessage sends a user message to an agent via stream-json.
// project is required when agentID is "manager".
func (m Model) sendAgentMessage(agentID, project, content string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		var err error
		if isManager(agentID) {
			err = m.client.ManagerSendMessage(project, content)
		} else if isPlanner(agentID) {
			err = m.client.PlanSendMessage(extractPlannerID(agentID), content)
		} else {
			err = m.client.AgentSendMessage(agentID, content)
		}
		return agentInputMsg{Err: err}
	}
}

// fetchAgentChatHistory retrieves chat history for an agent (or manager/planner).
// project is required when agentID is "manager".
func (m Model) fetchAgentChatHistory(agentID, project string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return agentChatHistoryMsg{AgentID: agentID, Entries: nil}
		}
		var entries []daemon.ChatEntryDTO
		var err error
		if isManager(agentID) {
			var resp *daemon.ManagerChatHistoryResponse
			resp, err = m.client.ManagerChatHistory(project, 0) // 0 = all entries
			if err == nil {
				entries = resp.Entries
			}
		} else if isPlanner(agentID) {
			var resp *daemon.PlanChatHistoryResponse
			resp, err = m.client.PlanChatHistory(extractPlannerID(agentID), 0)
			if err == nil {
				entries = resp.Entries
			}
		} else {
			var resp *daemon.AgentChatHistoryResponse
			resp, err = m.client.AgentChatHistory(agentID, 0) // 0 = all entries
			if err == nil {
				entries = resp.Entries
			}
		}
		if err != nil {
			return agentChatHistoryMsg{AgentID: agentID, Err: err}
		}
		return agentChatHistoryMsg{AgentID: agentID, Entries: entries}
	}
}

// fetchStats retrieves aggregated session statistics.
func (m Model) fetchStats() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		resp, err := m.client.Stats("")
		if err != nil {
			return statsMsg{Err: err}
		}
		return statsMsg{Stats: resp}
	}
}

// fetchCommits retrieves recent commits for the recent work section.
func (m Model) fetchCommits() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		resp, err := m.client.CommitList("", 20) // Get up to 20 recent commits
		if err != nil {
			slog.Debug("tui.fetchCommits: CommitList failed", "error", err)
			return commitListMsg{Err: err}
		}
		return commitListMsg{Commits: resp.Commits}
	}
}

// fetchProjectsForPlan retrieves the list of projects for plan mode.
func (m Model) fetchProjectsForPlan() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return projectListMsg{Err: fmt.Errorf("not connected")}
		}
		resp, err := m.client.ProjectList()
		if err != nil {
			return projectListMsg{Err: err}
		}
		var projects []string
		for _, p := range resp.Projects {
			projects = append(projects, p.Name)
		}
		// Sort projects alphabetically (case-insensitive)
		slices.SortFunc(projects, func(a, b string) int {
			return strings.Compare(strings.ToLower(a), strings.ToLower(b))
		})
		return projectListMsg{Projects: projects}
	}
}

// startPlanner starts a planner for the given project and prompt.
func (m Model) startPlanner(project, prompt string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return planStartResultMsg{Err: fmt.Errorf("not connected")}
		}
		resp, err := m.client.PlanStart(project, prompt)
		if err != nil {
			return planStartResultMsg{Err: err}
		}
		return planStartResultMsg{PlannerID: resp.ID, Project: resp.Project}
	}
}

// allowPermission approves a permission request.
func (m Model) allowPermission(requestID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.RespondPermission(requestID, "allow", "", false)
		return permissionResultMsg{Err: err}
	}
}

// denyPermission denies a permission request.
func (m Model) denyPermission(requestID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.RespondPermission(requestID, "deny", "denied by user", false)
		return permissionResultMsg{Err: err}
	}
}

// answerUserQuestion responds to a user question with the selected answers.
func (m Model) answerUserQuestion(questionID string, answers map[string]string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.RespondUserQuestion(questionID, answers)
		return userQuestionResultMsg{QuestionID: questionID, Err: err}
	}
}

// abortAgent aborts a running agent, planner, or manager.
// project is required when agentID is "manager".
func (m Model) abortAgent(agentID, project string, force bool) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		var err error
		if isManager(agentID) {
			// Manager uses ManagerStop (graceful only, force is ignored)
			err = m.client.ManagerStop(project)
		} else if isPlanner(agentID) {
			// Planners use PlanStop (graceful only, force is ignored)
			err = m.client.PlanStop(extractPlannerID(agentID))
		} else {
			err = m.client.AgentAbort(agentID, force)
		}
		return abortResultMsg{Err: err}
	}
}

// fetchUsage retrieves current usage statistics.
func (m Model) fetchUsage() tea.Cmd {
	limits := m.usageLimits
	return func() tea.Msg {
		window, err := usage.GetCurrentBillingWindowWithUsage()
		if err != nil {
			return usageUpdateMsg{Err: err}
		}
		return usageUpdateMsg{
			Percent:   window.Usage.PercentInt(limits),
			Remaining: window.TimeRemaining(),
		}
	}
}

// fetchProjectsForSupervisor retrieves the list of projects with their running state.
func (m Model) fetchProjectsForSupervisor() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return supervisorProjectListMsg{Err: fmt.Errorf("not connected")}
		}
		resp, err := m.client.ProjectList()
		if err != nil {
			return supervisorProjectListMsg{Err: err}
		}
		var projects []string
		running := make(map[string]bool)
		for _, p := range resp.Projects {
			projects = append(projects, p.Name)
			running[p.Name] = p.Running
		}
		// Sort projects alphabetically (case-insensitive)
		slices.SortFunc(projects, func(a, b string) int {
			return strings.Compare(strings.ToLower(a), strings.ToLower(b))
		})
		return supervisorProjectListMsg{Projects: projects, Running: running}
	}
}

// startSupervisor starts supervision for the given project.
func (m Model) startSupervisor(project string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return supervisorStartResultMsg{Err: fmt.Errorf("not connected")}
		}
		err := m.client.Start(project, false)
		if err != nil {
			return supervisorStartResultMsg{Err: err}
		}
		return supervisorStartResultMsg{Project: project}
	}
}

// stopSupervisor stops supervision for the given project.
func (m Model) stopSupervisor(project string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return supervisorStopResultMsg{Err: fmt.Errorf("not connected")}
		}
		err := m.client.Stop(project, false)
		if err != nil {
			return supervisorStopResultMsg{Err: err}
		}
		return supervisorStopResultMsg{Project: project}
	}
}
