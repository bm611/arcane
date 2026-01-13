package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	_ "modernc.org/sqlite"
)

const (
	maxChatWidth = 100
	modalWidth   = 60
	contentWidth = 54
	sidebarWidth = 30

	historyListLimit = 50

	roleUser      = "user"
	roleAssistant = "assistant"
	roleTool      = "tool"

	// Context window management
	maxContextTokens    = 100000 // Target max tokens before compaction
	recentMessagesKeep  = 10     // Number of recent messages to keep intact
	charsPerToken       = 4      // Rough estimate for token calculation
	truncatedResultSize = 200    // Max chars for truncated tool results

	// Agent loop limit
	maxToolIterations = 15 // Max tool call rounds before forcing a response
)

// AppMode represents the current operating mode of the application
type AppMode int

const (
	ModeChat  AppMode = iota // Regular conversation mode without tools
	ModeAgent                // Agent mode with full tool access
)

const chatSystemPrompt = `You are Arcane, a helpful AI assistant. You engage in natural conversation, answer questions, explain concepts, and help with general tasks. You provide clear, concise, and accurate responses. You do not have access to file system tools in this mode - if the user needs file operations, suggest they switch to Agent mode with Ctrl+A.`

const agentSystemPrompt = `You are Arcane, an AI coding assistant with full access to the file system. You can read, write, and edit files, search codebases, and run shell commands to help users with software development tasks.

Available tools:
- ls: List directory contents (defaults to current directory)
- read: Read file contents with line numbers
- write: Create or overwrite files
- edit: Find and replace text in files (old string must be unique)
- glob: Find files by pattern, sorted by modification time
- grep: Search files for regex patterns
- bash: Run shell commands (30s timeout)

Guidelines:
- Always read files before editing to understand context
- Make minimal, targeted changes
- Use ls and glob to explore the codebase structure first
- Verify your changes when possible by reading the file after editing
- Be concise in explanations, focus on the task

Current working directory: %s`

type AIModel struct {
	ID          string
	Name        string
	Provider    string
	Description string
}

type chatListItem struct {
	ID             int64
	UpdatedAtUnix  int64
	LastUserPrompt string
	ModelID        string
}

type dbMessage struct {
	Role    string
	Content string
}

var availableModels = []AIModel{
	{ID: "google/gemini-3-flash-preview", Name: "Gemini 3 Flash Preview", Provider: "Google", Description: "Fast multimodal model"},
	{ID: "x-ai/grok-code-fast-1", Name: "Grok Code Fast 1", Provider: "xAI", Description: "Code-focused fast model"},
	{ID: "deepseek/deepseek-v3.2", Name: "DeepSeek V3.2", Provider: "DeepSeek", Description: "Reasoning model"},
	{ID: "x-ai/grok-4.1-fast", Name: "Grok 4.1 Fast", Provider: "xAI", Description: "General purpose fast model"},
	{ID: "z-ai/glm-4.7", Name: "GLM 4.7", Provider: "Z.ai", Description: "Multilingual model"},
	{ID: "minimax/minimax-m2.1", Name: "MiniMax M2.1", Provider: "MiniMax", Description: "Chat model"},
	{ID: "perplexity/sonar-pro", Name: "Perplexity Sonar Pro", Provider: "Perplexity", Description: "Search-optimized model"},
	{ID: "openai/gpt-oss-120b:free", Name: "GPT-OSS 120B Free", Provider: "OpenAI", Description: "Open-source large language model"},
}

type errMsg error

type OpenModelSelectorMsg struct{}
type CloseModelSelectorMsg struct{}
type ModelSelectedMsg struct{ Model AIModel }

type responseMsg struct {
	content          string
	promptTokens     int64
	completionTokens int64
	history          []openai.ChatCompletionMessageParamUnion
	contextTokens    int
}

type toolCallMsg struct {
	name      string
	arguments string
}

type toolResultMsg struct {
	name    string
	result  string
	summary string // Brief summary of the action taken
}

// ToolAction represents a completed tool action for display
type ToolAction struct {
	Name    string
	Summary string
}

// generateToolSummary creates a brief summary of a tool action
func generateToolSummary(name string, argsJSON string, result string) string {
	var args map[string]interface{}
	json.Unmarshal([]byte(argsJSON), &args)

	switch name {
	case "read":
		path, _ := args["path"].(string)
		lines := strings.Count(result, "\n")
		if lines == 0 && result != "" {
			lines = 1
		}
		return fmt.Sprintf("READ %s (%d lines)", filepath.Base(path), lines)
	case "write":
		path, _ := args["path"].(string)
		content, _ := args["content"].(string)
		lines := strings.Count(content, "\n") + 1
		return fmt.Sprintf("WRITE %s (%d lines)", filepath.Base(path), lines)
	case "edit":
		path, _ := args["path"].(string)
		if strings.Contains(result, "error") {
			return fmt.Sprintf("EDIT %s (failed)", filepath.Base(path))
		}
		return fmt.Sprintf("EDIT %s", filepath.Base(path))
	case "glob":
		pat, _ := args["pat"].(string)
		matches := strings.Count(result, "\n")
		if result == "none" {
			matches = 0
		} else if matches == 0 && result != "" {
			matches = 1
		}
		return fmt.Sprintf("GLOB %s (%d files)", pat, matches)
	case "grep":
		pat, _ := args["pat"].(string)
		matches := strings.Count(result, "\n")
		if result == "none" {
			matches = 0
		} else if matches == 0 && result != "" {
			matches = 1
		}
		return fmt.Sprintf("GREP \"%s\" (%d matches)", pat, matches)
	case "bash":
		cmd, _ := args["cmd"].(string)
		if len(cmd) > 30 {
			cmd = cmd[:27] + "..."
		}
		return fmt.Sprintf("BASH %s", cmd)
	case "ls":
		path, _ := args["path"].(string)
		if path == "" {
			path = "."
		}
		entries := strings.Count(result, "\n")
		if result == "(empty directory)" {
			entries = 0
		}
		return fmt.Sprintf("LS %s (%d entries)", path, entries)
	default:
		return fmt.Sprintf("%s called", strings.ToUpper(name))
	}
}

