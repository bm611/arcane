package main

import (
	"context"
	"fmt"
	"os"
	"strings"

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
	contentWidth = 54
)

type AIModel struct {
	ID          string
	Name        string
	Provider    string
	Description string
}

var availableModels = []AIModel{
	{ID: "google/gemini-3-flash-preview", Name: "Gemini 3 Flash Preview", Provider: "Google", Description: "Fast multimodal model"},
	{ID: "x-ai/grok-code-fast-1", Name: "Grok Code Fast 1", Provider: "xAI", Description: "Code-focused fast model"},
	{ID: "deepseek/deepseek-v3.2", Name: "DeepSeek V3.2", Provider: "DeepSeek", Description: "Reasoning model"},
	{ID: "x-ai/grok-4.1-fast", Name: "Grok 4.1 Fast", Provider: "xAI", Description: "General purpose fast model"},
	{ID: "z-ai/glm-4.7", Name: "GLM 4.7", Provider: "Z.ai", Description: "Multilingual model"},
	{ID: "minimax/minimax-m2.1", Name: "MiniMax M2.1", Provider: "MiniMax", Description: "Chat model"},
	{ID: "perplexity/sonar-pro", Name: "Perplexity Sonar Pro", Provider: "Perplexity", Description: "Search-optimized model"},
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
	history            []openai.ChatCompletionMessageParamUnion
	renderer           *glamour.TermRenderer
	err                error
	loading            bool
	inputTokens        int64
	outputTokens       int64
	windowWidth        int
	windowHeight       int
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

	return model{
		textInput:          ti,
		viewport:           vp,
		spinner:            sp,
		client:             client,
		history:            []openai.ChatCompletionMessageParamUnion{},
		renderer:           nil,
		messages:           []string{},
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
		content := msg.content
		if m.renderer != nil {
			rendered, _ := m.renderer.Render(msg.content)
			content = strings.TrimSpace(rendered)
		}
		m.messages = append(m.messages, formatAIMessage(content))
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
	m.inputTokens = 0
	m.outputTokens = 0
	m.viewport.SetContent(getWelcomeScreen(m.viewport.Width, m.viewport.Height))
	m.viewport.GotoTop()
	m.textInput.Reset()
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
		infoStyle(fmt.Sprintf("\n(%s • ctrl+b: models • ctrl+n: new chat • ctrl+c: quit%s)", modelDisplay, tokenInfo)),
	)

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
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
