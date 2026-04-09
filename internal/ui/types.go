package ui

import (
	"arcane/internal/models"
	"context"
	"database/sql"
	"regexp"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/openai/openai-go/v3"
)

var (
	MaxChatWidth       = 100
	ModalWidth         = 60
	CompactWidthThresh = 100

	HistoryListLimit = 50
	HistoryPageSize  = 10
)

const (
	// Context window management
	DefaultContextTokens = 80000 // Fallback if model context length is not available
	RecentMessagesKeep   = 6     // Number of recent messages to keep intact
	CharsPerToken        = 4     // Rough estimate for token calculation
	TruncatedResultSize  = 2000  // Max chars for truncated tool results

	// Agent loop limit
	MaxToolIterations = 15 // Max tool call rounds before forcing a response
)

const ChatSystemPrompt = `You are Arcane, a helpful AI assistant. You engage in natural conversation, answer questions, explain concepts, and help with general tasks. You provide clear, concise, and accurate responses. You do not have access to file system tools in this mode - if the user needs file operations, suggest they switch to Agent mode with Ctrl+A.`

const AgentSystemPrompt = `You are Arcane, an AI coding assistant with full access to the file system.

Tools (all have output limits to save context):
- ls: List directory contents
- read: Read file (default 200 lines, use offset/limit for more)
- write: Create or overwrite files
- edit: Find and replace text (old string must be unique)
- glob: Find files by pattern
- grep: Search for regex (max 30 results, 5 per file)
- bash: Run shell commands (30s timeout, output truncated)

Guidelines:
- Read files before editing. Use offset parameter for large files.
- Make minimal, targeted changes
- Use grep with specific paths to narrow searches
- Be concise, focus on the task

Working directory: %s`

var AvailableModels = []models.AIModel{
	{ID: "x-ai/grok-4.1-fast", Name: "Grok 4.1 Fast", Provider: "Xai", Description: "General purpose fast model", ContextLength: 131072},
	{ID: "google/gemini-3-flash-preview", Name: "Gemini 3 Flash", Provider: "Gemini", Description: "Fast multimodal model", ContextLength: 1048576},
	{ID: "google/gemini-3.1-flash-lite-preview", Name: "Gemini 3.1 Flash Lite", Provider: "Gemini", Description: "Fast multimodal model", ContextLength: 1048576},
	{ID: "minimax/minimax-m2.7", Name: "MiniMax M2.7", Provider: "MiniMax", Description: "Chat model", ContextLength: 1000000},
	{ID: "perplexity/sonar-pro", Name: "Perplexity Sonar Pro", Provider: "Perplexity", Description: "Search-optimized model", ContextLength: 200000},
	{ID: "z-ai/glm-5.1", Name: "GLM 5.1", Provider: "Z.ai", Description: "Multilingual model", ContextLength: 128000},
	{ID: "openai/gpt-oss-120b:free", Name: "GPT-OSS 120B Free", Provider: "OpenAI", Description: "Open-source large language model", ContextLength: 128000},
}

type ErrMsg error

type StreamChunkMsg struct{ Delta string }
type CancelledMsg struct{}

type ToolExecRecord struct {
	Name   string
	Args   string
	Result string
}

var InlineToolCallRE = regexp.MustCompile(`^\s*([a-zA-Z][a-zA-Z0-9_]*)\s*(\{.*\})\s*$`)

type (
	OpenModelSelectorMsg  struct{}
	CloseModelSelectorMsg struct{}
	ModelSelectedMsg      struct{ Model models.AIModel }
)

type ResponseMsg struct {
	Content          string
	PromptTokens     int64
	CompletionTokens int64
	History          []openai.ChatCompletionMessageParamUnion
	ContextTokens    int
}

type ToolCallMsg struct {
	Name      string
	Arguments string
}

type ToolResultMsg struct {
	Name    string
	Result  string
	Summary string // Brief summary of the action taken
}

type Model struct {
	Viewport           viewport.Model
	Messages           []string
	TextInput          textarea.Model
	Spinner            spinner.Model
	Client             openai.Client
	DB                 *sql.DB
	DBErr              error
	CurrentChatID      int64
	History            []openai.ChatCompletionMessageParamUnion
	Renderer           *glamour.TermRenderer
	Err                error
	Loading            bool
	InputTokens        int64
	OutputTokens       int64
	WindowWidth        int
	WindowHeight       int
	HistoryOpen        bool
	HistorySelectedIdx int
	HistoryChatCount   int
	HistoryChats       []models.ChatListItem
	HistoryErr         error
	HistoryPage        int
	ModelSelectorOpen  bool
	ShortcutsOpen      bool
	CurrentModel       models.AIModel
	SelectedModelIndex int
	ModelViewport      viewport.Model
	ExecutingTool      string
	ToolArguments      string
	ToolActions        []models.ToolAction // Completed tool actions for current response
	Program            *tea.Program
	ContextTokens      int
	AppMode            models.AppMode

	// File mention autocomplete
	FileSuggestOpen   bool
	FileSuggestions   []string
	FileSuggestIdx    int
	FileSuggestPrefix string   // The partial text after @ being completed
	AttachedFiles     []string // Files attached via @mention for current message
	PendingFiles      []string // Files detected in current input (for display)

	// Working directory
	WorkingDir string

	// Mouse interaction
	MouseHoverArt bool

	// Streaming
	StreamingContent string             // Accumulated streaming response being built
	CancelFn         context.CancelFunc // Cancel function for the in-progress request
}