var toolDefinitions = []openai.ChatCompletionToolUnionParam{
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "read",
		Description: openai.String("Read file with line numbers (file path, not directory)"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"path":   map[string]interface{}{"type": "string"},
				"offset": map[string]interface{}{"type": "integer"},
				"limit":  map[string]interface{}{"type": "integer"},
			},
			"required": []string{"path"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "write",
		Description: openai.String("Write content to file"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"path":    map[string]interface{}{"type": "string"},
				"content": map[string]interface{}{"type": "string"},
			},
			"required": []string{"path", "content"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "edit",
		Description: openai.String("Replace old with new in file (old must be unique unless all=true)"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
				"old":  map[string]interface{}{"type": "string"},
				"new":  map[string]interface{}{"type": "string"},
				"all":  map[string]interface{}{"type": "boolean"},
			},
			"required": []string{"path", "old", "new"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "glob",
		Description: openai.String("Find files by pattern, sorted by mtime"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"pat":  map[string]interface{}{"type": "string"},
				"path": map[string]interface{}{"type": "string"},
			},
			"required": []string{"pat"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "grep",
		Description: openai.String("Search files for regex pattern"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"pat":  map[string]interface{}{"type": "string"},
				"path": map[string]interface{}{"type": "string"},
			},
			"required": []string{"pat"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "bash",
		Description: openai.String("Run shell command"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"cmd": map[string]interface{}{"type": "string"},
			},
			"required": []string{"cmd"},
		},
	}),
	openai.ChatCompletionFunctionTool(openai.FunctionDefinitionParam{
		Name:        "ls",
		Description: openai.String("List files and directories in a path (defaults to current directory)"),
		Parameters: openai.FunctionParameters{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
			},
			"required": []string{},
		},
	}),
}

func toolRead(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	offset, _ := args["offset"].(float64)
	limit, _ := args["limit"].(float64)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	lines := strings.Split(string(data), "\n")
	start := int(offset)
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}

	end := len(lines)
	if limit > 0 {
		end = start + int(limit)
		if end > len(lines) {
			end = len(lines)
		}
	}

	selected := lines[start:end]
	var sb strings.Builder
	for i, line := range selected {
		sb.WriteString(fmt.Sprintf("%4d| %s\n", start+i+1, line))
	}
	return sb.String(), nil
}

func toolWrite(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	content, _ := args["content"].(string)

	err := os.WriteFile(path, []byte(content), 0644)
	if err != nil {
		return "", err
	}
	return "ok", nil
}

func toolEdit(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	oldStr, _ := args["old"].(string)
	newStr, _ := args["new"].(string)
	all, _ := args["all"].(bool)

	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}

	text := string(data)
	if !strings.Contains(text, oldStr) {
		return "error: old_string not found", nil
	}

	count := strings.Count(text, oldStr)
	if !all && count > 1 {
		return fmt.Sprintf("error: old_string appears %d times, must be unique (use all=true)", count), nil
	}

	var replacement string
	if all {
		replacement = strings.ReplaceAll(text, oldStr, newStr)
	} else {
		replacement = strings.Replace(text, oldStr, newStr, 1)
	}

	err = os.WriteFile(path, []byte(replacement), 0644)
	if err != nil {
		return "", err
	}
	return "ok", nil
}

func toolGlob(args map[string]interface{}) (string, error) {
	pat, _ := args["pat"].(string)
	root, _ := args["path"].(string)
	if root == "" {
		root = "."
	}

	pattern := filepath.Join(root, pat)
	matches, err := filepath.Glob(pattern)
	if err != nil {
		return "", err
	}

	type fileInfo struct {
		path  string
		mtime time.Time
	}

	var infos []fileInfo
	for _, m := range matches {
		stat, err := os.Stat(m)
		if err == nil {
			infos = append(infos, fileInfo{path: m, mtime: stat.ModTime()})
		} else {
			infos = append(infos, fileInfo{path: m})
		}
	}

	sort.Slice(infos, func(i, j int) bool {
		return infos[i].mtime.After(infos[j].mtime)
	})

	var result []string
	for _, info := range infos {
		result = append(result, info.path)
	}

	if len(result) == 0 {
		return "none", nil
	}
	return strings.Join(result, "\n"), nil
}

func toolGrep(args map[string]interface{}) (string, error) {
	pat, _ := args["pat"].(string)
	root, _ := args["path"].(string)
	if root == "" {
		root = "."
	}

	re, err := regexp.Compile(pat)
	if err != nil {
		return "", err
	}

	var hits []string
	err = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		lines := strings.Split(string(data), "\n")
		for i, line := range lines {
			if re.MatchString(line) {
				hits = append(hits, fmt.Sprintf("%s:%d:%s", path, i+1, strings.TrimSpace(line)))
				if len(hits) >= 50 {
					return io.EOF
				}
			}
		}
		return nil
	})

	if err != nil && err != io.EOF {
		return "", err
	}

	if len(hits) == 0 {
		return "none", nil
	}
	return strings.Join(hits, "\n"), nil
}

func toolBash(args map[string]interface{}) (string, error) {
	cmdStr, _ := args["cmd"].(string)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", cmdStr)
	out, err := cmd.CombinedOutput()
	result := strings.TrimSpace(string(out))
	if result == "" {
		if err != nil {
			return fmt.Sprintf("error: %v", err), nil
		}
		return "(empty)", nil
	}
	return result, nil
}

func toolLs(args map[string]interface{}) (string, error) {
	path, _ := args["path"].(string)
	if path == "" {
		path = "."
	}

	entries, err := os.ReadDir(path)
	if err != nil {
		return "", err
	}

	var sb strings.Builder
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			continue
		}
		
		typeChar := "-"
		if entry.IsDir() {
			typeChar = "d"
		}
		
		size := info.Size()
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		
		sb.WriteString(fmt.Sprintf("%s %8d  %s\n", typeChar, size, name))
	}
	
	result := sb.String()
	if result == "" {
		return "(empty directory)", nil
	}
	return result, nil
}

func (m *model) executeTool(name string, argsJSON string) (string, error) {
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return "", err
	}

	switch name {
	case "read":
		return toolRead(args)
	case "write":
		return toolWrite(args)
	case "edit":
		return toolEdit(args)
	case "glob":
		return toolGlob(args)
	case "grep":
		return toolGrep(args)
	case "bash":
		return toolBash(args)
	case "ls":
		return toolLs(args)
	default:
		return "", fmt.Errorf("unknown tool: %s", name)
	}
}

// estimateTokens provides a rough token count estimate based on character count
func estimateTokens(s string) int {
	return len(s) / charsPerToken
}

