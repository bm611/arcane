package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
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
	maxChatWidth       = 100
	modalWidth         = 60
	compactWidthThresh = 100 // Width below which sidebar moves to bottom

	historyListLimit = 50
	historyPageSize  = 10

	// Context window management
	defaultContextTokens = 80000 // Fallback if model context length is not available
	recentMessagesKeep   = 6     // Number of recent messages to keep intact
	charsPerToken        = 4     // Rough estimate for token calculation
	truncatedResultSize  = 500   // Max chars for truncated tool results (increased for better context)

	// Agent loop limit
	maxToolIterations = 15 // Max tool call rounds before forcing a response
)

const chatSystemPrompt = `You are Arcane, a helpful AI assistant. You engage in natural conversation, answer questions, explain concepts, and help with general tasks. You provide clear, concise, and accurate responses. You do not have access to file system tools in this mode - if the user needs file operations, suggest they switch to Agent mode with Ctrl+A.`

const agentSystemPrompt = `You are Arcane, an AI coding assistant with full access to the file system.

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

// getFileSuggestions returns files/dirs matching a prefix, supporting subdirectory paths and recursive search
func getFileSuggestions(prefix string) []string {
	cwd, err := os.Getwd()
	if err != nil {
		return nil
	}

	// If prefix contains a "/", do directory-specific search
	if strings.Contains(prefix, "/") {
		return getDirectorySuggestions(cwd, prefix)
	}

	// Otherwise, do recursive fuzzy search
	return getRecursiveSuggestions(cwd, prefix)
}

// getDirectorySuggestions handles paths like "internal/tools/"
func getDirectorySuggestions(cwd, prefix string) []string {
	dir := ""
	filePrefix := prefix

	if idx := strings.LastIndex(prefix, "/"); idx != -1 {
		dir = prefix[:idx+1]
		filePrefix = prefix[idx+1:]
	}

	searchDir := cwd
	if dir != "" {
		searchDir = filepath.Join(cwd, dir)
	}

	entries, err := os.ReadDir(searchDir)
	if err != nil {
		return nil
	}

	var suggestions []string
	lowerFilePrefix := strings.ToLower(filePrefix)

	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(filePrefix, ".") {
			continue
		}
		if strings.HasPrefix(strings.ToLower(name), lowerFilePrefix) {
			fullPath := dir + name
			suggestions = append(suggestions, fullPath)
		}
	}

	return sortAndLimitSuggestions(cwd, suggestions)
}

// getRecursiveSuggestions searches all files recursively for matches
func getRecursiveSuggestions(cwd, prefix string) []string {
	var suggestions []string
	lowerPrefix := strings.ToLower(prefix)

	filepath.Walk(cwd, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}

		// Skip hidden directories and common non-code directories
		name := info.Name()
		if info.IsDir() {
			if strings.HasPrefix(name, ".") || name == "node_modules" || name == "vendor" || name == "__pycache__" {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip hidden files unless searching for them
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(prefix, ".") {
			return nil
		}

		// Match against filename
		if strings.Contains(strings.ToLower(name), lowerPrefix) {
			relPath, _ := filepath.Rel(cwd, path)
			suggestions = append(suggestions, relPath)
		}

		// Stop if we have enough suggestions
		if len(suggestions) >= 20 {
			return filepath.SkipAll
		}

		return nil
	})

	return sortAndLimitSuggestions(cwd, suggestions)
}

// sortAndLimitSuggestions sorts by directories first, then alphabetically, and limits results
func sortAndLimitSuggestions(cwd string, suggestions []string) []string {
	sort.Slice(suggestions, func(i, j int) bool {
		iInfo, _ := os.Stat(filepath.Join(cwd, suggestions[i]))
		jInfo, _ := os.Stat(filepath.Join(cwd, suggestions[j]))
		iDir := iInfo != nil && iInfo.IsDir()
		jDir := jInfo != nil && jInfo.IsDir()
		if iDir != jDir {
			return iDir
		}
		// Prefer shorter paths (closer to root)
		iDepth := strings.Count(suggestions[i], "/")
		jDepth := strings.Count(suggestions[j], "/")
		if iDepth != jDepth {
			return iDepth < jDepth
		}
		return strings.ToLower(suggestions[i]) < strings.ToLower(suggestions[j])
	})

	if len(suggestions) > 10 {
		suggestions = suggestions[:10]
	}

	return suggestions
}

// extractFileMentions parses @filename mentions from input and returns clean text + file list
func extractFileMentions(input string) (cleanInput string, files []string) {
	// Match @word or @"path with spaces"
	mentionRE := regexp.MustCompile(`@("([^"]+)"|([^\s]+))`)
	matches := mentionRE.FindAllStringSubmatch(input, -1)

	seen := make(map[string]bool)
	for _, match := range matches {
		var filename string
		if match[2] != "" {
			filename = match[2] // Quoted path
		} else {
			filename = match[3] // Unquoted path
		}
		if filename != "" && !seen[filename] {
			// Check if file exists
			if _, err := os.Stat(filename); err == nil {
				files = append(files, filename)
				seen[filename] = true
			}
		}
	}

	// Remove mentions from input for clean display
	cleanInput = mentionRE.ReplaceAllString(input, "")
	cleanInput = strings.TrimSpace(cleanInput)
	// Collapse multiple spaces
	cleanInput = regexp.MustCompile(`\s+`).ReplaceAllString(cleanInput, " ")

	return cleanInput, files
}

