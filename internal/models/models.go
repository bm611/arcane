package models

// AppMode represents the current operating mode of the application
type AppMode int

const (
	ModeChat  AppMode = iota // Regular conversation mode without tools
	ModeAgent                // Agent mode with full tool access
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

type AIModel struct {
	ID            string
	Name          string
	Provider      string
	Description   string
	ContextLength int // Maximum context window size in tokens
}

type ChatListItem struct {
	ID             int64
	UpdatedAtUnix  int64
	LastUserPrompt string
	ModelID        string
}

type DBMessage struct {
	Role    string
	Content string
}

// ToolAction represents a completed tool action for display
type ToolAction struct {
	Name    string
	Summary string
}