// estimateHistoryTokens calculates approximate token count for the entire history
func estimateHistoryTokens(history []openai.ChatCompletionMessageParamUnion) int {
	total := 0
	for _, msg := range history {
		total += estimateMessageTokens(msg)
	}
	return total
}

// estimateMessageTokens estimates tokens for a single message
func estimateMessageTokens(msg openai.ChatCompletionMessageParamUnion) int {
	// Extract content based on message type using JSON marshaling
	data, err := json.Marshal(msg)
	if err != nil {
		return 0
	}
	return estimateTokens(string(data))
}

// truncateToolResult shortens a tool result while preserving useful info
func truncateToolResult(toolName, result string) string {
	lines := strings.Split(result, "\n")
	lineCount := len(lines)
	
	if len(result) <= truncatedResultSize {
		return result
	}
	
	// Create a summary based on tool type
	preview := result
	if len(preview) > truncatedResultSize {
		preview = preview[:truncatedResultSize]
	}
	
	return fmt.Sprintf("[%s: %d lines] %s...", toolName, lineCount, strings.TrimSpace(preview))
}

// compactHistory reduces history size by truncating old tool results
func compactHistory(history []openai.ChatCompletionMessageParamUnion) []openai.ChatCompletionMessageParamUnion {
	currentTokens := estimateHistoryTokens(history)
	
	// If under limit, no compaction needed
	if currentTokens < maxContextTokens {
		return history
	}
	
	// Keep first message (initial task) and last N messages intact
	if len(history) <= recentMessagesKeep+1 {
		return history
	}
	
	compacted := make([]openai.ChatCompletionMessageParamUnion, 0, len(history))
	
	// Always keep first message
	compacted = append(compacted, history[0])
	
	// Process middle messages - truncate tool results
	middleEnd := len(history) - recentMessagesKeep
	for i := 1; i < middleEnd; i++ {
		msg := history[i]
		
		// Check if this is a tool result message by marshaling and inspecting
		data, err := json.Marshal(msg)
		if err != nil {
			compacted = append(compacted, msg)
			continue
		}
		
		var rawMsg map[string]interface{}
		if err := json.Unmarshal(data, &rawMsg); err != nil {
			compacted = append(compacted, msg)
			continue
		}
		
		// Check for tool message (has tool_call_id)
		if _, hasToolCallID := rawMsg["tool_call_id"]; hasToolCallID {
			if content, ok := rawMsg["content"].(string); ok && len(content) > truncatedResultSize {
				// Create truncated tool message
				truncated := truncateToolResult("tool", content)
				if toolCallID, ok := rawMsg["tool_call_id"].(string); ok {
					compacted = append(compacted, openai.ToolMessage(toolCallID, truncated))
					continue
				}
			}
		}
		
		compacted = append(compacted, msg)
	}
	
	// Keep last N messages intact
	compacted = append(compacted, history[middleEnd:]...)
	
	return compacted
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#B39DDB")).
			Padding(0, 1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#545454")).
			Render

	userLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#90CAF9")).
			Bold(true).
			Padding(0, 1).
			MarginRight(1)

	userMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#E0E0E0"}).
			PaddingLeft(2).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#90CAF9"))

	aiLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#B39DDB")).
			Bold(true).
			Padding(0, 1).
			MarginRight(1)

	aiMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#E0E0E0"}).
			PaddingTop(1).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#B39DDB"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#EF9A9A")).
			Bold(true)

	toolActionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			PaddingLeft(2)

	toolIconStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CE93D8")).
			Bold(true)

	toolNameStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFCC80")).
			Bold(true)

	toolDetailStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#545454"))

	sidebarStyle = lipgloss.NewStyle().
			BorderLeft(true).
			BorderStyle(lipgloss.NormalBorder()).
			BorderForeground(lipgloss.Color("#545454")).
			PaddingLeft(1).
			PaddingRight(1).
			PaddingTop(1)

	sidebarTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#B39DDB")).
				MarginBottom(0)

	sidebarLabelStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#AAAAAA"})

	sidebarValueStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#FFFFFF"})

	sidebarDividerStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#3a3a3a"))

	sidebarShortcutKeyStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#FFCC80")).
				Bold(true)

	sidebarShortcutDescStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#888888"))

	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#B39DDB")).
			Padding(0, 1)

	welcomeArtStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#B39DDB")).
			Bold(true)

	welcomeSubtitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#545454")).
				Italic(true)

	modalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#B39DDB")).
			Padding(1, 2)

	modalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#B39DDB")).
			Width(contentWidth).
			MarginBottom(1)

	modalItemStyle = lipgloss.NewStyle().
			Padding(0, 1).
			Width(contentWidth)

	modalSelectedStyle = lipgloss.NewStyle().
				Padding(0, 1).
				Background(lipgloss.Color("#2D2D44")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Width(contentWidth)

	modelNameStyle = lipgloss.NewStyle().
			Bold(true).
			MarginRight(1).
			Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#FFFFFF"})

	providerStyle = lipgloss.NewStyle().
			Italic(true).
			MarginRight(1)

	descStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Width(50)

	hintColor = lipgloss.Color("#545454")
)

var (
	providerColors = map[string]string{
		"Google":     "#CE93D8",
		"xAI":        "#FFCC80",
		"DeepSeek":   "#80CBC4",
		"MiniMax":    "#81D4FA",
		"Perplexity": "#EF9A9A",
		"Z.ai":       "#A5D6A7",
		"OpenAI":     "#A5D6A7",
	}
)

type model struct {
	viewport           viewport.Model
	messages           []string
	textInput          textinput.Model
	spinner            spinner.Model
	client             openai.Client
	db                 *sql.DB
	dbErr              error
	currentChatID      int64
	history            []openai.ChatCompletionMessageParamUnion
	renderer           *glamour.TermRenderer
	err                error
	loading            bool
	inputTokens        int64
	outputTokens       int64
	windowWidth        int
	windowHeight       int
	historyOpen        bool
	historySelectedIdx int
	historyChatCount   int
	historyChats       []chatListItem
	historyErr         error
	modelSelectorOpen  bool
	currentModel       AIModel
	selectedModelIndex int
	executingTool      string
	toolArguments      string
	toolActions        []ToolAction // Completed tool actions for current response
	program            *tea.Program
	contextTokens      int
	appMode            AppMode
}

func initialModel() model {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		fmt.Println("Error: OPENROUTER_API_KEY environment variable not set")
		os.Exit(1)
	}

	client := openai.NewClient(
		option.WithAPIKey(apiKey),
		option.WithBaseURL("https://openrouter.ai/api/v1"),
		option.WithHeader("HTTP-Referer", "https://github.com/broxdeez/arcane"), // Placeholder
		option.WithHeader("X-Title", "Arcane CLI"),
	)

	ti := textinput.New()
	ti.Placeholder = "Type a message..."
	ti.Prompt = "❯ "
	ti.PromptStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#B39DDB")).Bold(true)
	ti.CharLimit = 1000
	ti.Width = 80
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#B39DDB"))

	vp := viewport.New(60, 15)

	db, dbErr := openArcaneDB()

	return model{
		textInput:          ti,
		viewport:           vp,
		spinner:            sp,
		client:             client,
		db:                 db,
		dbErr:              dbErr,
		currentChatID:      0,
		history:            []openai.ChatCompletionMessageParamUnion{},
		renderer:           nil,
		messages:           []string{},
		historyOpen:        false,
		historySelectedIdx: 0,
		historyChatCount:   0,
		historyChats:       nil,
		historyErr:         nil,
		modelSelectorOpen:  false,
		currentModel:       availableModels[5], // minimax/minimax-m2.1 as default
		selectedModelIndex: 5,
		appMode:            ModeChat, // Start in chat mode by default
	}
}

func (m *model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
		spCmd tea.Cmd
	)

	switch msg := msg.(type) {
	case spinner.TickMsg:
		m.spinner, spCmd = m.spinner.Update(msg)
		if m.loading {
			m.updateViewport()
		}
		return m, spCmd

	case tea.KeyMsg:
		if m.historyOpen {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "ctrl+h":
				m.historyOpen = false
				m.historyErr = nil
				return m, nil
			case "up", "k":
				if len(m.historyChats) == 0 {
					return m, nil
				}
				m.historySelectedIdx--
				if m.historySelectedIdx < 0 {
					m.historySelectedIdx = len(m.historyChats) - 1
				}
				return m, nil
			case "down", "j":
				if len(m.historyChats) == 0 {
					return m, nil
				}
				m.historySelectedIdx++
				if m.historySelectedIdx >= len(m.historyChats) {
					m.historySelectedIdx = 0
				}
				return m, nil
			case "enter":
				if len(m.historyChats) == 0 {
					return m, nil
				}
				chat := m.historyChats[m.historySelectedIdx]
				if err := m.loadChatFromDB(chat.ID, chat.ModelID); err != nil {
					m.historyErr = err
					return m, nil
				}
				m.historyOpen = false
				m.historyErr = nil
				return m, nil
			}
			return m, nil
		}

		if m.modelSelectorOpen {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.modelSelectorOpen = false
				return m, nil
			case "ctrl+b":
				m.modelSelectorOpen = false
				return m, nil
			case "up", "k":
				m.selectedModelIndex--
				if m.selectedModelIndex < 0 {
					m.selectedModelIndex = len(availableModels) - 1
				}
				return m, nil
			case "down", "j":
				m.selectedModelIndex++
				if m.selectedModelIndex >= len(availableModels) {
					m.selectedModelIndex = 0
				}
				return m, nil
			case "enter":
				m.currentModel = availableModels[m.selectedModelIndex]
				m.modelSelectorOpen = false
				return m, nil
			}
			return m, nil
		}

		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit

		case tea.KeyCtrlN:
			m.resetSession()
			return m, nil

		case tea.KeyCtrlA:
			// Toggle between Chat and Agent mode
			if m.appMode == ModeChat {
				m.appMode = ModeAgent
			} else {
				m.appMode = ModeChat
			}
			return m, nil

		case tea.KeyCtrlB:
			m.modelSelectorOpen = true
			m.historyOpen = false
			return m, nil

		case tea.KeyCtrlH:
			m.modelSelectorOpen = false
			m.historyOpen = true
			m.refreshHistoryFromDB()
			return m, nil

		case tea.KeyEnter:
			if m.loading {
				return m, nil
			}
			input := m.textInput.Value()
			if input == "" {
				return m, nil
			}

			if input == "/clear" || input == "/reset" {
				m.resetSession()
				return m, nil
			}

			m.messages = append(m.messages, formatUserMessage(input, m.viewport.Width, len(m.messages) == 0))
			if err := m.persistUserMessage(input); err != nil {
				m.messages = append(m.messages, errorStyle.Render(fmt.Sprintf("History error: %v", err)))
			}
			m.textInput.Reset()
			m.loading = true
			m.updateViewport()

			return m, tea.Batch(m.sendMessage(input), m.spinner.Tick)
		}

	case toolCallMsg:
		m.executingTool = msg.name
		m.toolArguments = msg.arguments
		m.updateViewport()
		return m, nil

	case toolResultMsg:
		m.executingTool = ""
		m.toolArguments = ""
		// Store the completed tool action for display
		m.toolActions = append(m.toolActions, ToolAction{
			Name:    msg.name,
			Summary: msg.summary,
		})
		m.updateViewport()
		return m, nil

	case responseMsg:
		m.loading = false
		m.inputTokens += msg.promptTokens
		m.outputTokens += msg.completionTokens
		m.history = msg.history
		m.contextTokens = msg.contextTokens
		displayContent := msg.content
		if m.renderer != nil {
			rendered, _ := m.renderer.Render(msg.content)
			displayContent = strings.TrimSpace(rendered)
		}
		// If there were tool actions, prepend them to the message
		if len(m.toolActions) > 0 {
			toolDisplay := formatToolActions(m.toolActions)
			m.messages = append(m.messages, formatAIMessageWithTools(toolDisplay, displayContent))
		} else {
			m.messages = append(m.messages, formatAIMessage(displayContent))
		}
		m.toolActions = nil // Clear for next response
		if err := m.persistAssistantMessage(msg.content); err != nil {
			m.messages = append(m.messages, errorStyle.Render(fmt.Sprintf("History error: %v", err)))
		}
		m.updateViewport()
		return m, nil

	case errMsg:
		m.loading = false
		m.err = msg
		m.messages = append(m.messages, errorStyle.Render(fmt.Sprintf("Error: %v", msg)))
		m.updateViewport()
		return m, nil

	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
		m.windowHeight = msg.Height
		// Calculate chat width (left side) accounting for sidebar
		chatWidth := msg.Width - sidebarWidth
		if chatWidth > maxChatWidth {
			chatWidth = maxChatWidth
		}
		m.viewport.Width = chatWidth - 2
		m.viewport.Height = msg.Height - 6
		m.textInput.Width = chatWidth - 6
		glamourStyle := "dark"
		if !lipgloss.HasDarkBackground() {
			glamourStyle = "light"
		}
		m.renderer, _ = glamour.NewTermRenderer(
			glamour.WithStylePath(glamourStyle),
			glamour.WithWordWrap(chatWidth-6),
		)
		m.updateViewport()
		return m, tea.Batch(tiCmd, vpCmd)
	}

	m.textInput, tiCmd = m.textInput.Update(msg)

	// Filter out terminal background color queries and cursor reference codes that leak into the input
	val := m.textInput.Value()
	if strings.Contains(val, "]11;rgb:") || strings.Contains(val, "1;rgb:") || strings.Contains(val, "[1;1R") {
		m.textInput.Reset()
	}

	m.viewport, vpCmd = m.viewport.Update(msg)

	return m, tea.Batch(tiCmd, vpCmd)
}

