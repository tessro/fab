package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/tessro/fab/internal/daemon"
)

// isManager returns true if the given agent ID is the manager agent.
func isManager(agentID string) bool {
	return isManagerAgent(agentID)
}

// isPlanner returns true if the given agent ID is a planner agent.
func isPlanner(agentID string) bool {
	return isPlannerAgent(agentID)
}

// countRunning counts agents in running or starting state.
func countRunning(agents []daemon.AgentStatus) int {
	count := 0
	for _, a := range agents {
		if a.State == "running" || a.State == "starting" {
			count++
		}
	}
	return count
}

// pendingPermissionForAgent returns the first pending permission request for the given agent.
func (m *Model) pendingPermissionForAgent(agentID string) *daemon.PermissionRequest {
	if agentID == "" {
		return nil
	}
	for i := range m.pendingPermissions {
		if m.pendingPermissions[i].AgentID == agentID {
			return &m.pendingPermissions[i]
		}
	}
	return nil
}

// pendingUserQuestionForAgent returns the first pending user question for the given agent.
func (m *Model) pendingUserQuestionForAgent(agentID string) *daemon.UserQuestion {
	if agentID == "" {
		return nil
	}
	for i := range m.pendingUserQuestions {
		if m.pendingUserQuestions[i].AgentID == agentID {
			return &m.pendingUserQuestions[i]
		}
	}
	return nil
}

// pendingActionForAgent returns the first pending action for the given agent.
func (m *Model) pendingActionForAgent(agentID string) *daemon.StagedAction {
	if agentID == "" {
		return nil
	}
	for i := range m.stagedActions {
		if m.stagedActions[i].AgentID == agentID {
			return &m.stagedActions[i]
		}
	}
	return nil
}

// updateNeedsAttention rebuilds the map of agents that need user attention.
func (m *Model) updateNeedsAttention() {
	attention := make(map[string]bool)
	for _, perm := range m.pendingPermissions {
		attention[perm.AgentID] = true
	}
	for _, action := range m.stagedActions {
		attention[action.AgentID] = true
	}
	for _, question := range m.pendingUserQuestions {
		attention[question.AgentID] = true
	}
	m.agentList.SetNeedsAttention(attention)
}

// pruneStaleAgentState removes state for agents that no longer exist.
// This is called after fetching a fresh agent list (e.g., after reconnecting)
// to clean up any stale state from agents that were removed while disconnected.
func (m *Model) pruneStaleAgentState() tea.Cmd {
	// Build set of valid agent IDs
	validAgents := make(map[string]bool)
	for _, agent := range m.agentList.Agents() {
		validAgents[agent.ID] = true
	}

	// Prune pending permissions for dead agents
	var validPermissions []daemon.PermissionRequest
	for _, perm := range m.pendingPermissions {
		if validAgents[perm.AgentID] {
			validPermissions = append(validPermissions, perm)
		}
	}
	m.pendingPermissions = validPermissions

	// Prune pending user questions for dead agents
	var validQuestions []daemon.UserQuestion
	for _, q := range m.pendingUserQuestions {
		if validAgents[q.AgentID] {
			validQuestions = append(validQuestions, q)
		}
	}
	m.pendingUserQuestions = validQuestions

	// Check if currently viewed agent still exists
	currentAgentID := m.chatView.AgentID()
	if currentAgentID != "" && !validAgents[currentAgentID] {
		// Current agent no longer exists - clear and re-select
		m.chatView.ClearAgent()
		if len(m.agentList.Agents()) > 0 {
			return m.selectCurrentAgent()
		}
	}

	return nil
}

// selectCurrentAgent updates the chat view with the currently selected agent
// and returns a command to fetch its chat history.
func (m *Model) selectCurrentAgent() tea.Cmd {
	agent := m.agentList.Selected()
	if agent == nil {
		return nil
	}
	m.chatView.SetAgent(agent.ID, agent.Project)
	m.chatView.SetPendingPermission(m.pendingPermissionForAgent(agent.ID))
	m.chatView.SetPendingAction(m.pendingActionForAgent(agent.ID))
	m.chatView.SetPendingUserQuestion(m.pendingUserQuestionForAgent(agent.ID))
	return m.fetchAgentChatHistory(agent.ID, agent.Project)
}

// syncFocusToComponents updates component focus states to match the ModeState focus.
func (m *Model) syncFocusToComponents(focus Focus) {
	m.agentList.SetFocused(focus == FocusAgentList)
	// Chat view should remain visually focused when input line has focus,
	// since the input is part of the chat pane.
	m.chatView.SetFocused(focus == FocusChatView || focus == FocusInputLine)
	m.inputLine.SetFocused(focus == FocusInputLine)

	if focus == FocusInputLine {
		m.chatView.SetInputView(m.inputLine.View(), 1, true)
	}
}

// updateLayout recalculates component dimensions for two-pane layout.
func (m *Model) updateLayout() {
	headerHeight := 1 // Single line header
	statusHeight := 1 // Single line status bar
	contentHeight := m.height - headerHeight - statusHeight - 1
	if contentHeight < 1 {
		contentHeight = 1
	}

	// Split width: 38% agent list, 62% chat view
	listWidth := m.width * 38 / 100
	chatWidth := m.width - listWidth

	m.agentList.SetSize(listWidth, contentHeight)
	m.chatView.SetSize(chatWidth, contentHeight)
	m.helpBar.SetWidth(m.width)

	// Input line sized to fit inside chat pane (no border, just content + padding)
	// Height: 1 line content + 1 line divider = 2 total
	inputLineHeight := 2
	m.inputLine.SetSize(chatWidth-2, 1) // Width accounts for chat pane border only
	m.chatView.SetInputView(m.inputLine.View(), inputLineHeight, m.modeState.IsInputting())
}
