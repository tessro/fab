package tui

import (
	"fmt"
	"log/slog"
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
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		eventChan, err := m.client.StreamEvents(nil)
		if err != nil {
			return StreamEventMsg{Err: err}
		}
		return StreamStartMsg{EventChan: eventChan}
	}
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
	if m.eventChan == nil {
		return nil
	}
	ch := m.eventChan
	return func() tea.Msg {
		result, ok := <-ch
		if !ok {
			// Channel closed
			return StreamEventMsg{Err: fmt.Errorf("event stream closed")}
		}
		return StreamEventMsg{Event: result.Event, Err: result.Err}
	}
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
			return AgentListMsg{Err: err}
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
				agents = append(agents, daemon.AgentStatus{
					ID:          agentID,
					Project:     p.Project,
					State:       p.State,
					Worktree:    p.WorkDir,
					StartedAt:   startedAt,
					Description: "Planner",
				})
			}
		} else if err != nil {
			slog.Warn("tui.fetchAgentList: PlanList failed", "error", err)
		}

		slog.Debug("tui.fetchAgentList: returning", "total_agents", len(agents))
		return AgentListMsg{Agents: agents}
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
		return AgentInputMsg{Err: err}
	}
}

// fetchAgentChatHistory retrieves chat history for an agent (or manager/planner).
// project is required when agentID is "manager".
func (m Model) fetchAgentChatHistory(agentID, project string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return AgentChatHistoryMsg{AgentID: agentID, Entries: nil}
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
			return AgentChatHistoryMsg{AgentID: agentID, Err: err}
		}
		return AgentChatHistoryMsg{AgentID: agentID, Entries: entries}
	}
}

// fetchStagedActions retrieves pending actions for user approval.
func (m Model) fetchStagedActions() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		resp, err := m.client.ListStagedActions("")
		if err != nil {
			return StagedActionsMsg{Err: err}
		}
		return StagedActionsMsg{Actions: resp.Actions}
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
			return StatsMsg{Err: err}
		}
		return StatsMsg{Stats: resp}
	}
}

// fetchProjectsForPlan retrieves the list of projects for plan mode.
func (m Model) fetchProjectsForPlan() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return ProjectListMsg{Err: fmt.Errorf("not connected")}
		}
		resp, err := m.client.ProjectList()
		if err != nil {
			return ProjectListMsg{Err: err}
		}
		var projects []string
		for _, p := range resp.Projects {
			projects = append(projects, p.Name)
		}
		return ProjectListMsg{Projects: projects}
	}
}

// startPlanner starts a planner for the given project and prompt.
func (m Model) startPlanner(project, prompt string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return PlanStartResultMsg{Err: fmt.Errorf("not connected")}
		}
		resp, err := m.client.PlanStart(project, prompt)
		if err != nil {
			return PlanStartResultMsg{Err: err}
		}
		return PlanStartResultMsg{PlannerID: resp.ID, Project: resp.Project}
	}
}

// approveAction approves a staged action.
func (m Model) approveAction(actionID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.ApproveAction(actionID)
		return ActionResultMsg{Err: err}
	}
}

// rejectAction rejects a staged action.
func (m Model) rejectAction(actionID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.RejectAction(actionID, "")
		return ActionResultMsg{Err: err}
	}
}

// allowPermission approves a permission request.
func (m Model) allowPermission(requestID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.RespondPermission(requestID, "allow", "", false)
		return PermissionResultMsg{Err: err}
	}
}

// denyPermission denies a permission request.
func (m Model) denyPermission(requestID string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.RespondPermission(requestID, "deny", "denied by user", false)
		return PermissionResultMsg{Err: err}
	}
}

// answerUserQuestion responds to a user question with the selected answers.
func (m Model) answerUserQuestion(questionID string, answers map[string]string) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.RespondUserQuestion(questionID, answers)
		return UserQuestionResultMsg{QuestionID: questionID, Err: err}
	}
}

// abortAgent aborts a running agent.
func (m Model) abortAgent(agentID string, force bool) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return nil
		}
		err := m.client.AgentAbort(agentID, force)
		return AbortResultMsg{Err: err}
	}
}

// fetchUsage retrieves current usage statistics.
func (m Model) fetchUsage() tea.Cmd {
	limits := m.usageLimits
	return func() tea.Msg {
		window, err := usage.GetCurrentBillingWindowWithUsage()
		if err != nil {
			return UsageUpdateMsg{Err: err}
		}
		return UsageUpdateMsg{
			Percent:   window.Usage.PercentInt(limits),
			Remaining: window.TimeRemaining(),
		}
	}
}
