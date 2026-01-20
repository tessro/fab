// Package daemon provides the fab daemon server and IPC protocol.
package daemon

// TUIClient defines the interface for TUI components to communicate with the daemon.
// This interface enables unit testing of TUI components without a real daemon connection.
type TUIClient interface {
	// Connection management
	Connect() error
	Close() error
	IsConnected() bool

	// Event streaming
	StreamEvents(projects []string) (<-chan EventResult, error)
	StopEventStream()

	// Agent operations
	AgentList(project string) (*AgentListResponse, error)
	AgentSendMessage(id, content string) error
	AgentChatHistory(id string, limit int) (*AgentChatHistoryResponse, error)
	AgentAbort(id string, force bool) error

	// Manager operations
	ManagerSendMessage(project, content string) error
	ManagerChatHistory(project string, limit int) (*ManagerChatHistoryResponse, error)
	ManagerClearHistory(project string) error
	ManagerStop(project string) error

	// Planner operations
	PlanStart(project, prompt string) (*PlanStartResponse, error)
	PlanStop(id string) error
	PlanList(project string) (*PlanListResponse, error)
	PlanSendMessage(id, content string) error
	PlanChatHistory(id string, limit int) (*PlanChatHistoryResponse, error)

	// Approval operations
	RespondPermission(id, behavior, message string, interrupt bool) error
	RespondUserQuestion(id string, answers map[string]string) error

	// Project/Stats operations
	ProjectList() (*ProjectListResponse, error)
	Stats(project string) (*StatsResponse, error)
	CommitList(project string, limit int) (*CommitListResponse, error)
}

// Compile-time assertions to verify Client implements all interfaces.
var (
	_ TUIClient = (*Client)(nil)
)
