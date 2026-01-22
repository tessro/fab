// Package tui provides the Bubbletea-based terminal user interface for fab.
package tui

import (
	"fmt"
	"log/slog"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/tessro/fab/internal/daemon"
)

// Focus indicates which panel is currently focused.
type Focus int

const (
	FocusAgentList Focus = iota
	FocusChatView
	FocusInputLine
)

// connectionState represents the current IPC connection status.
type connectionState int

const (
	// connectionConnected means the TUI is connected to the daemon.
	connectionConnected connectionState = iota
	// connectionDisconnected means the connection was lost.
	connectionDisconnected
	// connectionReconnecting means a reconnection attempt is in progress.
	connectionReconnecting
)

// Model is the main Bubbletea model for the fab TUI.
type Model struct {
	// Window dimensions
	width  int
	height int

	// UI state
	ready bool
	err   error

	// Mode state (centralized focus and mode management)
	modeState ModeState

	// Components
	header    Header
	agentList AgentList
	chatView  ChatView
	inputLine InputLine
	helpBar   HelpBar

	// Daemon client for IPC
	client   daemon.TUIClient
	attached bool

	// Event channel from dedicated streaming connection
	eventChan <-chan daemon.EventResult

	// Connection state tracking
	connState      connectionState
	reconnectDelay time.Duration
	reconnectCount int
	maxReconnects  int

	// Pending permission requests (for selected agent)
	pendingPermissions []daemon.PermissionRequest

	// Pending user questions (for selected agent)
	pendingUserQuestions []daemon.UserQuestion

	// Spinner animation frame counter
	spinnerFrame int

	// Key bindings
	keys KeyBindings

	// Initial agent to select on startup (empty = first agent)
	initialAgentID string

	// Pending planner ID to select when it appears in the list
	// Set when user starts a plan from TUI, cleared when selected
	pendingPlannerID string
}

// New creates a new TUI model.
func New() Model {
	agentList := NewAgentList()
	agentList.SetFocused(true) // Agent list is focused by default

	return Model{
		header:         NewHeader(),
		agentList:      agentList,
		chatView:       NewChatView(),
		inputLine:      NewInputLine(),
		helpBar:        NewHelpBar(),
		modeState:      NewModeState(),
		keys:           DefaultKeyBindings(),
		connState:      connectionConnected,
		reconnectDelay: 500 * time.Millisecond,
		maxReconnects:  10,
	}
}

// TUIOptions configures the TUI behavior.
type TUIOptions struct {
	// InitialAgentID specifies an agent to select on startup.
	// If empty, the first agent in the list will be selected.
	InitialAgentID string
}

// NewWithClient creates a new TUI model with a pre-connected daemon client.
func NewWithClient(client daemon.TUIClient, opts *TUIOptions) Model {
	m := New()
	m.client = client
	if opts != nil {
		m.initialAgentID = opts.InitialAgentID
	}
	return m
}

// Init implements tea.Model.
func (m Model) Init() tea.Cmd {
	slog.Debug("tui.Init: starting", "has_client", m.client != nil, "initial_agent_id", m.initialAgentID)
	cmds := []tea.Cmd{
		m.inputLine.input.Cursor.BlinkCmd(),
		m.tickCmd(), // Start spinner animation
	}
	if m.client != nil {
		// Fetch agent list first, then attach to stream
		// (must be sequential to avoid concurrent decoder access)
		slog.Debug("tui.Init: scheduling fetchAgentList")
		cmds = append(cmds, m.fetchAgentList())
	}
	return tea.Batch(cmds...)
}

// View implements tea.Model.
func (m Model) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Header
	header := m.header.View()

	// Update help bar mode state
	m.modeState.SetPendingApprovals(
		m.pendingPermissionForAgent(m.chatView.AgentID()) != nil,
		false, // no more staged actions (removed manual mode)
		m.pendingUserQuestionForAgent(m.chatView.AgentID()) != nil,
	)
	m.helpBar.SetModeState(m.modeState)
	status := m.helpBar.View()

	// Left pane: agent list
	agentList := m.agentList.View()

	// Right pane: chat view
	chatView := m.chatView.View()

	content := lipgloss.JoinHorizontal(lipgloss.Top, agentList, chatView)

	return fmt.Sprintf("%s\n%s\n%s", header, content, status)
}

// Run starts the TUI without a daemon connection.
func Run() error {
	p := tea.NewProgram(
		New(),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	_, err := p.Run()
	return err
}

// RunWithClient starts the TUI with a pre-connected daemon client.
func RunWithClient(client daemon.TUIClient, opts *TUIOptions) error {
	var initialAgentID string
	if opts != nil {
		initialAgentID = opts.InitialAgentID
	}
	slog.Debug("tui.RunWithClient: starting", "initial_agent_id", initialAgentID)
	p := tea.NewProgram(
		NewWithClient(client, opts),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
	slog.Debug("tui.RunWithClient: running program")
	_, err := p.Run()
	slog.Debug("tui.RunWithClient: program exited", "error", err)
	return err
}
