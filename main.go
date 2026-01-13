package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"

	"arcane/internal/db"
	"arcane/internal/models"
	"arcane/internal/styles"
	"arcane/internal/tools"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

const (
	maxChatWidth = 100
	modalWidth   = 60
	sidebarWidth = 30

	historyListLimit = 50

	// Context window management
	maxContextTokens    = 100000 // Target max tokens before compaction
	recentMessagesKeep  = 10     // Number of recent messages to keep intact
	charsPerToken       = 4      // Rough estimate for token calculation
	truncatedResultSize = 200    // Max chars for truncated tool results

	// Agent loop limit
	maxToolIterations = 15 // Max tool call rounds before forcing a response
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
- If you call tools, wait for the tool results before answering and base your answer strictly on those results
- Be concise in explanations, focus on the task

Current working directory: %s`

var availableModels = []models.AIModel{
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

type toolExecRecord struct {
	Name   string
	Args   string
	Result string
}

var inlineToolCallRE = regexp.MustCompile(`^\s*([a-zA-Z][a-zA-Z0-9_]*)\s*(\{.*\})\s*$`)

type OpenModelSelectorMsg struct{}
type CloseModelSelectorMsg struct{}
type ModelSelectedMsg struct{ Model models.AIModel }

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
	historyChats       []models.ChatListItem
	historyErr         error
	modelSelectorOpen  bool
	currentModel       models.AIModel
	selectedModelIndex int
	executingTool      string
	toolArguments      string
	toolActions        []models.ToolAction // Completed tool actions for current response
	program            *tea.Program
	contextTokens      int
	appMode            models.AppMode
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

	dbConn, dbErr := db.OpenArcaneDB()

	return model{
		textInput:          ti,
		viewport:           vp,
		spinner:            sp,
		client:             client,
		db:                 dbConn,
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
		currentModel:       availableModels[0], // Gemini Flash as default
		selectedModelIndex: 0,
		appMode:            models.ModeChat, // Start in chat mode by default
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
			if m.appMode == models.ModeChat {
				m.appMode = models.ModeAgent
			} else {
				m.appMode = models.ModeChat
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
				m.messages = append(m.messages, styles.ErrorStyle.Render(fmt.Sprintf("History error: %v", err)))
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
		m.toolActions = append(m.toolActions, models.ToolAction{
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
			m.messages = append(m.messages, styles.ErrorStyle.Render(fmt.Sprintf("History error: %v", err)))
		}
		m.updateViewport()
		return m, nil

	case errMsg:
		m.loading = false
		m.err = msg
		m.messages = append(m.messages, styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", msg)))
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
	label := styles.UserLabelStyle.Render("YOU")
	msg := styles.UserMsgStyle.Width(width - 4).Render(content)
	if isFirst {
		return fmt.Sprintf("\n%s\n%s", label, msg)
	}
	return fmt.Sprintf("%s\n%s", label, msg)
}

func formatAIMessage(content string) string {
	label := styles.AiLabelStyle.Render("ARCANE")
	msg := styles.AiMsgStyle.Render(content)
	return fmt.Sprintf("%s\n%s", label, msg)
}

func formatToolActions(actions []models.ToolAction) string {
	var lines []string
	for _, action := range actions {
		icon := styles.ToolIconStyle.Render("●")
		name := styles.ToolNameStyle.Render(action.Summary)
		lines = append(lines, styles.ToolActionStyle.Render(fmt.Sprintf("%s %s", icon, name)))
	}
	return strings.Join(lines, "\n")
}

func formatAIMessageWithTools(toolDisplay, content string) string {
	label := styles.AiLabelStyle.Render("ARCANE")
	msg := styles.AiMsgStyle.Render(content)
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
	subtitle := "“Code is not just logic, it is the architecture of imagination.”"

	styledArt := styles.WelcomeArtStyle.Render(art)
	styledSubtitle := styles.WelcomeSubtitleStyle.Italic(true).Render(subtitle)

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
		loadingParts = append(loadingParts, styles.AiLabelStyle.Render("ARCANE"))

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

func isKnownToolName(name string) bool {
	switch name {
	case "ls", "read", "write", "edit", "glob", "grep", "bash":
		return true
	default:
		return false
	}
}

func parseInlineToolCall(content string) (name string, argsJSON string, ok bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", "", false
	}

	m := inlineToolCallRE.FindStringSubmatch(content)
	if len(m) != 3 {
		return "", "", false
	}

	name = strings.TrimSpace(m[1])
	argsJSON = strings.TrimSpace(m[2])
	if !isKnownToolName(name) {
		return "", "", false
	}

	var v any
	if err := json.Unmarshal([]byte(argsJSON), &v); err != nil {
		return "", "", false
	}
	if _, ok := v.(map[string]any); !ok {
		return "", "", false
	}

	return name, argsJSON, true
}

func lastToolResult(execs []toolExecRecord, toolName string) (string, bool) {
	for i := len(execs) - 1; i >= 0; i-- {
		if execs[i].Name == toolName {
			return execs[i].Result, true
		}
	}
	return "", false
}

func formatLsResultAsAnswer(lsResult string) string {
	lsResult = strings.TrimSpace(lsResult)
	if lsResult == "" || lsResult == "(empty directory)" {
		return "The current directory is empty."
	}

	lines := strings.Split(lsResult, "\n")
	items := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, "[FILE]")
		line = strings.TrimPrefix(line, "[DIR]")
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		items = append(items, line)
	}
	if len(items) == 0 {
		return "The current directory is empty."
	}

	var sb strings.Builder
	sb.WriteString("Entries in the current directory:\n")
	for _, it := range items {
		sb.WriteString("- ")
		sb.WriteString(it)
		sb.WriteString("\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func coerceAgentFinalContent(content string, execs []toolExecRecord) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		if ls, ok := lastToolResult(execs, "ls"); ok {
			return formatLsResultAsAnswer(ls)
		}
		return content
	}
	if _, _, ok := parseInlineToolCall(trimmed); ok {
		if ls, ok := lastToolResult(execs, "ls"); ok {
			return formatLsResultAsAnswer(ls)
		}
		return content
	}

	// If the model claims emptiness but ls found entries, prefer the ls result.
	low := strings.ToLower(trimmed)
	if strings.Contains(low, "no files") || strings.Contains(low, "directory appears to be empty") || strings.Contains(low, "directory is empty") {
		if ls, ok := lastToolResult(execs, "ls"); ok {
			ls = strings.TrimSpace(ls)
			if ls != "" && ls != "(empty directory)" {
				return formatLsResultAsAnswer(ls)
			}
		}
	}

	return content
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

func findModelByID(id string) (models.AIModel, int, bool) {
	for i, mdl := range availableModels {
		if mdl.ID == id {
			return mdl, i, true
		}
	}
	return models.AIModel{}, 0, false
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

	count, chats, err := db.GetRecentChats(m.db, historyListLimit)
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
		id, err := db.CreateChat(m.db, nowUnix, m.currentModel.ID)
		if err != nil {
			return err
		}
		m.currentChatID = id
	}

	if err := db.InsertDBMessage(m.db, m.currentChatID, models.RoleUser, content, nowUnix); err != nil {
		return err
	}
	return db.UpdateChatOnUser(m.db, m.currentChatID, nowUnix, m.currentModel.ID, promptPreview(content))
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
	if err := db.InsertDBMessage(m.db, m.currentChatID, models.RoleAssistant, content, nowUnix); err != nil {
		return err
	}
	return db.TouchChat(m.db, m.currentChatID, nowUnix)
}

func (m *model) loadChatFromDB(chatID int64, modelID string) error {
	if m.dbErr != nil {
		return m.dbErr
	}
	if m.db == nil {
		return fmt.Errorf("history database not initialized")
	}

	msgs, err := db.GetChatMessages(m.db, chatID)
	if err != nil {
		return err
	}

	if modelID != "" {
		if mdl, idx, ok := findModelByID(modelID); ok {
			m.currentModel = mdl
			m.selectedModelIndex = idx
		} else {
			m.currentModel = models.AIModel{ID: modelID, Name: modelID, Provider: "Unknown"}
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
		case models.RoleUser:
			m.messages = append(m.messages, formatUserMessage(msg.Content, m.viewport.Width, len(m.messages) == 0))
			m.history = append(m.history, openai.UserMessage(msg.Content))
		case models.RoleAssistant:
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
		if m.appMode == models.ModeAgent {
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
		if m.appMode == models.ModeChat {
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
		var toolExecs []toolExecRecord
		iteration := 0
		for {
			iteration++

			// Compact history if approaching context limit
			history = compactHistory(history)

			resp, err := m.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
				Model:    m.currentModel.ID,
				Messages: history,
				Tools:    tools.Definitions,
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
			inlineName, inlineArgs, inlineOK := parseInlineToolCall(choice.Message.Content)

			// Some providers/models include speculative user-facing content alongside tool calls.
			// Keeping that content in history can anchor the model into incorrect answers even
			// after tool results arrive, so we drop it for tool-call messages.
			assistantMsg := choice.Message
			if len(assistantMsg.ToolCalls) > 0 || inlineOK {
				assistantMsg.Content = ""
			}
			history = append(history, assistantMsg.ToParam())

			if iteration >= maxToolIterations {
				content := choice.Message.Content
				if len(choice.Message.ToolCalls) > 0 || inlineOK {
					content = content + "\n\n*[Stopped after " + fmt.Sprint(maxToolIterations) + " tool iterations]*"
				}
				content = coerceAgentFinalContent(content, toolExecs)
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
			if len(choice.Message.ToolCalls) > 0 {
				for _, tc := range choice.Message.ToolCalls {
					if m.program != nil {
						m.program.Send(toolCallMsg{name: tc.Function.Name, arguments: tc.Function.Arguments})
					}

					result, err := tools.ExecuteTool(tc.Function.Name, tc.Function.Arguments)
					if err != nil {
						result = fmt.Sprintf("error: %v", err)
					}
					toolExecs = append(toolExecs, toolExecRecord{Name: tc.Function.Name, Args: tc.Function.Arguments, Result: result})
					history = append(history, openai.ToolMessage(tc.ID, result))

					if m.program != nil {
						summary := tools.GenerateToolSummary(tc.Function.Name, tc.Function.Arguments, result)
						m.program.Send(toolResultMsg{name: tc.Function.Name, result: result, summary: summary})
					}
				}
				continue
			}

			// GLM-style inline tool call fallback (e.g. `ls{}` in content with no tool_calls)
			if inlineOK {
				if m.program != nil {
					m.program.Send(toolCallMsg{name: inlineName, arguments: inlineArgs})
				}
				result, err := tools.ExecuteTool(inlineName, inlineArgs)
				if err != nil {
					result = fmt.Sprintf("error: %v", err)
				}
				toolExecs = append(toolExecs, toolExecRecord{Name: inlineName, Args: inlineArgs, Result: result})
				history = append(history, openai.AssistantMessage(fmt.Sprintf("Tool %s result:\n%s", inlineName, result)))
				if m.program != nil {
					summary := tools.GenerateToolSummary(inlineName, inlineArgs, result)
					m.program.Send(toolResultMsg{name: inlineName, result: result, summary: summary})
				}
				continue
			}

			content := coerceAgentFinalContent(choice.Message.Content, toolExecs)
			storedHistory := history[1:]
			return responseMsg{
				content:          content,
				promptTokens:     totalPromptTokens,
				completionTokens: totalCompletionTokens,
				history:          storedHistory,
				contextTokens:    estimateHistoryTokens(storedHistory),
			}
		}
	}
}

func (m *model) renderModelSelector() string {
	title := styles.ModalTitleStyle.Render("Select AI Model")

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
		if c, ok := styles.ProviderColors[mdl.Provider]; ok {
			providerColor = c
		}

		// Build row content as plain text first
		row1 := fmt.Sprintf("%s%s %s  %s", cursor, status, mdl.Name, mdl.Provider)
		row2 := fmt.Sprintf("     %s", mdl.Description)
		itemContent := fmt.Sprintf("%s\n%s", row1, row2)

		// Apply the appropriate style (selected vs normal)
		var styledItem string
		if isSelected {
			styledItem = styles.ModalSelectedStyle.Render(itemContent)
		} else {
			// For non-selected, apply provider color to the provider text
			row1Styled := fmt.Sprintf("%s%s %s  %s",
				lipgloss.NewStyle().Foreground(lipgloss.Color("#B39DDB")).Render(cursor),
				lipgloss.NewStyle().Foreground(lipgloss.Color("#90CAF9")).Render(status),
				lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#FFFFFF"}).Render(mdl.Name),
				lipgloss.NewStyle().Foreground(lipgloss.Color(providerColor)).Render(mdl.Provider),
			)
			row2Styled := fmt.Sprintf("     %s", lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(mdl.Description))
			styledItem = styles.ModalItemStyle.Render(fmt.Sprintf("%s\n%s", row1Styled, row2Styled))
		}

		items = append(items, styledItem)
	}

	// Join all items vertically
	listContent := lipgloss.JoinVertical(lipgloss.Left, items...)

	// Wrap everything in the modal content
	content := lipgloss.JoinVertical(lipgloss.Left, title, listContent)

	hint := lipgloss.NewStyle().
		Foreground(styles.HintColor).
		Width(styles.ContentWidth).
		PaddingTop(1).
		Render("↑/↓: navigate • Enter: select • Esc: close")

	return lipgloss.JoinVertical(lipgloss.Left, content, hint)
}

func (m *model) renderHistorySelector() string {
	title := styles.ModalTitleStyle.Render(fmt.Sprintf("Recent Chats (%d)", m.historyChatCount))

	var body string
	if m.historyErr != nil {
		errLine := lipgloss.NewStyle().Width(styles.ContentWidth).Render(styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", m.historyErr)))
		body = errLine
	} else if len(m.historyChats) == 0 {
		body = styles.ModalItemStyle.Render(lipgloss.NewStyle().Foreground(styles.HintColor).Render("No chats yet"))
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
			// styles.ContentWidth - 2 (padding) - len(cursor) - 1 (space) - len(timeStr)
			availableWidth := styles.ContentWidth - 2 - len(cursor) - 1 - len(timeStr)
			prompt = truncateRunes(prompt, availableWidth)

			itemContent := fmt.Sprintf("%s%s %s", cursor, prompt, lipgloss.NewStyle().Foreground(styles.HintColor).Render(timeStr))
			if isSelected {
				items = append(items, styles.ModalSelectedStyle.Render(itemContent))
			} else {
				items = append(items, styles.ModalItemStyle.Render(itemContent))
			}
		}
		body = lipgloss.JoinVertical(lipgloss.Left, items...)
	}

	content := lipgloss.JoinVertical(lipgloss.Left, title, body)
	hint := lipgloss.NewStyle().
		Foreground(styles.HintColor).
		Width(styles.ContentWidth).
		PaddingTop(1).
		Render("↑/↓: navigate • Enter: open • Esc: close")

	return lipgloss.JoinVertical(lipgloss.Left, content, hint)
}

func (m *model) renderSidebar(height int) string {
	w := sidebarWidth - 3 // Account for border and padding
	divider := styles.SidebarDividerStyle.Render(strings.Repeat("─", w))

	var sections []string

	// Model section
	providerColor := "#545454"
	if c, ok := styles.ProviderColors[m.currentModel.Provider]; ok {
		providerColor = c
	}
	modelSection := lipgloss.JoinVertical(lipgloss.Left,
		styles.SidebarTitleStyle.Render("MODEL"),
		styles.SidebarValueStyle.Render(truncateRunes(m.currentModel.Name, w)),
		lipgloss.NewStyle().Foreground(lipgloss.Color(providerColor)).Italic(true).Render(m.currentModel.Provider),
	)
	sections = append(sections, modelSection, divider)

	// Mode section
	var modeBadge string
	if m.appMode == models.ModeAgent {
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
		styles.SidebarTitleStyle.Render("MODE"),
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
		styles.SidebarTitleStyle.Render("CONTEXT"),
		styles.SidebarLabelStyle.Render(contextText),
		bar,
	)
	sections = append(sections, contextSection, divider)

	// Tokens section
	inTokens := fmt.Sprintf("%d", m.inputTokens)
	outTokens := fmt.Sprintf("%d", m.outputTokens)
	tokenSection := lipgloss.JoinVertical(lipgloss.Left,
		styles.SidebarTitleStyle.Render("TOKENS"),
		styles.SidebarLabelStyle.Render("In:  ")+styles.InputTokenStyle.Render(inTokens),
		styles.SidebarLabelStyle.Render("Out: ")+styles.OutputTokenStyle.Render(outTokens),
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
	shortcutLines := []string{styles.SidebarTitleStyle.Render("SHORTCUTS")}
	for _, s := range shortcuts {
		line := styles.SidebarShortcutKeyStyle.Render(s.key) + " " + styles.SidebarShortcutDescStyle.Render(s.desc)
		shortcutLines = append(shortcutLines, line)
	}
	shortcutSection := lipgloss.JoinVertical(lipgloss.Left, shortcutLines...)
	sections = append(sections, shortcutSection)

	content := lipgloss.JoinVertical(lipgloss.Left, sections...)
	return styles.SidebarStyle.Height(height).Width(sidebarWidth).Render(content)
}

func (m *model) View() string {
	chatWidth := m.windowWidth - sidebarWidth
	if chatWidth > maxChatWidth {
		chatWidth = maxChatWidth
	}
	inputBox := styles.InputBoxStyle.Width(chatWidth - 4).Render(m.textInput.View())

	// Build chat area (left side) - centered
	chatAreaWidth := m.windowWidth - sidebarWidth
	chatContent := lipgloss.JoinVertical(lipgloss.Center,
		styles.TitleStyle.Render("ARCANE AI"),
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
		modal = styles.ModalStyle.Width(modalWidth).Render(modal)

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
		modal = styles.ModalStyle.Width(modalWidth).Render(modal)

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
