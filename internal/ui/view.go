package ui

import (
	"arcane/internal/styles"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func (m *Model) UpdateModelSelectorContent() {
	var items []string
	var lastProvider string
	for i, mdl := range AvailableModels {
		if mdl.Provider != lastProvider {
			if lastProvider != "" {
				items = append(items, "")
			}
			providerColor := "#545454"
			if c, ok := styles.ProviderColors[mdl.Provider]; ok {
				providerColor = c
			}
			header := styles.ModalHeaderStyle.Copy().
				Foreground(lipgloss.Color(providerColor)).
				Render(mdl.Provider)
			items = append(items, header)
			lastProvider = mdl.Provider
		}

		isSelected := i == m.SelectedModelIndex
		isCurrent := m.CurrentModel.ID == mdl.ID

		// Build row content as plain text first
		// We format it simply as "ModelName" (left aligned)
		// The active status "●" will be appended or prepended minimally if needed,
		// or we can bold the active one.
		// User asked for "left align both model and provider name".
		// Provider headers are already left aligned.
		// Let's just show the model name.

		displayName := mdl.Name
		if isCurrent {
			displayName = "● " + displayName
		} else {
			displayName = "  " + displayName
		}

		// Apply the appropriate style (selected vs normal)
		var styledItem string
		if isSelected {
			// Highlight the whole line by ensuring width fills content
			styledItem = styles.ModalSelectedStyle.Copy().
				Width(styles.ContentWidth).
				Render(displayName)
		} else {
			// Normal style
			style := styles.ModalItemStyle.Copy().Width(styles.ContentWidth)
			if isCurrent {
				style = style.Foreground(lipgloss.Color("#90CAF9")) // Highlight active model text
			} else {
				style = style.Foreground(lipgloss.AdaptiveColor{Light: "#1a1a2e", Dark: "#FFFFFF"})
			}
			styledItem = style.Render(displayName)
		}

		items = append(items, styledItem)
	}

	// Join all items vertically
	listContent := lipgloss.JoinVertical(lipgloss.Left, items...)
	m.ModelViewport.SetContent(listContent)
}

func (m *Model) RenderModelSelector() string {
	title := styles.ModalTitleStyle.Render("Select AI Model")
	
	// Ensure content is up to date (this might be better called in Update, but good for safety)
	// m.UpdateModelSelectorContent() // Commented out to avoid side effects in Render, call explicitly in Update

	// Wrap everything in the modal content
	content := lipgloss.JoinVertical(lipgloss.Left, title, m.ModelViewport.View())

	hint := lipgloss.NewStyle().
		Foreground(styles.HintColor).
		Width(styles.ContentWidth).
		PaddingTop(1).
		Render("↑/↓: navigate • Enter: select • Esc: close")

	return lipgloss.JoinVertical(lipgloss.Left, content, hint)
}

func (m *Model) RenderHistorySelector() string {
	totalPages := (m.HistoryChatCount + HistoryPageSize - 1) / HistoryPageSize
	if totalPages < 1 {
		totalPages = 1
	}
	title := styles.ModalTitleStyle.Render(fmt.Sprintf("Recent Chats (%d) - Page %d/%d", m.HistoryChatCount, m.HistoryPage+1, totalPages))

	var body string
	if m.HistoryErr != nil {
		errLine := lipgloss.NewStyle().Width(styles.ContentWidth).Render(styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", m.HistoryErr)))
		body = errLine
	} else if len(m.HistoryChats) == 0 {
		body = styles.ModalItemStyle.Render(lipgloss.NewStyle().Foreground(styles.HintColor).Render("No chats yet"))
	} else {
		items := make([]string, 0, len(m.HistoryChats))
		for i, chat := range m.HistoryChats {
			isSelected := i == m.HistorySelectedIdx
			cursor := "  "
			if isSelected {
				cursor = "> "
			}
			timeStr := RelativeTime(time.Unix(chat.UpdatedAtUnix, 0))
			prompt := PromptPreview(chat.LastUserPrompt)
			if prompt == "" {
				prompt = "(no prompt)"
			}
			availableWidth := styles.ContentWidth - 2 - len(cursor) - 1 - len(timeStr)
			prompt = TruncateRunes(prompt, availableWidth)

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

func (m *Model) RenderShortcutsModal() string {
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

func (m *Model) RenderBottomBar() string {
	// 1. Mode Badge (Left)
	modeBadge := "CHAT"
	// modeColor := "#81D4FA" // Light Blue
	// if m.AppMode == models.ModeAgent {
	// 	modeBadge = "AGENT"
	// 	modeColor = "#CE93D8" // Light Purple
	// }
	// mode := lipgloss.NewStyle().
	// 	Bold(true).
	// 	Foreground(lipgloss.Color("#FFFFFF")).
	// 	Background(lipgloss.Color(modeColor)).
	// 	Padding(0, 1).
	// 	Render(modeBadge)
	// Temporarily commenting out color logic to simplify porting
	// Re-enabling with logic
	modeColor := "#81D4FA"
	if m.AppMode == 1 { // models.ModeAgent
		modeBadge = "AGENT"
		modeColor = "#CE93D8"
	}
	mode := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#FFFFFF")).
		Background(lipgloss.Color(modeColor)).
		Padding(0, 1).
		Render(modeBadge)

	// 2. Working Directory
	cwdDisplay := m.WorkingDir
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(cwdDisplay, home) {
		cwdDisplay = "~" + cwdDisplay[len(home):]
	}
	cwdDisplay = TruncateRunes(cwdDisplay, 30)
	cwd := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#888888")).
		Render(cwdDisplay)

	// 3. Model Name
	modelName := TruncateRunes(m.CurrentModel.Name, 25)
	model := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#B39DDB")).
		Render(modelName)

	// 4. Context Window
	maxCtx := m.GetMaxContextTokens()
	contextPct := 0
	if m.ContextTokens > 0 && maxCtx > 0 {
		contextPct = int(float64(m.ContextTokens) / float64(maxCtx) * 100)
	}
	ctxColor := "#888888"
	if contextPct > 80 {
		ctxColor = "#EF9A9A" // Red
	} else if contextPct > 60 {
		ctxColor = "#FFF59D" // Yellow
	}

	ctxText := fmt.Sprintf("%d%% (%dk/%dk)", contextPct, m.ContextTokens/1000, maxCtx/1000)
	ctx := lipgloss.NewStyle().
		Foreground(lipgloss.Color(ctxColor)).
		Render(ctxText)

	// 5. Token Usage
	tokens := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#666666")).
		Render(fmt.Sprintf("In:%d Out:%d", m.InputTokens, m.OutputTokens))

	// 6. Help Hint (Far Right)
	help := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#555555")).
		Render("Help: ^S")

	// Spacer to push items apart
	// We want: [Mode] [CWD] [Model] ...spacer... [Context] [Tokens] [Help]

	leftSide := lipgloss.JoinHorizontal(lipgloss.Center, mode, "  ", cwd, "  ", model)
	rightSide := lipgloss.JoinHorizontal(lipgloss.Center, ctx, "  ", tokens, "  ", help)

	// Calculate available space for spacer
	availableWidth := m.WindowWidth - lipgloss.Width(leftSide) - lipgloss.Width(rightSide) - 2 // -2 for padding
	if availableWidth < 0 {
		availableWidth = 0
	}
	spacer := strings.Repeat(" ", availableWidth)

	bar := lipgloss.JoinHorizontal(lipgloss.Center, leftSide, spacer, rightSide)

	return lipgloss.NewStyle().
		Width(m.WindowWidth).
		BorderTop(true).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("#333333")).
		Padding(0, 1).
		Render(bar)
}

func (m *Model) RenderPendingFiles() string {
	if len(m.PendingFiles) == 0 {
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
	for _, file := range m.PendingFiles {
		chips = append(chips, chipStyle.Render("📄 "+filepath.Base(file)))
	}

	return labelStyle.Render("Attached: ") + strings.Join(chips, " ")
}

func (m *Model) RenderFileSuggestions() string {
	if !m.FileSuggestOpen || len(m.FileSuggestions) == 0 {
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

	for i, suggestion := range m.FileSuggestions {
		// Check if it's a directory
		info, _ := os.Stat(suggestion)
		display := suggestion
		if info != nil && info.IsDir() {
			display = suggestion + "/"
		}

		if i == m.FileSuggestIdx {
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

func GetWelcomeScreen(width, height int) string {
	art := `
 ╭──────────────────────────────────────────────────────────────╮
 │                                                              │
 │    ▄▄▄       ██▀███   ▄████▄   ▄▄▄       ███▄    █  ▓█████   │
 │   ▒████▄    ▓██ ▒ ██▒▒██▀ ▀█  ▒████▄     ██ ▀█   █  ▓█   ▀   │
 │   ▒██  ▀█▄  ▓██ ░▄█ ▒▒▓█    ▄ ▒██  ▀█▄  ▓██  ▀█ ██▒ ▒███     │
 │   ░██▄▄▄▄██ ▒██▀▀█▄  ▒▓▓▄ ▄██▒░██▄▄▄▄██ ▓██▒  ▐▌██▒ ▒▓█  ▄   │
 │    ▓█   ▓██▒░██▓ ▒██▒▒ ▓███▀ ░ ▓█   ▓██▒▒██░   ▓██░ ░▒████▒  │
 │    ▒▒   ▓▒█░░ ▒▓ ░▒▓░░ ░▒ ▒  ░ ▒▒   ▓▒█░░ ▒░   ▒ ▒  ░░ ▒░ ░  │
 │     ▒   ▒▒ ░  ░▒ ░ ▒░  ░  ▒     ▒   ▒▒ ░░ ░░   ░ ▒░  ░ ░  ░  │
 │     ░   ▒     ░░   ░ ░          ░   ▒      ░   ░ ░     ░     │
 │         ░  ░   ░     ░ ░            ░  ░         ░     ░  ░  │
 │                                                              │
 ╰──────────────────────────────────────────────────────────────╯
`
	subtitle := "“Code is not just logic, it is the architecture of imagination.”"

	styledArt := styles.WelcomeArtStyle.Render(art)
	styledSubtitle := styles.WelcomeSubtitleStyle.Italic(true).Render(subtitle)

	content := lipgloss.JoinVertical(lipgloss.Center, styledArt, "", styledSubtitle)

	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

func (m *Model) UpdateViewport() {
	if len(m.Messages) == 0 && !m.Loading {
		m.Viewport.SetContent(GetWelcomeScreen(m.Viewport.Width, m.Viewport.Height))
		return
	}

	content := strings.Join(m.Messages, "\n\n")
	if m.Loading {
		statusText := " Generating..."
		if m.ExecutingTool != "" {
			statusText = fmt.Sprintf(" %s...", m.ExecutingTool)
		}

		// Build loading message with completed tool actions
		var loadingParts []string
		loadingParts = append(loadingParts, styles.AiLabelStyle.Render("ARCANE"))

		// Show completed tool actions
		if len(m.ToolActions) > 0 {
			loadingParts = append(loadingParts, FormatToolActions(m.ToolActions))
		}

		// Show current status (spinner + status text)
		loadingParts = append(loadingParts, fmt.Sprintf("%s%s", m.Spinner.View(), statusText))

		loadingMsg := strings.Join(loadingParts, "\n")
		if len(m.Messages) > 0 {
			content = content + "\n\n" + loadingMsg
		} else {
			content = loadingMsg
		}
	}
	m.Viewport.SetContent(content)
	m.Viewport.GotoBottom()
}

func (m *Model) View() string {
	var content string

	// Render file suggestions popup if open
	fileSuggestPopup := m.RenderFileSuggestions()
	pendingFilesDisplay := m.RenderPendingFiles()

	// Full-width chat with bottom bar
	// chatWidth := m.WindowWidth - 2
	// Input takes full window width minus padding, not limited by chatWidth
	inputWidth := m.WindowWidth - 4
	inputBox := styles.InputBoxStyle.Width(inputWidth).Render(m.TextInput.View())

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
		m.Viewport.View(),
		"",
		inputSection,
	)
	chatArea := lipgloss.PlaceHorizontal(m.WindowWidth, lipgloss.Center, chatContent)
	bottomBar := m.RenderBottomBar()

	content = lipgloss.JoinVertical(lipgloss.Left, chatArea, bottomBar)

	if m.HistoryOpen {
		modal := m.RenderHistorySelector()
		modal = styles.ModalStyle.Width(ModalWidth).Render(modal)

		return lipgloss.NewStyle().
			Background(lipgloss.Color("rgba(0,0,0,0.7)")).
			Render(lipgloss.Place(
				m.WindowWidth,
				m.WindowHeight,
				lipgloss.Center,
				lipgloss.Center,
				modal,
			))
	}

	if m.ModelSelectorOpen {
		modal := m.RenderModelSelector()
		modal = styles.ModalStyle.Width(ModalWidth).Render(modal)

		return lipgloss.NewStyle().
			Background(lipgloss.Color("rgba(0,0,0,0.7)")).
			Render(lipgloss.Place(
				m.WindowWidth,
				m.WindowHeight,
				lipgloss.Center,
				lipgloss.Center,
				modal,
			))
	}

	if m.ShortcutsOpen {
		modal := m.RenderShortcutsModal()
		modal = styles.ModalStyle.Width(ModalWidth).Render(modal)

		return lipgloss.NewStyle().
			Background(lipgloss.Color("rgba(0,0,0,0.7)")).
			Render(lipgloss.Place(
				m.WindowWidth,
				m.WindowHeight,
				lipgloss.Center,
				lipgloss.Center,
				modal,
			))
	}

	return content
}
