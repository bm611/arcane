package ui

import (
	"arcane/internal/db"
	"arcane/internal/models"
	"fmt"
	"os"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// GetMaxContextTokens returns the context limit for the current model
func (m *Model) GetMaxContextTokens() int {
	if m.CurrentModel.ContextLength > 0 {
		return m.CurrentModel.ContextLength
	}
	return DefaultContextTokens
}

func InitialModel() Model {
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

	mvp := viewport.New(ModalWidth-4, 15)

	return Model{
		TextInput:          ti,
		Viewport:           vp,
		ModelViewport:      mvp,
		Spinner:            sp,
		Client:             client,
		DB:                 dbConn,
		DBErr:              dbErr,
		CurrentChatID:      0,
		History:            []openai.ChatCompletionMessageParamUnion{},
		Renderer:           nil,
		Messages:           []string{},
		HistoryOpen:        false,
		HistorySelectedIdx: 0,
		HistoryChatCount:   0,
		HistoryChats:       nil,
		HistoryErr:         nil,
		HistoryPage:        0,
		ModelSelectorOpen:  false,
		CurrentModel:       AvailableModels[0], // Gemini Flash as default
		SelectedModelIndex: 0,
		AppMode:            models.ModeChat, // Start in chat mode by default
		WorkingDir:         cwd,
	}
}

func (m *Model) Init() tea.Cmd {
	// apiKey := os.Getenv("OPENROUTER_API_KEY")
	return tea.Batch(
		textinput.Blink,
		m.Spinner.Tick,
		// fetchModelContextLengthCmd(apiKey, m.currentModel.ID),
	)
}

func NewProgram() *tea.Program {
	m := InitialModel()
	p := tea.NewProgram(&m, tea.WithAltScreen())
	m.Program = p
	return p
}