func formatUserMessage(content string, width int, isFirst bool) string {
	label := userLabelStyle.Render("YOU")
	msg := userMsgStyle.Width(width - 4).Render(content)
	if isFirst {
		return fmt.Sprintf("\n%s\n%s", label, msg)
	}
	return fmt.Sprintf("%s\n%s", label, msg)
}

func formatAIMessage(content string) string {
	label := aiLabelStyle.Render("ARCANE")
	msg := aiMsgStyle.Render(content)
	return fmt.Sprintf("%s\n%s", label, msg)
}

func formatToolActions(actions []ToolAction) string {
	var lines []string
	for _, action := range actions {
		icon := toolIconStyle.Render("●")
		name := toolNameStyle.Render(action.Summary)
		lines = append(lines, toolActionStyle.Render(fmt.Sprintf("%s %s", icon, name)))
	}
	return strings.Join(lines, "\n")
}

func formatAIMessageWithTools(toolDisplay, content string) string {
	label := aiLabelStyle.Render("ARCANE")
	msg := aiMsgStyle.Render(content)
	return fmt.Sprintf("%s\n%s\n%s", label, toolDisplay, msg)
}

func getWelcomeScreen(width, height int) string {
	art := `
    ✧ ·──────────────────────────────────────────────────· ✧

	    ░█████╗░██████╗░░█████╗░░█████╗░███╗░░██╗███████╗
	    ██╔══██╗██╔══██╗██╔══██╗██╔══██╗████╗░██║██╔════╝
	    ███████║██████╔╝██║░░╚═╝███████║██╔██╗██║█████╗░░
	    ██╔══██║██╔══██╗██║░░▄█╗██╔══██║██║╚████║██╔══╝░░
	    ██║░░██║██║░░██║╚█████╔╝██║░░██║██║░╚███║███████╗
	    ╚═╝░░╚═╝╚═╝░░╚═╝░╚════╝░╚═╝░░╚═╝╚═╝░░╚══╝╚══════╝

    ✧ ·──────────────────────────────────────────────────· ✧
`
	subtitle := "The Arcane terminal awaits your command..."

	styledArt := welcomeArtStyle.Render(art)
	styledSubtitle := welcomeSubtitleStyle.Render(subtitle)

	content := lipgloss.JoinVertical(lipgloss.Center, styledArt, "", styledSubtitle)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (m *model) updateViewport() {
	if len(m.messages) == 0 && !m.loading {
		m.viewport.SetContent(getWelcomeScreen(m.viewport.Width, m.viewport.Height))
		return
	}

	content := strings.Join(m.messages, "\n\n")
	if m.loading {
		statusText := " Generating..."
		if m.executingTool != "" {
			statusText = fmt.Sprintf(" %s...", m.executingTool)
		}
		
		// Build loading message with completed tool actions
		var loadingParts []string
		loadingParts = append(loadingParts, aiLabelStyle.Render("ARCANE"))
		
		// Show completed tool actions
		if len(m.toolActions) > 0 {
			loadingParts = append(loadingParts, formatToolActions(m.toolActions))
		}
		
		// Show current status (spinner + status text)
		loadingParts = append(loadingParts, fmt.Sprintf("%s%s", m.spinner.View(), statusText))
		
		loadingMsg := strings.Join(loadingParts, "\n")
		if len(m.messages) > 0 {
			content = content + "\n\n" + loadingMsg
		} else {
			content = loadingMsg
		}
	}
	m.viewport.SetContent(content)
	m.viewport.GotoBottom()
}

func (m *model) resetSession() {
	m.messages = []string{}
	m.history = []openai.ChatCompletionMessageParamUnion{}
	m.currentChatID = 0
	m.inputTokens = 0
	m.outputTokens = 0
	m.contextTokens = 0
	m.historyOpen = false
	m.historyErr = nil
	m.viewport.SetContent(getWelcomeScreen(m.viewport.Width, m.viewport.Height))
	m.viewport.GotoTop()
	m.textInput.Reset()
}

func openArcaneDB() (*sql.DB, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		homeDir, herr := os.UserHomeDir()
		if herr != nil {
			return nil, err
		}
		configDir = filepath.Join(homeDir, ".config")
	}

	dbDir := filepath.Join(configDir, "arcane")
	if err := os.MkdirAll(dbDir, 0o700); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dbDir, "arcane.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		_ = db.Close()
		return nil, err
	}

	schema := []string{
		`CREATE TABLE IF NOT EXISTS chats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			model_id TEXT NOT NULL,
			last_user_prompt TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			FOREIGN KEY(chat_id) REFERENCES chats(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_chats_updated_at ON chats(updated_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_chat_id ON messages(chat_id, id);`,
	}

	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	return db, nil
}

