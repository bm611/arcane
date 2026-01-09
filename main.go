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
	chatModel    = "minimax/minimax-m2.1"
	searchModel  = "perplexity/sonar-pro"
	maxChatWidth = 100
)

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
			Foreground(lipgloss.Color("#E0E0E0")).
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
)

type errMsg error

type responseMsg struct {
	content          string
	promptTokens     int64
	completionTokens int64
}

type model struct {
	viewport     viewport.Model
	messages     []string
	textInput    textinput.Model
	spinner      spinner.Model
	client       openai.Client
	history      []openai.ChatCompletionMessageParamUnion
	renderer     *glamour.TermRenderer
	err          error
	loading      bool
	inputTokens  int64
	outputTokens int64
	windowWidth  int
	windowHeight int
	searchMode   bool
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
		textInput: ti,
		viewport:  vp,
		spinner:   sp,
		client:    client,
		history:   []openai.ChatCompletionMessageParamUnion{},
		renderer:  nil, // defer initialization until WindowSizeMsg
		messages:  []string{},
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
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit

		case tea.KeyCtrlN:
			m.resetSession()
			return m, nil

		case tea.KeyCtrlS:
			m.searchMode = !m.searchMode
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

			m.messages = append(m.messages, formatUserMessage(input))
			m.textInput.Reset()
			m.loading = true
			m.updateViewport()

			return m, tea.Batch(m.sendMessage(input), m.spinner.Tick)
		}

	case responseMsg:
		m.loading = false
		m.inputTokens += msg.promptTokens
		m.outputTokens += msg.completionTokens
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
		m.renderer, _ = glamour.NewTermRenderer(
			glamour.WithAutoStyle(),
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

func formatUserMessage(content string) string {
	label := userLabelStyle.Render("YOU")
	msg := userMsgStyle.Render(content)
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

		activeModel := chatModel
		if m.searchMode {
			activeModel = searchModel
		}

		ctx := context.Background()
		resp, err := m.client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    activeModel,
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
		}
	}
}

var (
	inputTokenStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#00ADD8"))
	outputTokenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
	chatModelStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#7D56F4"))
	searchModelStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF9F1C"))
)

func (m model) View() string {
	inputBox := inputBoxStyle.Width(m.viewport.Width - 2).Render(m.textInput.View())

	tokenInfo := ""
	if m.inputTokens > 0 || m.outputTokens > 0 {
		tokenInfo = fmt.Sprintf(" • in: %s out: %s",
			inputTokenStyle.Render(fmt.Sprintf("%d", m.inputTokens)),
			outputTokenStyle.Render(fmt.Sprintf("%d", m.outputTokens)),
		)
	}

	modelDisplay := chatModelStyle.Render(chatModel)
	if m.searchMode {
		modelDisplay = searchModelStyle.Render(searchModel)
	}

	content := fmt.Sprintf(
		"%s\n\n%s\n\n%s\n%s",
		titleStyle.Render("ARCANE AI"),
		m.viewport.View(),
		inputBox,
		infoStyle(fmt.Sprintf("\n(%s • ctrl+s: toggle search • ctrl+n: new chat • ctrl+c: quit%s)", modelDisplay, tokenInfo)),
	)

	return lipgloss.Place(m.windowWidth, m.windowHeight, lipgloss.Center, lipgloss.Top, content)
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
		os.Exit(1)
	}
}
