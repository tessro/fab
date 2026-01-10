package tui

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tessro/fab/internal/daemon"
)

// Update implements tea.Model.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Handle input mode
		if m.modeState.IsInputting() {
			switch {
			case key.Matches(msg, m.keys.Cancel):
				// Clear input and exit input mode, return to chat view
				m.inputLine.Clear()
				_ = m.modeState.ExitInputMode()
				m.syncFocusToComponents(FocusChatView)
				m.chatView.SetInputView(m.inputLine.View(), 1, false)
			case key.Matches(msg, m.keys.Submit):
				// Check if we're answering a user question with freeform "Other" input
				if question := m.pendingUserQuestionForAgent(m.chatView.AgentID()); question != nil {
					input := m.inputLine.Value()
					if input != "" {
						// Get the current question header for the answer
						header, _, _ := m.chatView.GetSelectedAnswer()
						slog.Debug("user question 'Other' answered",
							"question_id", question.ID,
							"header", header,
							"answer", input,
						)
						cmds = append(cmds, m.answerUserQuestion(question.ID, map[string]string{header: input}))
						m.inputLine.AddToHistory(input)
						m.inputLine.Clear()
						m.inputLine.SetPlaceholder("Type a message...")
						// Exit input mode, return to chat view
						_ = m.modeState.ExitInputMode()
						m.syncFocusToComponents(FocusChatView)
						m.chatView.SetInputView(m.inputLine.View(), 1, false)
					}
				} else if m.client != nil && m.chatView.AgentID() != "" {
					// Submit input to agent (normal message flow)
					input := m.inputLine.Value()
					if input != "" {
						// Show user message immediately in chat
						m.chatView.AppendEntry(daemon.ChatEntryDTO{
							Role:      "user",
							Content:   input,
							Timestamp: time.Now().Format(time.RFC3339),
						})
						// Send to agent
						cmds = append(cmds, m.sendAgentMessage(m.chatView.AgentID(), m.chatView.Project(), input))
						m.inputLine.AddToHistory(input)
						m.inputLine.Clear()
						// Exit input mode, return to chat view
						_ = m.modeState.ExitInputMode()
						m.syncFocusToComponents(FocusChatView)
						m.chatView.SetInputView(m.inputLine.View(), 1, false)
					}
				}
			case key.Matches(msg, m.keys.Tab):
				// Exit input mode, return to chat view
				_ = m.modeState.ExitInputMode()
				m.syncFocusToComponents(FocusChatView)
				m.chatView.SetInputView(m.inputLine.View(), 1, false)
			case key.Matches(msg, m.keys.HistoryUp):
				// Navigate to previous (older) history entry
				m.inputLine.HistoryUp()
				m.chatView.SetInputView(m.inputLine.View(), 1, true)
			case key.Matches(msg, m.keys.HistoryDown):
				// Navigate to next (newer) history entry
				m.inputLine.HistoryDown()
				m.chatView.SetInputView(m.inputLine.View(), 1, true)
			default:
				// Pass all other keys to input
				cmd := m.inputLine.Update(msg)
				cmds = append(cmds, cmd)
				m.chatView.SetInputView(m.inputLine.View(), 1, true)
			}
			return m, tea.Batch(cmds...)
		}

		// Handle plan project selection mode
		if m.modeState.IsPlanProjectSelect() {
			switch {
			case key.Matches(msg, m.keys.Cancel):
				// Cancel project selection
				_ = m.modeState.CancelPlanProjectSelect()
				m.chatView.ClearPlanProjectSelection()
			case key.Matches(msg, m.keys.Approve), key.Matches(msg, m.keys.Submit):
				// Select project and enter prompt mode
				project, err := m.modeState.SelectPlanProject()
				if err == nil {
					m.chatView.ClearPlanProjectSelection()
					m.chatView.SetPlanPromptMode(project)
					m.syncFocusToComponents(FocusInputLine)
					m.inputLine.SetPlaceholder("What would you like to plan?")
					m.inputLine.Focus()
					m.chatView.SetInputView(m.inputLine.View(), 1, true)
				}
			case key.Matches(msg, m.keys.Up):
				m.modeState.PlanProjectSelectUp()
				_, projects, idx := m.modeState.SelectedPlanProject()
				m.chatView.SetPlanProjectSelection(projects, idx)
			case key.Matches(msg, m.keys.Down):
				m.modeState.PlanProjectSelectDown()
				_, projects, idx := m.modeState.SelectedPlanProject()
				m.chatView.SetPlanProjectSelection(projects, idx)
			}
			return m, tea.Batch(cmds...)
		}

		// Handle plan prompt mode
		if m.modeState.IsPlanPrompt() {
			switch {
			case key.Matches(msg, m.keys.Cancel):
				// Cancel plan mode
				_ = m.modeState.CancelPlanPromptMode()
				m.inputLine.Clear()
				m.inputLine.SetPlaceholder("Type a message...")
				m.chatView.ClearPlanPromptMode()
				m.syncFocusToComponents(FocusAgentList)
			case key.Matches(msg, m.keys.Submit):
				// Submit plan request
				input := m.inputLine.Value()
				if input != "" {
					project, _ := m.modeState.ExitPlanPromptMode()
					cmds = append(cmds, m.startPlanner(project, input))
					m.inputLine.Clear()
					m.inputLine.SetPlaceholder("Type a message...")
					m.chatView.ClearPlanPromptMode()
					m.syncFocusToComponents(FocusChatView)
				}
			default:
				// Pass all other keys to input
				cmd := m.inputLine.Update(msg)
				cmds = append(cmds, cmd)
				m.chatView.SetInputView(m.inputLine.View(), 1, true)
			}
			return m, tea.Batch(cmds...)
		}

		switch {
		case key.Matches(msg, m.keys.Quit):
			// Close client to unblock any pending RecvEvent() calls
			if m.client != nil {
				m.client.Close()
			}
			return m, tea.Quit

		case key.Matches(msg, m.keys.Tab):
			// Cycle focus: agent list -> chat view -> agent list
			newFocus, _ := m.modeState.CycleFocus()
			m.syncFocusToComponents(newFocus)

		case key.Matches(msg, m.keys.FocusChat):
			// Focus input line (vim-style) - enters input mode
			if m.chatView.AgentID() != "" && m.modeState.IsNormal() {
				_ = m.modeState.EnterInputMode()
				m.syncFocusToComponents(FocusInputLine)
				m.chatView.SetInputView(m.inputLine.View(), 1, true)
			}

		case key.Matches(msg, m.keys.Approve):
			// Handle abort confirmation
			if m.modeState.IsAbortConfirming() {
				agentID, _ := m.modeState.ConfirmAbort()
				slog.Debug("confirming abort", "agent_id", agentID)
				cmds = append(cmds, m.abortAgent(agentID, false))
				m.chatView.SetAbortConfirming(false, "")
			} else {
				// Handle pending items for selected agent
				agentID := m.chatView.AgentID()
				// User questions take priority, then permissions, then actions
				if question := m.pendingUserQuestionForAgent(agentID); question != nil {
					header, label, isOther := m.chatView.GetSelectedAnswer()
					if isOther {
						// "Other" selected - enter input mode for freeform answer
						slog.Debug("user question 'Other' selected, entering input mode",
							"question_id", question.ID,
							"header", header,
						)
						if err := m.modeState.EnterInputMode(); err == nil {
							m.syncFocusToComponents(FocusInputLine)
							m.inputLine.SetPlaceholder("Enter your response...")
							m.inputLine.Focus()
							// Store the question info for when input is submitted
							// We'll handle this in the input submission flow
						}
					} else {
						// Regular option selected - submit answer
						slog.Debug("user question answered",
							"question_id", question.ID,
							"header", header,
							"answer", label,
						)
						cmds = append(cmds, m.answerUserQuestion(question.ID, map[string]string{header: label}))
					}
				} else if perm := m.pendingPermissionForAgent(agentID); perm != nil {
					slog.Debug("approving permission",
						"permission_id", perm.ID,
						"tool", perm.ToolName,
					)
					cmds = append(cmds, m.allowPermission(perm.ID))
				} else if action := m.pendingActionForAgent(agentID); action != nil {
					slog.Debug("approving action",
						"action_id", action.ID,
						"action_agent", action.AgentID,
					)
					cmds = append(cmds, m.approveAction(action.ID))
				}
			}

		case key.Matches(msg, m.keys.Reject):
			// Handle abort cancellation
			if m.modeState.IsAbortConfirming() {
				_ = m.modeState.CancelAbort()
				m.chatView.SetAbortConfirming(false, "")
			} else {
				// Reject pending permission or action for selected agent
				agentID := m.chatView.AgentID()
				// Permissions take priority over actions
				if perm := m.pendingPermissionForAgent(agentID); perm != nil {
					slog.Debug("denying permission",
						"permission_id", perm.ID,
						"tool", perm.ToolName,
					)
					cmds = append(cmds, m.denyPermission(perm.ID))
				} else if action := m.pendingActionForAgent(agentID); action != nil {
					cmds = append(cmds, m.rejectAction(action.ID))
				}
			}

		case key.Matches(msg, m.keys.Abort):
			// Start abort confirmation for selected agent
			agentID := m.chatView.AgentID()
			if agentID != "" && m.modeState.IsNormal() {
				if err := m.modeState.EnterAbortConfirm(agentID); err == nil {
					m.chatView.SetAbortConfirming(true, agentID)
				}
			}

		case key.Matches(msg, m.keys.Reconnect):
			// Manual reconnection when disconnected
			if m.connState == connectionDisconnected && m.client != nil {
				slog.Debug("manual reconnection triggered")
				m.connState = connectionReconnecting
				m.reconnectCount = 0
				m.reconnectDelay = 500 * time.Millisecond
				m.header.SetConnectionState(m.connState)
				cmds = append(cmds, m.attemptReconnect())
			}

		case key.Matches(msg, m.keys.Down):
			switch m.modeState.Focus {
			case FocusAgentList:
				m.agentList.MoveDown()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				// If there's a pending user question, navigate options instead of scrolling
				if m.chatView.HasPendingUserQuestion() {
					m.chatView.QuestionMoveDown()
				} else {
					m.chatView.ScrollDown(1)
				}
			}

		case key.Matches(msg, m.keys.Up):
			switch m.modeState.Focus {
			case FocusAgentList:
				m.agentList.MoveUp()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				// If there's a pending user question, navigate options instead of scrolling
				if m.chatView.HasPendingUserQuestion() {
					m.chatView.QuestionMoveUp()
				} else {
					m.chatView.ScrollUp(1)
				}
			}

		case key.Matches(msg, m.keys.Top):
			switch m.modeState.Focus {
			case FocusAgentList:
				m.agentList.MoveToTop()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				m.chatView.ScrollToTop()
			}

		case key.Matches(msg, m.keys.Bottom):
			switch m.modeState.Focus {
			case FocusAgentList:
				m.agentList.MoveToBottom()
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case FocusChatView:
				m.chatView.ScrollToBottom()
			}

		case key.Matches(msg, m.keys.PageUp):
			if m.modeState.Focus == FocusChatView {
				m.chatView.PageUp()
			}

		case key.Matches(msg, m.keys.PageDown):
			if m.modeState.Focus == FocusChatView {
				m.chatView.PageDown()
			}

		case key.Matches(msg, m.keys.Plan):
			// Start plan mode - fetch projects first
			if m.modeState.IsNormal() {
				cmds = append(cmds, m.fetchProjectsForPlan())
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.header.SetWidth(m.width)
		m.updateLayout()
		m.ready = true

	case streamStartMsg:
		// Event stream connected successfully
		m.eventChan = msg.EventChan
		m.attached = true
		m.connState = connectionConnected
		m.reconnectCount = 0
		m.reconnectDelay = 500 * time.Millisecond
		m.header.SetConnectionState(m.connState)
		cmds = append(cmds, m.waitForEvent())

	case streamEventMsg:
		if msg.Err != nil {
			slog.Debug("stream error, attempting reconnection", "err", msg.Err)
			m.connState = connectionDisconnected
			m.header.SetConnectionState(m.connState)
			m.eventChan = nil
			// Attempt automatic reconnection if under limit
			if m.reconnectCount < m.maxReconnects {
				m.connState = connectionReconnecting
				m.header.SetConnectionState(m.connState)
				cmds = append(cmds, m.attemptReconnect())
			} else {
				cmds = append(cmds, m.setError(fmt.Errorf("connection lost (press 'r' to reconnect)")))
			}
		} else if msg.Event != nil {
			slog.Debug("stream event received", "type", msg.Event.Type)
			if cmd := m.handleStreamEvent(msg.Event); cmd != nil {
				cmds = append(cmds, cmd)
			}
			// Continue listening for events
			cmds = append(cmds, m.waitForEvent())
		}

	case reconnectMsg:
		if msg.Success {
			slog.Debug("reconnection successful")
			m.eventChan = msg.EventChan
			m.attached = true
			m.connState = connectionConnected
			m.reconnectCount = 0
			m.reconnectDelay = 500 * time.Millisecond
			m.header.SetConnectionState(m.connState)
			// Fetch fresh agent list after reconnection
			cmds = append(cmds, m.fetchAgentList())
			cmds = append(cmds, m.waitForEvent())
		} else {
			slog.Debug("reconnection failed", "err", msg.Err, "attempt", m.reconnectCount+1)
			m.reconnectCount++
			// Exponential backoff: 500ms, 1s, 2s, 4s, 8s (capped)
			m.reconnectDelay = min(m.reconnectDelay*2, 8*time.Second)
			if m.reconnectCount < m.maxReconnects {
				cmds = append(cmds, m.attemptReconnect())
			} else {
				m.connState = connectionDisconnected
				m.header.SetConnectionState(m.connState)
				cmds = append(cmds, m.setError(fmt.Errorf("connection lost after %d attempts (press 'r' to reconnect)", m.reconnectCount)))
			}
		}

	case agentListMsg:
		if msg.Err != nil {
			slog.Error("tui.Update: agentListMsg error", "error", msg.Err)
			cmds = append(cmds, m.setError(msg.Err))
		} else {
			slog.Debug("tui.Update: agentListMsg received", "count", len(msg.Agents), "initial_agent_id", m.initialAgentID, "pending_planner_id", m.pendingPlannerID)
			m.agentList.SetAgents(msg.Agents)
			m.header.SetAgentCounts(len(msg.Agents), countRunning(msg.Agents))
			// Prune state for agents that no longer exist (e.g., after reconnecting)
			if cmd := m.pruneStaleAgentState(); cmd != nil {
				cmds = append(cmds, cmd)
			}

			// Check if we have a pending planner to select (from starting plan in TUI)
			if m.pendingPlannerID != "" {
				tuiPlannerID := plannerAgentID(m.pendingPlannerID)
				slog.Debug("tui.Update: looking for pending planner", "pending_planner_id", m.pendingPlannerID, "tui_planner_id", tuiPlannerID)
				found := false
				for i, agent := range msg.Agents {
					if agent.ID == tuiPlannerID {
						slog.Debug("tui.Update: found pending planner, selecting", "index", i, "agent_id", agent.ID)
						m.pendingPlannerID = "" // Clear pending
						m.agentList.SetSelected(i)
						if cmd := m.selectCurrentAgent(); cmd != nil {
							cmds = append(cmds, cmd)
						}
						found = true
						break
					}
				}
				if !found {
					slog.Debug("tui.Update: pending planner not found in agent list", "tui_planner_id", tuiPlannerID)
				}
			} else if m.chatView.AgentID() == "" && len(msg.Agents) > 0 {
				// Auto-select agent if none is currently selected
				// If an initial agent was specified, find and select it
				if m.initialAgentID != "" {
					slog.Debug("tui.Update: looking for initial agent", "initial_agent_id", m.initialAgentID)
					found := false
					for i, agent := range msg.Agents {
						slog.Debug("tui.Update: checking agent", "index", i, "agent_id", agent.ID)
						if agent.ID == m.initialAgentID {
							slog.Debug("tui.Update: found initial agent, selecting", "index", i)
							m.agentList.SetSelected(i)
							found = true
							break
						}
					}
					if !found {
						slog.Warn("tui.Update: initial agent not found in agent list", "initial_agent_id", m.initialAgentID, "agent_count", len(msg.Agents))
					}
					// Clear the initial agent ID so we don't keep trying to select it
					m.initialAgentID = ""
				}
				if cmd := m.selectCurrentAgent(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
			// Attach to event stream after initial agent list fetch
			if !m.attached {
				slog.Debug("tui.Update: attaching to event stream")
				cmds = append(cmds, m.attachToStream())
			}
			// Fetch staged actions for approval display
			cmds = append(cmds, m.fetchStagedActions())
			// Fetch stats for header display
			cmds = append(cmds, m.fetchStats())
		}

	case agentInputMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		}

	case agentChatHistoryMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		} else if msg.AgentID == m.chatView.AgentID() {
			// Only apply if still viewing this agent
			m.chatView.SetEntries(msg.Entries)
		}

	case stagedActionsMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		} else {
			// Store all staged actions - filtering happens in pendingActionForAgent
			m.stagedActions = msg.Actions
			// Update chat view with pending action for current agent
			m.chatView.SetPendingAction(m.pendingActionForAgent(m.chatView.AgentID()))
			// Update attention indicators
			m.updateNeedsAttention()
		}

	case statsMsg:
		if msg.Err == nil && msg.Stats != nil {
			// Only use stats for commit count - usage comes from usageUpdateMsg
			m.header.SetCommitCount(msg.Stats.CommitCount)
		}

	case actionResultMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		} else {
			// Refresh staged actions after approve/reject
			cmds = append(cmds, m.fetchStagedActions())
		}

	case permissionResultMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
			// Check if connection was lost and trigger reconnection
			if m.client != nil && !m.client.IsConnected() {
				slog.Debug("connection lost during permission response, triggering reconnection")
				m.connState = connectionReconnecting
				m.header.SetConnectionState(m.connState)
				cmds = append(cmds, m.attemptReconnect())
			}
		} else {
			// Remove the permission from our pending list
			permID := m.chatView.PendingPermissionID()
			if permID != "" {
				for i := range m.pendingPermissions {
					if m.pendingPermissions[i].ID == permID {
						m.pendingPermissions = append(m.pendingPermissions[:i], m.pendingPermissions[i+1:]...)
						break
					}
				}
			}
		}
		// Clear the chat view's pending permission
		m.chatView.SetPendingPermission(nil)
		// Update attention indicators
		m.updateNeedsAttention()

	case userQuestionResultMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		} else {
			// Remove the question from our pending list
			for i := range m.pendingUserQuestions {
				if m.pendingUserQuestions[i].ID == msg.QuestionID {
					m.pendingUserQuestions = append(m.pendingUserQuestions[:i], m.pendingUserQuestions[i+1:]...)
					break
				}
			}
		}
		// Clear the chat view's pending question
		m.chatView.SetPendingUserQuestion(nil)
		// Update attention indicators
		m.updateNeedsAttention()

	case projectListMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		} else if len(msg.Projects) == 0 {
			cmds = append(cmds, m.setError(fmt.Errorf("no projects configured")))
		} else {
			// Enter plan project selection mode
			if err := m.modeState.EnterPlanProjectSelect(msg.Projects); err != nil {
				cmds = append(cmds, m.setError(err))
			} else {
				// Show project selection in chat view
				m.chatView.SetPlanProjectSelection(msg.Projects, 0)
			}
		}

	case planStartResultMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		} else {
			slog.Info("planner started from TUI",
				"planner", msg.PlannerID,
				"project", msg.Project,
			)
			// Store the planner ID so we can auto-select it when the
			// planner_created event arrives
			m.pendingPlannerID = msg.PlannerID
			// The planner_created event will add it to the agent list
			// We just need to refresh the list to ensure we see it
			cmds = append(cmds, m.fetchAgentList())
		}

	case abortResultMsg:
		if msg.Err != nil {
			cmds = append(cmds, m.setError(msg.Err))
		}
		// Clear abort confirmation state (in case of error)
		if m.modeState.IsAbortConfirming() {
			_ = m.modeState.CancelAbort()
			m.chatView.SetAbortConfirming(false, "")
		}

	case tickMsg:
		// Advance spinner frame and schedule next tick
		m.spinnerFrame++
		m.agentList.SetSpinnerFrame(m.spinnerFrame)
		cmds = append(cmds, m.tickCmd())

		// Periodically refresh usage stats (every 30 seconds)
		if time.Since(m.lastUsageFetch) > 30*time.Second {
			m.lastUsageFetch = time.Now()
			cmds = append(cmds, m.fetchUsage())
		}

		// Refresh daemon stats every 30 seconds for commit count
		m.statsRefreshTick++
		if m.statsRefreshTick >= 300 && m.connState == connectionConnected {
			m.statsRefreshTick = 0
			cmds = append(cmds, m.fetchStats())
		}

	case usageUpdateMsg:
		if msg.Err != nil {
			// Silently ignore usage fetch errors - not critical
			slog.Debug("usage fetch error", "err", msg.Err)
		} else {
			m.header.SetUsage(msg.Percent, msg.Remaining)
			m.lastUsageFetch = time.Now()
		}

	case clearErrorMsg:
		// Clear error display
		m.err = nil
		m.helpBar.ClearError()
	}

	return m, tea.Batch(cmds...)
}