func promptPreview(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	const maxRunes = 500
	r := []rune(s)
	if len(r) > maxRunes {
		return string(r[:maxRunes])
	}
	return s
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max <= 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	if d < 0 {
		d = -d
	}
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		mins := int(d.Minutes())
		if mins == 1 {
			return "1 min ago"
		}
		return fmt.Sprintf("%d mins ago", mins)
	}
	if d < 24*time.Hour {
		hrs := int(d.Hours())
		if hrs == 1 {
			return "1 hr ago"
		}
		return fmt.Sprintf("%d hrs ago", hrs)
	}
	days := int(d.Hours() / 24)
	if days < 14 {
		if days == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", days)
	}
	weeks := days / 7
	if weeks == 1 {
		return "1 week ago"
	}
	return fmt.Sprintf("%d weeks ago", weeks)
}

func createChat(db *sql.DB, nowUnix int64, modelID string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO chats(created_at, updated_at, model_id, last_user_prompt) VALUES(?, ?, ?, '')",
		nowUnix,
		nowUnix,
		modelID,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func insertDBMessage(db *sql.DB, chatID int64, role, content string, nowUnix int64) error {
	_, err := db.Exec(
		"INSERT INTO messages(chat_id, role, content, created_at) VALUES(?, ?, ?, ?)",
		chatID,
		role,
		content,
		nowUnix,
	)
	return err
}

func updateChatOnUser(db *sql.DB, chatID int64, nowUnix int64, modelID, lastUserPrompt string) error {
	_, err := db.Exec(
		"UPDATE chats SET updated_at = ?, model_id = ?, last_user_prompt = ? WHERE id = ?",
		nowUnix,
		modelID,
		lastUserPrompt,
		chatID,
	)
	return err
}

func touchChat(db *sql.DB, chatID int64, nowUnix int64) error {
	_, err := db.Exec(
		"UPDATE chats SET updated_at = ? WHERE id = ?",
		nowUnix,
		chatID,
	)
	return err
}

func getRecentChats(db *sql.DB, limit int) (int, []chatListItem, error) {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM chats").Scan(&count); err != nil {
		return 0, nil, err
	}

	rows, err := db.Query(
		"SELECT id, updated_at, last_user_prompt, model_id FROM chats ORDER BY updated_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	items := make([]chatListItem, 0, limit)
	for rows.Next() {
		var it chatListItem
		if err := rows.Scan(&it.ID, &it.UpdatedAtUnix, &it.LastUserPrompt, &it.ModelID); err != nil {
			return 0, nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}

	return count, items, nil
}

func getChatMessages(db *sql.DB, chatID int64) ([]dbMessage, error) {
	rows, err := db.Query(
		"SELECT role, content FROM messages WHERE chat_id = ? ORDER BY id ASC",
		chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	msgs := []dbMessage{}
	for rows.Next() {
		var m dbMessage
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}

func findModelByID(id string) (AIModel, int, bool) {
	for i, mdl := range availableModels {
		if mdl.ID == id {
			return mdl, i, true
		}
	}
	return AIModel{}, 0, false
}

func (m *model) refreshHistoryFromDB() {
	m.historyErr = nil
	m.historyChats = nil
	m.historyChatCount = 0
	m.historySelectedIdx = 0

	if m.dbErr != nil {
		m.historyErr = m.dbErr
		return
	}
	if m.db == nil {
		m.historyErr = fmt.Errorf("history database not initialized")
		return
	}

	count, chats, err := getRecentChats(m.db, historyListLimit)
	if err != nil {
		m.historyErr = err
		return
	}
	m.historyChatCount = count
	m.historyChats = chats
}

func (m *model) persistUserMessage(content string) error {
	if m.dbErr != nil {
		return m.dbErr
	}
	if m.db == nil {
		return fmt.Errorf("history database not initialized")
	}

	nowUnix := time.Now().Unix()
	if m.currentChatID == 0 {
		id, err := createChat(m.db, nowUnix, m.currentModel.ID)
		if err != nil {
			return err
		}
		m.currentChatID = id
	}

	if err := insertDBMessage(m.db, m.currentChatID, roleUser, content, nowUnix); err != nil {
		return err
	}
	return updateChatOnUser(m.db, m.currentChatID, nowUnix, m.currentModel.ID, promptPreview(content))
}

func (m *model) persistAssistantMessage(content string) error {
	if m.currentChatID == 0 {
		return nil
	}
	if m.dbErr != nil {
		return m.dbErr
	}
	if m.db == nil {
		return fmt.Errorf("history database not initialized")
	}

	nowUnix := time.Now().Unix()
	if err := insertDBMessage(m.db, m.currentChatID, roleAssistant, content, nowUnix); err != nil {
		return err
	}
	return touchChat(m.db, m.currentChatID, nowUnix)
}

func (m *model) loadChatFromDB(chatID int64, modelID string) error {
	if m.dbErr != nil {
		return m.dbErr
	}
	if m.db == nil {
		return fmt.Errorf("history database not initialized")
	}

	msgs, err := getChatMessages(m.db, chatID)
	if err != nil {
		return err
	}

	if modelID != "" {
		if mdl, idx, ok := findModelByID(modelID); ok {
			m.currentModel = mdl
			m.selectedModelIndex = idx
		} else {
			m.currentModel = AIModel{ID: modelID, Name: modelID, Provider: "Unknown"}
			m.selectedModelIndex = 0
		}
	}

	m.currentChatID = chatID
	m.loading = false
	m.inputTokens = 0
	m.outputTokens = 0
	m.messages = []string{}
	m.history = []openai.ChatCompletionMessageParamUnion{}

	for _, msg := range msgs {
		switch msg.Role {
		case roleUser:
			m.messages = append(m.messages, formatUserMessage(msg.Content, m.viewport.Width, len(m.messages) == 0))
			m.history = append(m.history, openai.UserMessage(msg.Content))
		case roleAssistant:
			displayContent := msg.Content
			if m.renderer != nil {
				rendered, _ := m.renderer.Render(msg.Content)
				displayContent = strings.TrimSpace(rendered)
			}
			m.messages = append(m.messages, formatAIMessage(displayContent))
			m.history = append(m.history, openai.AssistantMessage(msg.Content))
		}
	}

	m.updateViewport()
	return nil
}

func (m *model) sendMessage(input string) tea.Cmd {
	return func() tea.Msg {
		ctx := context.Background()

		// Build system prompt based on mode
		var systemPrompt string
		if m.appMode == ModeAgent {
			cwd, _ := os.Getwd()
			systemPrompt = fmt.Sprintf(agentSystemPrompt, cwd)
		} else {
			systemPrompt = chatSystemPrompt
		}

		// Build history with system prompt
		history := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
		}
		history = append(history, m.history...)
		history = append(history, openai.UserMessage(input))

		var totalPromptTokens int64
		var totalCompletionTokens int64

		// Chat mode: single API call without tools
		if m.appMode == ModeChat {
			resp, err := m.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
				Model:    m.currentModel.ID,
				Messages: history,
			})
			if err != nil {
				return errMsg(err)
			}

			if len(resp.Choices) == 0 {
				return errMsg(fmt.Errorf("empty response from model"))
			}

			// Remove system message from history before storing
			storedHistory := history[1:]
			storedHistory = append(storedHistory, resp.Choices[0].Message.ToParam())

			return responseMsg{
				content:          resp.Choices[0].Message.Content,
				promptTokens:     resp.Usage.PromptTokens,
				completionTokens: resp.Usage.CompletionTokens,
				history:          storedHistory,
				contextTokens:    estimateHistoryTokens(storedHistory),
			}
		}

		// Agent mode: agentic loop with tools
		iteration := 0
		for {
			iteration++
			
			// Compact history if approaching context limit
			history = compactHistory(history)

			resp, err := m.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
				Model:    m.currentModel.ID,
				Messages: history,
				Tools:    toolDefinitions,
			})
			if err != nil {
				return errMsg(err)
			}

			totalPromptTokens += resp.Usage.PromptTokens
			totalCompletionTokens += resp.Usage.CompletionTokens

			if len(resp.Choices) == 0 {
				return errMsg(fmt.Errorf("empty response from model"))
			}

			choice := resp.Choices[0]
			history = append(history, choice.Message.ToParam())

			if len(choice.Message.ToolCalls) == 0 || iteration >= maxToolIterations {
				content := choice.Message.Content
				if iteration >= maxToolIterations && len(choice.Message.ToolCalls) > 0 {
					content = content + "\n\n*[Stopped after " + fmt.Sprint(maxToolIterations) + " tool iterations]*"
				}
				// Remove system message from history before storing
				storedHistory := history[1:]
				return responseMsg{
					content:          content,
					promptTokens:     totalPromptTokens,
					completionTokens: totalCompletionTokens,
					history:          storedHistory,
					contextTokens:    estimateHistoryTokens(storedHistory),
				}
			}

			// Handle tool calls - notify UI for each tool
			for _, tc := range choice.Message.ToolCalls {
				if m.program != nil {
					m.program.Send(toolCallMsg{name: tc.Function.Name, arguments: tc.Function.Arguments})
				}

				result, err := m.executeTool(tc.Function.Name, tc.Function.Arguments)
				if err != nil {
					result = fmt.Sprintf("error: %v", err)
				}
				history = append(history, openai.ToolMessage(tc.ID, result))

				if m.program != nil {
					summary := generateToolSummary(tc.Function.Name, tc.Function.Arguments, result)
					m.program.Send(toolResultMsg{name: tc.Function.Name, result: result, summary: summary})
				}
			}
		}
	}
}

