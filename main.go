package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
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

	historyListLimit = 50

	roleUser      = "user"
	roleAssistant = "assistant"
)

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
}

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
			Padding(0, 1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#545454")).
			Render

	userLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#00ADD8")).
			Bold(true).
			Padding(0, 1).
			MarginRight(1)

	userMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#E0E0E0"}).
			PaddingLeft(2).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#00ADD8"))

	aiLabelStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#7D56F4")).
			Bold(true).
			Padding(0, 1).
			MarginRight(1)

	aiMsgStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#E0E0E0"}).
			PaddingTop(1).
			BorderLeft(true).
			BorderStyle(lipgloss.ThickBorder()).
			BorderForeground(lipgloss.Color("#7D56F4"))

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FF6B6B")).
			Bold(true)

	inputBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#545454")).
			Padding(0, 1)

	welcomeArtStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#7D56F4")).
			Bold(true)

	welcomeSubtitleStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#545454")).
				Italic(true)

	modalStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#7D56F4")).
			Padding(1, 2)

	modalTitleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#7D56F4")).
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
		"Google":     "#9B59B6",
		"xAI":        "#E67E22",
		"DeepSeek":   "#1ABC9C",
		"MiniMax":    "#3498DB",
		"Perplexity": "#E74C3C",
		"Z.ai":       "#2ECC71",
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
	ti.CharLimit = 1000
	ti.Width = 80
	ti.Focus()

	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))

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
	}
}

func (m model) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, m.spinner.Tick)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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

	case responseMsg:
		m.loading = false
		m.inputTokens += msg.promptTokens
		m.outputTokens += msg.completionTokens
		m.history = msg.history
		displayContent := msg.content
		if m.renderer != nil {
			rendered, _ := m.renderer.Render(msg.content)
			displayContent = strings.TrimSpace(rendered)
		}
		m.messages = append(m.messages, formatAIMessage(displayContent))
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
		contentWidth := msg.Width
		if contentWidth > maxChatWidth {
			contentWidth = maxChatWidth
		}
		m.viewport.Width = contentWidth
		m.viewport.Height = msg.Height - 6
		m.textInput.Width = contentWidth - 4
		glamourStyle := "dark"
		if !lipgloss.HasDarkBackground() {
			glamourStyle = "light"
		}
		m.renderer, _ = glamour.NewTermRenderer(
			glamour.WithStylePath(glamourStyle),
			glamour.WithWordWrap(contentWidth-4),
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
		loadingMsg := fmt.Sprintf("%s\n%s Generating...", aiLabelStyle.Render("ARCANE"), m.spinner.View())
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

func (m model) sendMessage(input string) tea.Cmd {
	return func() tea.Msg {
		m.history = append(m.history, openai.UserMessage(input))

		ctx := context.Background()
		resp, err := m.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    m.currentModel.ID,
			Messages: m.history,
		})
		if err != nil {
			return errMsg(err)
		}

		if len(resp.Choices) == 0 || resp.Choices[0].Message.Content == "" {
			return errMsg(fmt.Errorf("empty response from model"))
		}

		content := resp.Choices[0].Message.Content
		m.history = append(m.history, openai.AssistantMessage(content))

		return responseMsg{
			content:          content,
			promptTokens:     resp.Usage.PromptTokens,
			completionTokens: resp.Usage.CompletionTokens,
			history:          m.history,
		}
	}
}

var (
	inputTokenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ADD8"))
	outputTokenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
)

func (m model) renderModelSelector() string {
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
				lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4")).Render(cursor),
				lipgloss.NewStyle().Foreground(lipgloss.Color("#00ADD8")).Render(status),
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

func (m model) renderHistorySelector() string {
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

func (m model) View() string {
	inputBox := inputBoxStyle.Width(m.viewport.Width - 2).Render(m.textInput.View())

	tokenInfo := ""
	if m.inputTokens > 0 || m.outputTokens > 0 {
		tokenInfo = fmt.Sprintf(" • in: %s out: %s",
			inputTokenStyle.Render(fmt.Sprintf("%d", m.inputTokens)),
			outputTokenStyle.Render(fmt.Sprintf("%d", m.outputTokens)),
		)
	}

	providerColor := "#545454"
	if c, ok := providerColors[m.currentModel.Provider]; ok {
		providerColor = c
	}
	modelDisplay := lipgloss.NewStyle().Foreground(lipgloss.Color(providerColor)).Render(m.currentModel.Name)

	content := fmt.Sprintf(
		"%s\n\n%s\n\n%s\n%s",
		titleStyle.Render("ARCANE AI"),
		m.viewport.View(),
		inputBox,
		infoStyle(fmt.Sprintf("\n(%s • ctrl+b: models • ctrl+h: history • ctrl+n: new chat • ctrl+c: quit%s)", modelDisplay, tokenInfo)),
	)

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

	return lipgloss.Place(m.windowWidth, m.windowHeight, lipgloss.Center, lipgloss.Top, content)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
	if m, ok := finalModel.(model); ok {
		if m.db != nil {
			_ = m.db.Close()
		}
	}
}