// buildFileContext creates a context string with file contents
func buildFileContext(files []string) string {
	if len(files) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("\n\n# Attached Files\n")

	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			continue
		}

		// Truncate large files
		text := string(content)
		lines := strings.Split(text, "\n")
		if len(lines) > 500 {
			text = strings.Join(lines[:500], "\n")
			text += fmt.Sprintf("\n\n[... truncated, %d more lines]", len(lines)-500)
		}

		sb.WriteString(fmt.Sprintf("\n## %s\n```\n%s\n```\n", file, text))
	}

	return sb.String()
}

// getAtPosition finds the @ mention being typed at cursor position
func getAtPosition(input string, cursorPos int) (prefix string, startPos int, found bool) {
	if cursorPos > len(input) {
		cursorPos = len(input)
	}

	// Look backwards from cursor for @
	for i := cursorPos - 1; i >= 0; i-- {
		ch := input[i]
		if ch == '@' {
			prefix = input[i+1 : cursorPos]
			return prefix, i, true
		}
		if ch == ' ' || ch == '\n' || ch == '\t' {
			return "", 0, false
		}
	}
	return "", 0, false
}

type toolExecRecord struct {
	Name   string
	Args   string
	Result string
}

var inlineToolCallRE = regexp.MustCompile(`^\s*([a-zA-Z][a-zA-Z0-9_]*)\s*(\{.*\})\s*$`)

type (
	OpenModelSelectorMsg  struct{}
	CloseModelSelectorMsg struct{}
	ModelSelectedMsg      struct{ Model models.AIModel }
)

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
	historyPage        int
	modelSelectorOpen  bool
	shortcutsOpen      bool
	currentModel       models.AIModel
	selectedModelIndex int
	executingTool      string
	toolArguments      string
	toolActions        []models.ToolAction // Completed tool actions for current response
	program            *tea.Program
	contextTokens      int
	appMode            models.AppMode

	// File mention autocomplete
	fileSuggestOpen     bool
	fileSuggestions     []string
	fileSuggestIdx      int
	fileSuggestPrefix   string   // The partial text after @ being completed
	attachedFiles       []string // Files attached via @mention for current message
	pendingFiles        []string // Files detected in current input (for display)

	// Working directory
	workingDir string
}

// getMaxContextTokens returns the context limit for the current model
func (m *model) getMaxContextTokens() int {
	if m.currentModel.ContextLength > 0 {
		return m.currentModel.ContextLength
	}
	return defaultContextTokens
}