var (
	inputTokenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#90CAF9"))
	outputTokenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#B39DDB"))
)

func (m *model) renderModelSelector() string {
	title := modalTitleStyle.Render("Select AI Model")

	var items []string
	for i, mdl := range availableModels {
		isSelected := i == m.selectedModelIndex
		isCurrent := m.currentModel.ID == mdl.ID

		// Cursor / Selection Indicator
		cursor := "  "
		if isSelected {
			cursor = "> "
		}

		// Status Indicator (Active Model)
		status := " "
		if isCurrent {
			status = "●"
		}

		// Provider color
		providerColor := "#545454"
		if c, ok := providerColors[mdl.Provider]; ok {
			providerColor = c
		}

		// Build row content as plain text first
		row1 := fmt.Sprintf("%s%s %s  %s", cursor, status, mdl.Name, mdl.Provider)
		row2 := fmt.Sprintf("     %s", mdl.Description)
		itemContent := fmt.Sprintf("%s\n%s", row1, row2)

		// Apply the appropriate style (selected vs normal)
		var styledItem string
		if isSelected {
			styledItem = modalSelectedStyle.Render(itemContent)
		} else {
			// For non-selected, apply provider color to the provider text
			row1Styled := fmt.Sprintf("%s%s %s  %s",
				lipgloss.NewStyle().Foreground(lipgloss.Color("#B39DDB")).Render(cursor),
				lipgloss.NewStyle().Foreground(lipgloss.Color("#90CAF9")).Render(status),
				lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#FFFFFF"}).Render(mdl.Name),
				lipgloss.NewStyle().Foreground(lipgloss.Color(providerColor)).Render(mdl.Provider),
			)
			row2Styled := fmt.Sprintf("     %s", lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(mdl.Description))
			styledItem = modalItemStyle.Render(fmt.Sprintf("%s\n%s", row1Styled, row2Styled))
		}

		items = append(items, styledItem)
	}

	// Join all items vertically
	listContent := lipgloss.JoinVertical(lipgloss.Left, items...)

	// Wrap everything in the modal content
	content := lipgloss.JoinVertical(lipgloss.Left, title, listContent)

	hint := lipgloss.NewStyle().
		Foreground(hintColor).
		Width(contentWidth).
		PaddingTop(1).
		Render("↑/↓: navigate • Enter: select • Esc: close")

	return lipgloss.JoinVertical(lipgloss.Left, content, hint)
}