// handleStreamEvent processes a stream event from the daemon.
// Returns a command to execute if needed (e.g., fetching chat history for newly selected agent).
func (m *Model) handleStreamEvent(event *daemon.StreamEvent) tea.Cmd {
	switch event.Type {
	case "chat_entry":
		// Handle chat entry events from stream-json parsing
		slog.Debug("chat_entry event received",
			"event_agent", event.AgentID,
			"chatview_agent", m.chatView.AgentID(),
			"match", event.AgentID == m.chatView.AgentID(),
			"has_entry", event.ChatEntry != nil,
		)
		if event.ChatEntry != nil && event.AgentID == m.chatView.AgentID() {
			m.chatView.AppendEntry(*event.ChatEntry)
		}

	case "output":
		// Deprecated: kept for backwards compatibility with raw output
		// This is no longer used by the chat view

	case "state":
		// Update agent state in the list
		agents := m.agentList.Agents()
		for i := range agents {
			if agents[i].ID == event.AgentID {
				agents[i].State = event.State
				m.agentList.SetAgents(agents)
				break
			}
		}
		m.header.SetAgentCounts(len(agents), countRunning(agents))

	case "info":
		// Update agent task/description in the list
		agents := m.agentList.Agents()
		for i := range agents {
			if agents[i].ID == event.AgentID {
				agents[i].Task = event.Task
				agents[i].Description = event.Description
				m.agentList.SetAgents(agents)
				break
			}
		}

	case "created":
		// A new agent was created - add to list with proper StartedAt
		agents := m.agentList.Agents()
		startedAt := time.Now() // fallback
		if event.StartedAt != "" {
			if t, err := time.Parse(time.RFC3339, event.StartedAt); err == nil {
				startedAt = t
			}
		}
		agents = append(agents, daemon.AgentStatus{
			ID:        event.AgentID,
			Project:   event.Project,
			State:     "starting",
			StartedAt: startedAt,
		})
		m.agentList.SetAgents(agents)
		m.header.SetAgentCounts(len(agents), countRunning(agents))
		// Auto-select the new agent if no agent is currently selected
		if m.chatView.AgentID() == "" {
			return m.selectCurrentAgent()
		}

	case "deleted":
		// An agent was deleted - remove from list
		wasSelected := event.AgentID == m.chatView.AgentID()
		agents := m.agentList.Agents()
		for i := range agents {
			if agents[i].ID == event.AgentID {
				agents = append(agents[:i], agents[i+1:]...)
				break
			}
		}
		m.agentList.SetAgents(agents)
		m.header.SetAgentCounts(len(agents), countRunning(agents))
		// If the deleted agent was selected, auto-select the next agent
		if wasSelected {
			m.chatView.ClearAgent()
			if len(agents) > 0 {
				return m.selectCurrentAgent()
			}
		}

	case "permission_request":
		// A new permission request arrived
		if event.PermissionRequest != nil {
			slog.Debug("permission_request event",
				"agent", event.AgentID,
				"tool", event.PermissionRequest.ToolName,
			)
			// Add to our list of pending permissions
			m.pendingPermissions = append(m.pendingPermissions, *event.PermissionRequest)
			// Update chat view if this is for the current agent
			if event.AgentID == m.chatView.AgentID() {
				m.chatView.SetPendingPermission(m.pendingPermissionForAgent(event.AgentID))
			}
			// Update attention indicators
			m.updateNeedsAttention()
		}

	case "user_question":
		// A new user question arrived (from AskUserQuestion tool)
		if event.UserQuestion != nil {
			slog.Debug("user_question event",
				"agent", event.AgentID,
				"question_count", len(event.UserQuestion.Questions),
			)
			// Add to our list of pending user questions
			m.pendingUserQuestions = append(m.pendingUserQuestions, *event.UserQuestion)
			// Update chat view if this is for the current agent
			if event.AgentID == m.chatView.AgentID() {
				m.chatView.SetPendingUserQuestion(m.pendingUserQuestionForAgent(event.AgentID))
			}
			// Update attention indicators
			m.updateNeedsAttention()
		}

	case "action_queued":
		// A new staged action was queued
		if event.StagedAction != nil {
			slog.Debug("action_queued event",
				"agent", event.AgentID,
				"action_id", event.StagedAction.ID,
				"type", event.StagedAction.Type,
			)
			// Add to our list of staged actions
			m.stagedActions = append(m.stagedActions, *event.StagedAction)
			// Update chat view if this is for the current agent
			if event.AgentID == m.chatView.AgentID() {
				m.chatView.SetPendingAction(m.pendingActionForAgent(event.AgentID))
			}
			// Update attention indicators
			m.updateNeedsAttention()
		}

	case "manager_chat_entry":
		// Manager agent chat entry - display if manager is selected
		if event.ChatEntry != nil && m.chatView.AgentID() == ManagerAgentID {
			m.chatView.AppendEntry(*event.ChatEntry)
		}

	case "manager_state":
		// Manager agent state changed - add/remove/update in the agent list
		agents := m.agentList.Agents()

		// Find manager in the current list
		managerIndex := -1
		for i := range agents {
			if agents[i].ID == ManagerAgentID {
				managerIndex = i
				break
			}
		}

		switch event.ManagerState {
		case "stopped":
			// Remove manager from the list when stopped
			if managerIndex >= 0 {
				wasSelected := m.chatView.AgentID() == ManagerAgentID
				agents = append(agents[:managerIndex], agents[managerIndex+1:]...)
				m.agentList.SetAgents(agents)
				// If manager was selected, select the next agent
				if wasSelected {
					m.chatView.ClearAgent()
					if len(agents) > 0 {
						return m.selectCurrentAgent()
					}
				}
			}
		case "starting", "running":
			// Add manager to the list if not present
			if managerIndex < 0 {
				startedAt := time.Now() // fallback
				if event.StartedAt != "" {
					if t, err := time.Parse(time.RFC3339, event.StartedAt); err == nil {
						startedAt = t
					}
				}
				// Prepend manager as first entry
				managerAgent := daemon.AgentStatus{
					ID:          ManagerAgentID,
					Project:     "manager",
					State:       event.ManagerState,
					StartedAt:   startedAt,
					Description: "Manager",
				}
				agents = append([]daemon.AgentStatus{managerAgent}, agents...)
				m.agentList.SetAgents(agents)
			} else {
				// Update existing manager state
				agents[managerIndex].State = event.ManagerState
				m.agentList.SetAgents(agents)
			}
		default:
			// Update state for other transitions (stopping, etc.)
			if managerIndex >= 0 {
				agents[managerIndex].State = event.ManagerState
				m.agentList.SetAgents(agents)
			}
		}
		m.header.SetAgentCounts(len(agents), countRunning(agents))

	case "planner_created":
		// A new planner was created - add to list
		agents := m.agentList.Agents()
		startedAt := time.Now()
		if event.StartedAt != "" {
			if t, err := time.Parse(time.RFC3339, event.StartedAt); err == nil {
				startedAt = t
			}
		}
		tuiAgentID := plannerAgentID(event.AgentID)
		agents = append(agents, daemon.AgentStatus{
			ID:          tuiAgentID,
			Project:     event.Project,
			State:       "starting",
			StartedAt:   startedAt,
			Description: "Planner",
		})
		m.agentList.SetAgents(agents)
		m.header.SetAgentCounts(len(agents), countRunning(agents))

		// Check if this is the planner we just started from TUI
		shouldSelect := m.pendingPlannerID == event.AgentID
		if shouldSelect {
			m.pendingPlannerID = "" // Clear pending
			// Select the new planner in the list
			for i, agent := range agents {
				if agent.ID == tuiAgentID {
					m.agentList.SetSelected(i)
					break
				}
			}
			return m.selectCurrentAgent()
		}

		// Auto-select the new planner if no agent is currently selected
		if m.chatView.AgentID() == "" {
			return m.selectCurrentAgent()
		}

	case "planner_state":
		// Update planner state in the list
		tuiAgentID := plannerAgentID(event.AgentID)
		agents := m.agentList.Agents()
		for i := range agents {
			if agents[i].ID == tuiAgentID {
				agents[i].State = event.State
				m.agentList.SetAgents(agents)
				break
			}
		}
		m.header.SetAgentCounts(len(agents), countRunning(agents))

	case "planner_deleted":
		// A planner was deleted - remove from list
		tuiAgentID := plannerAgentID(event.AgentID)
		wasSelected := tuiAgentID == m.chatView.AgentID()
		agents := m.agentList.Agents()
		for i := range agents {
			if agents[i].ID == tuiAgentID {
				agents = append(agents[:i], agents[i+1:]...)
				break
			}
		}
		m.agentList.SetAgents(agents)
		m.header.SetAgentCounts(len(agents), countRunning(agents))
		// If the deleted planner was selected, auto-select the next agent
		if wasSelected {
			m.chatView.ClearAgent()
			if len(agents) > 0 {
				return m.selectCurrentAgent()
			}
		}

	case "planner_chat_entry":
		// Handle chat entry events from planner
		tuiAgentID := plannerAgentID(event.AgentID)
		if event.ChatEntry != nil && tuiAgentID == m.chatView.AgentID() {
			m.chatView.AppendEntry(*event.ChatEntry)
		}

	case "plan_complete":
		// Could show a completion notification
		// For now, just log it (the planner will call fab agent done)
		slog.Debug("plan completed",
			"planner", event.AgentID,
			"project", event.Project,
			"plan_file", event.Data,
		)
	}
	return nil
}