// fetchModelContextLength fetches the context length for a model from OpenRouter API
func fetchModelContextLength(apiKey, modelID string) int {
	url := fmt.Sprintf("https://openrouter.ai/api/v1/models/%s", modelID)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return 0
	}

	var result struct {
		Data struct {
			ContextLength int `json:"context_length"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return 0
	}
	return result.Data.ContextLength
}

// fetchModelContextLengthMsg is sent when model context is fetched
type fetchModelContextLengthMsg struct {
	modelID       string
	contextLength int
}

// fetchModelContextLengthCmd fetches context length in background
func fetchModelContextLengthCmd(apiKey, modelID string) tea.Cmd {
	return func() tea.Msg {
		ctxLen := fetchModelContextLength(apiKey, modelID)
		return fetchModelContextLengthMsg{modelID: modelID, contextLength: ctxLen}
	}
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

	cwd, _ := os.Getwd()

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
		historyPage:        0,
		modelSelectorOpen:  false,
		currentModel:       availableModels[0], // Gemini Flash as default
		selectedModelIndex: 0,
		appMode:            models.ModeChat, // Start in chat mode by default
		workingDir:         cwd,
	}
}

func (m *model) Init() tea.Cmd {
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	return tea.Batch(
		textinput.Blink,
		m.spinner.Tick,
		fetchModelContextLengthCmd(apiKey, m.currentModel.ID),
	)
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
			case "left", "h":
				if m.historyPage > 0 {
					m.historyPage--
					m.refreshHistoryFromDB()
				}
				return m, nil
			case "right", "l":
				totalPages := (m.historyChatCount + historyPageSize - 1) / historyPageSize
				if m.historyPage < totalPages-1 {
					m.historyPage++
					m.refreshHistoryFromDB()
				}
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
				apiKey := os.Getenv("OPENROUTER_API_KEY")
				return m, fetchModelContextLengthCmd(apiKey, m.currentModel.ID)
			}
			return m, nil
		}

		if m.shortcutsOpen {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "enter", "?", "ctrl+s":
				m.shortcutsOpen = false
				return m, nil
			}
			return m, nil
		}

		// File suggestion popup handling
		if m.fileSuggestOpen {
			switch msg.String() {
			case "esc":
				m.fileSuggestOpen = false
				return m, nil
			case "up", "ctrl+p":
				if len(m.fileSuggestions) > 0 {
					m.fileSuggestIdx--
					if m.fileSuggestIdx < 0 {
						m.fileSuggestIdx = len(m.fileSuggestions) - 1
					}
				}
				return m, nil
			case "down", "ctrl+n":
				if len(m.fileSuggestions) > 0 {
					m.fileSuggestIdx++
					if m.fileSuggestIdx >= len(m.fileSuggestions) {
						m.fileSuggestIdx = 0
					}
				}
				return m, nil
			case "tab", "enter":
				if len(m.fileSuggestions) > 0 && m.fileSuggestIdx < len(m.fileSuggestions) {
					selected := m.fileSuggestions[m.fileSuggestIdx]
					// Replace the @prefix with @selected
					val := m.textInput.Value()
					cursorPos := m.textInput.Position()
					prefix, startPos, found := getAtPosition(val, cursorPos)
					if found {
						newVal := val[:startPos] + "@" + selected + " " + val[startPos+1+len(prefix):]
						m.textInput.SetValue(newVal)
						m.textInput.SetCursor(startPos + len(selected) + 2)
					}
					m.fileSuggestOpen = false
				}
				return m, nil
			}
		}

		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			if m.fileSuggestOpen {
				m.fileSuggestOpen = false
				return m, nil
			}
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
			m.shortcutsOpen = false
			return m, nil

		case tea.KeyCtrlS: // Using Ctrl+S for shortcuts
			m.shortcutsOpen = true
			m.modelSelectorOpen = false
			m.historyOpen = false
			return m, nil

		case tea.KeyCtrlH:
			m.modelSelectorOpen = false
			m.historyOpen = true
			m.shortcutsOpen = false
			m.historyPage = 0
			m.refreshHistoryFromDB()
			return m, nil

		case tea.KeyEnter:
			// If file suggestions are open, handle selection instead
			if m.fileSuggestOpen && len(m.fileSuggestions) > 0 {
				selected := m.fileSuggestions[m.fileSuggestIdx]
				val := m.textInput.Value()
				cursorPos := m.textInput.Position()
				prefix, startPos, found := getAtPosition(val, cursorPos)
				if found {
					newVal := val[:startPos] + "@" + selected + " " + val[startPos+1+len(prefix):]
					m.textInput.SetValue(newVal)
					m.textInput.SetCursor(startPos + len(selected) + 2)
				}
				m.fileSuggestOpen = false
				return m, nil
			}

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

			// Extract file mentions and build context
			cleanInput, files := extractFileMentions(input)
			m.attachedFiles = files

			// Display message shows clean input but indicates attached files
			displayInput := cleanInput
			if len(files) > 0 {
				fileNames := make([]string, len(files))
				for i, f := range files {
					fileNames[i] = filepath.Base(f)
				}
				displayInput = fmt.Sprintf("%s\n📎 %s", cleanInput, strings.Join(fileNames, ", "))
			}

			m.messages = append(m.messages, formatUserMessage(displayInput, m.viewport.Width, len(m.messages) == 0))
			if err := m.persistUserMessage(input); err != nil {
				m.messages = append(m.messages, styles.ErrorStyle.Render(fmt.Sprintf("History error: %v", err)))
			}
			m.textInput.Reset()
			m.fileSuggestOpen = false
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

	case fetchModelContextLengthMsg:
		if msg.modelID == m.currentModel.ID && msg.contextLength > 0 {
			m.currentModel.ContextLength = msg.contextLength
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.windowWidth = msg.Width
		m.windowHeight = msg.Height

		// Full width mode (no sidebar)
		// Reserve 2 lines for bottom bar + border
		chatWidth := msg.Width - 2
		if chatWidth > maxChatWidth {
			chatWidth = maxChatWidth
		}
		m.viewport.Width = chatWidth - 2
		m.viewport.Height = msg.Height - 8 // Extra space for bottom bar + input + header

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

	// Check for @ file mention trigger
	val = m.textInput.Value()
	cursorPos := m.textInput.Position()
	if prefix, _, found := getAtPosition(val, cursorPos); found {
		suggestions := getFileSuggestions(prefix)
		if len(suggestions) > 0 {
			m.fileSuggestions = suggestions
			m.fileSuggestOpen = true
			m.fileSuggestIdx = 0
			m.fileSuggestPrefix = prefix
		} else {
			m.fileSuggestOpen = false
		}
	} else {
		m.fileSuggestOpen = false
	}

	// Update pending files display (files currently mentioned in input)
	_, m.pendingFiles = extractFileMentions(val)

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
		icon := styles.ToolIconStyle.Render("→")
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
func compactHistory(history []openai.ChatCompletionMessageParamUnion, maxTokens int) []openai.ChatCompletionMessageParamUnion {
	currentTokens := estimateHistoryTokens(history)

	// If under limit, no compaction needed
	if currentTokens < maxTokens {
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
	m.historySelectedIdx = 0

	if m.dbErr != nil {
		m.historyErr = m.dbErr
		return
	}
	if m.db == nil {
		m.historyErr = fmt.Errorf("history database not initialized")
		return
	}

	offset := m.historyPage * historyPageSize
	count, chats, err := db.GetRecentChats(m.db, historyPageSize, offset)
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
	// Capture attached files before returning the command
	attachedFiles := m.attachedFiles
	m.attachedFiles = nil // Clear for next message

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

		// Extract clean input and build file context
		cleanInput, _ := extractFileMentions(input)
		fileContext := buildFileContext(attachedFiles)

		// Combine user message with file context
		userMessage := cleanInput
		if fileContext != "" {
			userMessage = cleanInput + fileContext
		}

		// Build history with system prompt
		history := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
		}
		history = append(history, m.history...)
		history = append(history, openai.UserMessage(userMessage))

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
			history = compactHistory(history, m.getMaxContextTokens())

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
	totalPages := (m.historyChatCount + historyPageSize - 1) / historyPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	title := styles.ModalTitleStyle.Render(fmt.Sprintf("Recent Chats (%d) - Page %d/%d", m.historyChatCount, m.historyPage+1, totalPages))

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
		Render("↑/↓: navigate • ←/→: page • Enter: open • Esc: close")

	return lipgloss.JoinVertical(lipgloss.Left, content, hint)
}

func (m *model) renderShortcutsModal() string {
	title := styles.ModalTitleStyle.Render("Keyboard Shortcuts")

	shortcuts := []struct {
		key  string
		desc string
	}{
		{"Ctrl+C", "Quit Application"},
		{"Ctrl+N", "New Chat Session"},
		{"Ctrl+A", "Toggle Agent/Chat Mode"},
		{"Ctrl+B", "Select AI Model"},
		{"Ctrl+H", "View Chat History"},
		{"Ctrl+S", "View Shortcuts (this menu)"},
		{"@", "Mention File (in input)"},
		{"Ctrl+L", "Clear Screen (standard)"},
	}

	var items []string
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFCC80")).
		Bold(true).
		Width(12)
	
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E0E0E0"))

	for _, s := range shortcuts {
		line := fmt.Sprintf("%s %s", keyStyle.Render(s.key), descStyle.Render(s.desc))
		items = append(items, styles.ModalItemStyle.Render(line))
	}

	listContent := lipgloss.JoinVertical(lipgloss.Left, items...)
	content := lipgloss.JoinVertical(lipgloss.Left, title, listContent)

	hint := lipgloss.NewStyle().
		Foreground(styles.HintColor).
		Width(styles.ContentWidth).
		PaddingTop(1).
		Render("Esc/Enter: close")

	return lipgloss.JoinVertical(lipgloss.Left, content, hint)
}

func (m *model) isCompactMode() bool {
	return m.windowWidth < compactWidthThresh
}

func (m *model) renderBottomBar() string {
	// 1. Mode Badge (Left)
	modeBadge := "CHAT"
	modeColor := "#81D4FA" // Light Blue
	if m.appMode == models.ModeAgent {
		modeBadge = "AGENT"
		modeColor = "#CE93D8" // Light Purple
	}
	mode := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color(modeColor)).
		Padding(0, 1).
		Render(modeBadge)

	// 2. Working Directory
	cwdDisplay := m.workingDir
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwdDisplay, home) {
		cwdDisplay = "~" + cwdDisplay[len(home):]
	}
	cwdDisplay = truncateRunes(cwdDisplay, 30)
	cwd := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Render(cwdDisplay)

	// 3. Model Name
	modelName := truncateRunes(m.currentModel.Name, 25)
	model := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#B39DDB")).
		Render(modelName)

	// 4. Context Window
	maxCtx := m.getMaxContextTokens()
	contextPct := 0
	if m.contextTokens > 0 && maxCtx > 0 {
		contextPct = int(float64(m.contextTokens) / float64(maxCtx) * 100)
	}
	ctxColor := "#888888"
	if contextPct > 80 {
		ctxColor = "#EF9A9A" // Red
	} else if contextPct > 60 {
		ctxColor = "#FFF59D" // Yellow
	}
	
	ctxText := fmt.Sprintf("%d%% (%dk/%dk)", contextPct, m.contextTokens/1000, maxCtx/1000)
	ctx := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ctxColor)).
		Render(ctxText)

	// 5. Token Usage
	tokens := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		Render(fmt.Sprintf("In:%d Out:%d", m.inputTokens, m.outputTokens))

	// 6. Help Hint (Far Right)
	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555555")).
		Render("Help: ^S")

	// Spacer to push items apart
	// We want: [Mode] [CWD] [Model] ...spacer... [Context] [Tokens] [Help]
	
	leftSide := lipgloss.JoinHorizontal(lipgloss.Center, mode, "  ", cwd, "  ", model)
	rightSide := lipgloss.JoinHorizontal(lipgloss.Center, ctx, "  ", tokens, "  ", help)
	
	// Calculate available space for spacer
	availableWidth := m.windowWidth - lipgloss.Width(leftSide) - lipgloss.Width(rightSide) - 2 // -2 for padding
	if availableWidth < 0 {
		availableWidth = 0
	}
	spacer := strings.Repeat(" ", availableWidth)

	bar := lipgloss.JoinHorizontal(lipgloss.Center, leftSide, spacer, rightSide)

	return lipgloss.NewStyle().
		Width(m.windowWidth).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#333333")).
		Padding(0, 1).
		Render(bar)
}

func (m *model) renderPendingFiles() string {
	if len(m.pendingFiles) == 0 {
		return ""
	}

	chipStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7C4DFF")).
		Padding(0, 1).
		MarginRight(1)

	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888"))

	var chips []string
	for _, file := range m.pendingFiles {
		chips = append(chips, chipStyle.Render("📄 "+filepath.Base(file)))
	}

	return labelStyle.Render("Attached: ") + strings.Join(chips, " ")
}

func (m *model) renderFileSuggestions() string {
	if !m.fileSuggestOpen || len(m.fileSuggestions) == 0 {
		return ""
	}

	suggestionStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E0E0E0")).
		Padding(0, 1)

	selectedStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color("#7C4DFF")).
		Padding(0, 1)

	var lines []string
	header := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Italic(true).
		Render("  Files (↑↓ to select, Tab/Enter to insert)")
	lines = append(lines, header)

	for i, suggestion := range m.fileSuggestions {
		// Check if it's a directory
		info, _ := os.Stat(suggestion)
		display := suggestion
		if info != nil && info.IsDir() {
			display = suggestion + "/"
		}

		if i == m.fileSuggestIdx {
			lines = append(lines, selectedStyle.Render("▸ "+display))
		} else {
			lines = append(lines, suggestionStyle.Render("  "+display))
		}
	}

	popupStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7C4DFF")).
		Background(lipgloss.Color("#1E1E2E")).
		Padding(0, 1)

	return popupStyle.Render(strings.Join(lines, "\n"))
}

func (m *model) View() string {
	var content string

	// Render file suggestions popup if open
	fileSuggestPopup := m.renderFileSuggestions()
	pendingFilesDisplay := m.renderPendingFiles()

	// Full-width chat with bottom bar
	chatWidth := m.windowWidth - 2
	if chatWidth > maxChatWidth {
		chatWidth = maxChatWidth
	}
	inputBox := styles.InputBoxStyle.Width(chatWidth - 4).Render(m.textInput.View())

	var inputSection string
	var inputParts []string
	if pendingFilesDisplay != "" {
		inputParts = append(inputParts, pendingFilesDisplay)
	}
	if fileSuggestPopup != "" {
		inputParts = append(inputParts, fileSuggestPopup)
	}
	inputParts = append(inputParts, inputBox)
	inputSection = lipgloss.JoinVertical(lipgloss.Left, inputParts...)

	chatContent := lipgloss.JoinVertical(lipgloss.Center,
		styles.TitleStyle.Render("ARCANE AI"),
		"",
		m.viewport.View(),
		"",
		inputSection,
	)
	chatArea := lipgloss.PlaceHorizontal(m.windowWidth, lipgloss.Center, chatContent)
	bottomBar := m.renderBottomBar()

	content = lipgloss.JoinVertical(lipgloss.Left, chatArea, bottomBar)

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

	if m.shortcutsOpen {
		modal := m.renderShortcutsModal()
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