func (m *model) renderHistorySelector() string {
	title := modalTitleStyle.Render(fmt.Sprintf("Recent Chats (%d)", m.historyChatCount))

	var body string
	if m.historyErr != nil {
		errLine := lipgloss.NewStyle().Width(contentWidth).Render(errorStyle.Render(fmt.Sprintf("Error: %v", m.historyErr)))
		body = errLine
	} else if len(m.historyChats) == 0 {
		body = modalItemStyle.Render(lipgloss.NewStyle().Foreground(hintColor).Render("No chats yet"))
	} else {
		items := make([]string, 0, len(m.historyChats))
		for i, chat := range m.historyChats {
			isSelected := i == m.historySelectedIdx
			cursor := "  "
			if isSelected {
				cursor = "> "
			}
			timeStr := relativeTime(time.Unix(chat.UpdatedAtUnix, 0))
			prompt := promptPreview(chat.LastUserPrompt)
			if prompt == "" {
				prompt = "(no prompt)"
			}
			// One line: cursor + prompt + space + timeStr
			// contentWidth - 2 (padding) - len(cursor) - 1 (space) - len(timeStr)
			availableWidth := contentWidth - 2 - len(cursor) - 1 - len(timeStr)
			prompt = truncateRunes(prompt, availableWidth)

			itemContent := fmt.Sprintf("%s%s %s", cursor, prompt, lipgloss.NewStyle().Foreground(hintColor).Render(timeStr))
			if isSelected {
				items = append(items, modalSelectedStyle.Render(itemContent))
			} else {
				items = append(items, modalItemStyle.Render(itemContent))
			}
		}
		body = lipgloss.JoinVertical(lipgloss.Left, items...)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, title, body)
	hint := lipgloss.NewStyle().
		Foreground(hintColor).
		Width(contentWidth).
		PaddingTop(1).
		Render("↑/↓: navigate • Enter: open • Esc: close")

	return lipgloss.JoinVertical(lipgloss.Left, content, hint)
}

