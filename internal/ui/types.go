package ui

import (
	"arcane/internal/models"
	"database/sql"
	"regexp"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/openai/openai-go/v3"
)

const (
	MaxChatWidth       = 100
	ModalWidth         = 60
	CompactWidthThresh = 100 // Width below which sidebar moves to bottom

	HistoryListLimit = 50
	HistoryPageSize  = 10

	// Context window management
	DefaultContextTokens = 80000 // Fallback if model context length is not available
	RecentMessagesKeep   = 6     // Number of recent messages to keep intact
	CharsPerToken        = 4     // Rough estimate for token calculation
	TruncatedResultSize  = 500   // Max chars for truncated tool results (increased for better context)

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
	{ID: "google/gemini-3-flash-preview", Name: "Gemini 3 Flash Preview", Provider: "Google", Description: "Fast multimodal model"},
	{ID: "x-ai/grok-code-fast-1", Name: "Grok Code Fast 1", Provider: "xAI", Description: "Code-focused fast model"},
	{ID: "deepseek/deepseek-v3.2", Name: "DeepSeek V3.2", Provider: "DeepSeek", Description: "Reasoning model"},
	{ID: "x-ai/grok-4.1-fast", Name: "Grok 4.1 Fast", Provider: "xAI", Description: "General purpose fast model"},
	{ID: "z-ai/glm-4.7", Name: "GLM 4.7", Provider: "Z.ai", Description: "Multilingual model"},
	{ID: "minimax/minimax-m2.1", Name: "MiniMax M2.1", Provider: "MiniMax", Description: "Chat model"},
	{ID: "perplexity/sonar-pro", Name: "Perplexity Sonar Pro", Provider: "Perplexity", Description: "Search-optimized model"},
	{ID: "openai/gpt-oss-120b:free", Name: "GPT-OSS 120B Free", Provider: "OpenAI", Description: "Open-source large language model"},
}

type ErrMsg error

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
	TextInput          textinput.Model
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
	ExecutingTool      string
	ToolArguments      string
	ToolActions        []models.ToolAction // Completed tool actions for current response
	Program            *tea.Program
	ContextTokens      int
	AppMode            models.AppMode

	// File mention autocomplete
	FileSuggestOpen     bool
	FileSuggestions     []string
	FileSuggestIdx      int
	FileSuggestPrefix   string   // The partial text after @ being completed
	AttachedFiles       []string // Files attached via @mention for current message
	PendingFiles        []string // Files detected in current input (for display)

	// Working directory
	WorkingDir string
}