func (m *model) renderSidebar(height int) string {
	w := sidebarWidth - 3 // Account for border and padding
	divider := sidebarDividerStyle.Render(strings.Repeat("─", w))

	var sections []string

	// Model section
	providerColor := "#545454"
	if c, ok := providerColors[m.currentModel.Provider]; ok {
		providerColor = c
	}
	modelSection := lipgloss.JoinVertical(lipgloss.Left,
		sidebarTitleStyle.Render("MODEL"),
		sidebarValueStyle.Render(truncateRunes(m.currentModel.Name, w)),
		lipgloss.NewStyle().Foreground(lipgloss.Color(providerColor)).Italic(true).Render(m.currentModel.Provider),
	)
	sections = append(sections, modelSection, divider)

	// Mode section
	var modeBadge string
	if m.appMode == ModeAgent {
		modeBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#CE93D8")).
			Padding(0, 1).
			Render("AGENT")
	} else {
		modeBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#81D4FA")).
			Padding(0, 1).
			Render("CHAT")
	}
	modeSection := lipgloss.JoinVertical(lipgloss.Left,
		sidebarTitleStyle.Render("MODE"),
		modeBadge,
	)
	sections = append(sections, modeSection, divider)

	// Context section
	contextPct := 0.0
	if m.contextTokens > 0 {
		contextPct = float64(m.contextTokens) / float64(maxContextTokens) * 100
	}
	barWidth := w - 2
	filled := int(float64(barWidth) * contextPct / 100)
	if filled > barWidth {
		filled = barWidth
	}
	barColor := "#A5D6A7" // Green
	if contextPct > 80 {
		barColor = "#EF9A9A" // Red
	} else if contextPct > 60 {
		barColor = "#FFF59D" // Yellow
	}
	bar := lipgloss.NewStyle().Foreground(lipgloss.Color(barColor)).Render(strings.Repeat("█", filled)) +
		lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3a3a")).Render(strings.Repeat("░", barWidth-filled))
	contextText := fmt.Sprintf("%dk/%dk", m.contextTokens/1000, maxContextTokens/1000)
	contextSection := lipgloss.JoinVertical(lipgloss.Left,
		sidebarTitleStyle.Render("CONTEXT"),
		sidebarLabelStyle.Render(contextText),
		bar,
	)
	sections = append(sections, contextSection, divider)

	// Tokens section
	inTokens := fmt.Sprintf("%d", m.inputTokens)
	outTokens := fmt.Sprintf("%d", m.outputTokens)
	tokenSection := lipgloss.JoinVertical(lipgloss.Left,
		sidebarTitleStyle.Render("TOKENS"),
		sidebarLabelStyle.Render("In:  ")+inputTokenStyle.Render(inTokens),
		sidebarLabelStyle.Render("Out: ")+outputTokenStyle.Render(outTokens),
	)
	sections = append(sections, tokenSection, divider)

	// Shortcuts section
	shortcuts := []struct {
		key  string
		desc string
	}{
		{"^A", "mode"},
		{"^B", "models"},
		{"^H", "history"},
		{"^N", "new"},
		{"^C", "quit"},
	}
	shortcutLines := []string{sidebarTitleStyle.Render("SHORTCUTS")}
	for _, s := range shortcuts {
		line := sidebarShortcutKeyStyle.Render(s.key) + " " + sidebarShortcutDescStyle.Render(s.desc)
		shortcutLines = append(shortcutLines, line)
	}
	shortcutSection := lipgloss.JoinVertical(lipgloss.Left, shortcutLines...)
	sections = append(sections, shortcutSection)

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return sidebarStyle.Height(height).Width(sidebarWidth).Render(content)
}

func (m *model) View() string {
	chatWidth := m.windowWidth - sidebarWidth
	if chatWidth > maxChatWidth {
		chatWidth = maxChatWidth
	}
	inputBox := inputBoxStyle.Width(chatWidth - 4).Render(m.textInput.View())

	// Build chat area (left side) - centered
	chatAreaWidth := m.windowWidth - sidebarWidth
	chatContent := lipgloss.JoinVertical(lipgloss.Center,
		titleStyle.Render("ARCANE AI"),
		"",
		m.viewport.View(),
		"",
		inputBox,
	)
	chatArea := lipgloss.PlaceHorizontal(chatAreaWidth, lipgloss.Center, chatContent)

	// Build sidebar (right side)
	sidebar := m.renderSidebar(m.windowHeight - 2)

	// Combine: centered chat + sidebar at far right
	content := lipgloss.JoinHorizontal(lipgloss.Top, chatArea, sidebar)

	if m.historyOpen {
		modal := m.renderHistorySelector()
		modal = modalStyle.Width(modalWidth).Render(modal)

		return lipgloss.NewStyle().
			Background(lipgloss.Color("rgba(0,0,0,0.7)")).
			Render(lipgloss.Place(
				m.windowWidth,
				m.windowHeight,
				lipgloss.Center,
				lipgloss.Center,
				modal,
			))
	}

	if m.modelSelectorOpen {
		modal := m.renderModelSelector()
		modal = modalStyle.Width(modalWidth).Render(modal)

		return lipgloss.NewStyle().
			Background(lipgloss.Color("rgba(0,0,0,0.7)")).
			Render(lipgloss.Place(
				m.windowWidth,
				m.windowHeight,
				lipgloss.Center,
				lipgloss.Center,
				modal,
			))
	}

	return content
}

type programSetMsg struct {
	program *tea.Program
}

func main() {
	m := initialModel()
	p := tea.NewProgram(&m, tea.WithAltScreen())
	m.program = p
	finalModel, err := p.Run()
	if err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
	if m, ok := finalModel.(*model); ok {
		if m.db != nil {
			_ = m.db.Close()
		}
	}
}
