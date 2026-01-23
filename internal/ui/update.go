package ui

import (
	"arcane/internal/db"
	"arcane/internal/models"
	"arcane/internal/styles"
	"arcane/internal/tools"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
	"github.com/openai/openai-go/v3"
)

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
		spCmd tea.Cmd
	)

	switch msg := msg.(type) {
	case spinner.TickMsg:
		m.Spinner, spCmd = m.Spinner.Update(msg)
		if m.Loading {
			m.UpdateViewport()
		}
		return m, spCmd

	case tea.KeyMsg:
		if m.HistoryOpen {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "ctrl+h":
				m.HistoryOpen = false
				m.HistoryErr = nil
				return m, nil
			case "up", "k":
				if len(m.HistoryChats) == 0 {
					return m, nil
				}
				m.HistorySelectedIdx--
				if m.HistorySelectedIdx < 0 {
					m.HistorySelectedIdx = len(m.HistoryChats) - 1
				}
				return m, nil
			case "down", "j":
				if len(m.HistoryChats) == 0 {
					return m, nil
				}
				m.HistorySelectedIdx++
				if m.HistorySelectedIdx >= len(m.HistoryChats) {
					m.HistorySelectedIdx = 0
				}
				return m, nil
			case "enter":
				if len(m.HistoryChats) == 0 {
					return m, nil
				}
				chat := m.HistoryChats[m.HistorySelectedIdx]
				if err := m.LoadChatFromDB(chat.ID, chat.ModelID); err != nil {
					m.HistoryErr = err
					return m, nil
				}
				m.HistoryOpen = false
				m.HistoryErr = nil
				return m, nil
			case "left", "h":
				if m.HistoryPage > 0 {
					m.HistoryPage--
					m.RefreshHistoryFromDB()
				}
				return m, nil
			case "right", "l":
				totalPages := (m.HistoryChatCount + HistoryPageSize - 1) / HistoryPageSize
				if m.HistoryPage < totalPages-1 {
					m.HistoryPage++
					m.RefreshHistoryFromDB()
				}
				return m, nil
			}
			return m, nil
		}

		if m.ModelSelectorOpen {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc":
				m.ModelSelectorOpen = false
				return m, nil
			case "ctrl+b":
				m.ModelSelectorOpen = false
				return m, nil
			case "up", "k":
				m.SelectedModelIndex--
				if m.SelectedModelIndex < 0 {
					m.SelectedModelIndex = len(AvailableModels) - 1
				}
				m.SyncModelViewportScroll()
				m.UpdateModelSelectorContent()
				return m, nil
			case "down", "j":
				m.SelectedModelIndex++
				if m.SelectedModelIndex >= len(AvailableModels) {
					m.SelectedModelIndex = 0
				}
				m.SyncModelViewportScroll()
				m.UpdateModelSelectorContent()
				return m, nil
			case "enter":
				m.CurrentModel = AvailableModels[m.SelectedModelIndex]
				m.ModelSelectorOpen = false
				return m, nil
			}
			return m, nil
		}

		if m.ShortcutsOpen {
			switch msg.String() {
			case "ctrl+c":
				return m, tea.Quit
			case "esc", "enter", "?", "ctrl+s":
				m.ShortcutsOpen = false
				return m, nil
			}
			return m, nil
		}

		if isNewlineShortcut(msg) {
			m.TextInput.InsertString("\n")
			m.FileSuggestOpen = false
			m.updateInputLayout()
			return m, nil
		}

		// File suggestion popup handling
		if m.FileSuggestOpen {
			switch msg.String() {
			case "esc":
				m.FileSuggestOpen = false
				return m, nil
			case "up", "ctrl+p":
				if len(m.FileSuggestions) > 0 {
					m.FileSuggestIdx--
					if m.FileSuggestIdx < 0 {
						m.FileSuggestIdx = len(m.FileSuggestions) - 1
					}
				}
				return m, nil
			case "down", "ctrl+n":
				if len(m.FileSuggestions) > 0 {
					m.FileSuggestIdx++
					if m.FileSuggestIdx >= len(m.FileSuggestions) {
						m.FileSuggestIdx = 0
					}
				}
				return m, nil
			case "tab", "enter":
				if len(m.FileSuggestions) > 0 && m.FileSuggestIdx < len(m.FileSuggestions) {
					selected := m.FileSuggestions[m.FileSuggestIdx]
					// Replace the @prefix with @selected
					val := m.TextInput.Value()
					cursorPos := TextareaCursorIndex(m.TextInput)
					prefix, startPos, found := GetAtPosition(val, cursorPos)
					if found {
						newVal := val[:startPos] + "@" + selected + " " + val[startPos+1+len(prefix):]
						newCursorIndex := startPos + len(selected) + 2
						m.TextInput.SetValue(newVal)
						row, col := TextareaCursorFromIndex(newVal, newCursorIndex)
						SetTextareaCursor(&m.TextInput, row, col)
					}
					m.FileSuggestOpen = false
				}
				return m, nil
			}
		}

		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			if m.FileSuggestOpen {
				m.FileSuggestOpen = false
				return m, nil
			}
			return m, tea.Quit

		case tea.KeyCtrlN:
			m.ResetSession()
			return m, nil

		case tea.KeyCtrlA:
			// Toggle between Chat and Agent mode
			if m.AppMode == models.ModeChat {
				m.AppMode = models.ModeAgent
			} else {
				m.AppMode = models.ModeChat
			}
			return m, nil

		case tea.KeyCtrlB:
			m.ModelSelectorOpen = true
			m.HistoryOpen = false
			m.ShortcutsOpen = false
			m.UpdateModelSelectorContent() // Initial render
			m.SyncModelViewportScroll()    // Initial scroll sync
			return m, nil

		case tea.KeyCtrlS: // Using Ctrl+S for shortcuts
			m.ShortcutsOpen = true
			m.ModelSelectorOpen = false
			m.HistoryOpen = false
			return m, nil

		case tea.KeyCtrlH:
			m.ModelSelectorOpen = false
			m.HistoryOpen = true
			m.ShortcutsOpen = false
			m.HistoryPage = 0
			m.RefreshHistoryFromDB()
			return m, nil

		case tea.KeyEnter:
			// If file suggestions are open, handle selection instead
			if m.FileSuggestOpen && len(m.FileSuggestions) > 0 {
				selected := m.FileSuggestions[m.FileSuggestIdx]
				val := m.TextInput.Value()
				cursorPos := TextareaCursorIndex(m.TextInput)
				prefix, startPos, found := GetAtPosition(val, cursorPos)
				if found {
					newVal := val[:startPos] + "@" + selected + " " + val[startPos+1+len(prefix):]
					newCursorIndex := startPos + len(selected) + 2
					m.TextInput.SetValue(newVal)
					row, col := TextareaCursorFromIndex(newVal, newCursorIndex)
					SetTextareaCursor(&m.TextInput, row, col)
				}
				m.FileSuggestOpen = false
				return m, nil
			}

			if m.Loading {
				return m, nil
			}
			input := m.TextInput.Value()
			if input == "" {
				return m, nil
			}

			if input == "/clear" || input == "/reset" {
				m.ResetSession()
				return m, nil
			}

			// Extract file mentions and build context
			cleanInput, files := ExtractFileMentions(input)
			m.AttachedFiles = files

			// Display message shows clean input but indicates attached files
			displayInput := cleanInput
			if len(files) > 0 {
				fileNames := make([]string, len(files))
				for i, f := range files {
					fileNames[i] = filepath.Base(f)
				}
				displayInput = fmt.Sprintf("%s\n📎 %s", cleanInput, strings.Join(fileNames, ", "))
			}

			m.Messages = append(m.Messages, FormatUserMessage(displayInput, m.Viewport.Width, len(m.Messages) == 0))
			if err := m.PersistUserMessage(input); err != nil {
				m.Messages = append(m.Messages, styles.ErrorStyle.Render(fmt.Sprintf("History error: %v", err)))
			}
			m.TextInput.Reset()
			m.updateInputLayout()
			m.FileSuggestOpen = false
			m.Loading = true
			m.UpdateViewport()

			return m, tea.Batch(m.SendMessage(input), m.Spinner.Tick)
		}

	case ToolCallMsg:
		m.ExecutingTool = msg.Name
		m.ToolArguments = msg.Arguments
		m.UpdateViewport()
		return m, nil

	case ToolResultMsg:
		m.ExecutingTool = ""
		m.ToolArguments = ""
		// Store the completed tool action for display
		m.ToolActions = append(m.ToolActions, models.ToolAction{
			Name:    msg.Name,
			Summary: msg.Summary,
		})
		m.UpdateViewport()
		return m, nil

	case ResponseMsg:
		m.Loading = false
		m.InputTokens += msg.PromptTokens
		m.OutputTokens += msg.CompletionTokens
		m.History = msg.History
		m.ContextTokens = msg.ContextTokens
		displayContent := msg.Content
		if m.Renderer != nil {
			rendered, _ := m.Renderer.Render(msg.Content)
			displayContent = strings.TrimSpace(rendered)
		}
		// If there were tool actions, prepend them to the message
		if len(m.ToolActions) > 0 {
			toolDisplay := FormatToolActions(m.ToolActions)
			m.Messages = append(m.Messages, FormatAIMessageWithTools(toolDisplay, displayContent))
		} else {
			m.Messages = append(m.Messages, FormatAIMessage(displayContent))
		}
		m.ToolActions = nil // Clear for next response
		if err := m.PersistAssistantMessage(msg.Content); err != nil {
			m.Messages = append(m.Messages, styles.ErrorStyle.Render(fmt.Sprintf("History error: %v", err)))
		}
		m.UpdateViewport()
		return m, nil

	case ErrMsg:
		m.Loading = false
		m.Err = msg
		m.Messages = append(m.Messages, styles.ErrorStyle.Render(fmt.Sprintf("Error: %v", msg)))
		m.UpdateViewport()
		return m, nil

	case tea.WindowSizeMsg:
		m.WindowWidth = msg.Width
		m.WindowHeight = msg.Height

		// Update modal dimensions
		ModalWidth = msg.Width - 10
		if ModalWidth > 60 {
			ModalWidth = 60
		}
		if ModalWidth < 30 {
			ModalWidth = 30
		}
		styles.ContentWidth = ModalWidth - 6

		// Update Viewport sizes
		m.ModelViewport.Width = styles.ContentWidth
		m.ModelViewport.Height = msg.Height - 15
		if m.ModelViewport.Height > 20 {
			m.ModelViewport.Height = 20
		}
		if m.ModelViewport.Height < 5 {
			m.ModelViewport.Height = 5
		}

		// Full width mode (no sidebar)
		// Reserve 2 lines for bottom bar + border
		chatWidth := msg.Width - 2
		m.Viewport.Width = chatWidth - 2

		m.updateInputLayout()
		glamourStyle := "dark"
		if !lipgloss.HasDarkBackground() {
			glamourStyle = "light"
		}
		m.Renderer, _ = glamour.NewTermRenderer(
			glamour.WithStylePath(glamourStyle),
			glamour.WithWordWrap(chatWidth-6),
		)
		m.UpdateViewport()
		return m, tea.Batch(tiCmd, vpCmd)
	}

	m.TextInput, tiCmd = m.TextInput.Update(msg)
	m.updateInputLayout()

	// Filter out terminal background color queries and cursor reference codes that leak into the input
	val := m.TextInput.Value()
	if strings.Contains(val, "]11;rgb:") || strings.Contains(val, "1;rgb:") || strings.Contains(val, "[1;1R") {
		m.TextInput.Reset()
	}

	// Check for @ file mention trigger
	val = m.TextInput.Value()
	cursorPos := TextareaCursorIndex(m.TextInput)
	if prefix, _, found := GetAtPosition(val, cursorPos); found {
		suggestions := GetFileSuggestions(prefix)
		if len(suggestions) > 0 {
			m.FileSuggestions = suggestions
			m.FileSuggestOpen = true
			m.FileSuggestIdx = 0
			m.FileSuggestPrefix = prefix
		} else {
			m.FileSuggestOpen = false
		}
	} else {
		m.FileSuggestOpen = false
	}

	// Update pending files display (files currently mentioned in input)
	_, m.PendingFiles = ExtractFileMentions(val)

	m.Viewport, vpCmd = m.Viewport.Update(msg)

	return m, tea.Batch(tiCmd, vpCmd)
}

func isNewlineShortcut(msg tea.KeyMsg) bool {
	switch msg.String() {
	case "shift+enter", "shift+return", "ctrl+j", "ctrl+enter", "alt+enter":
		return true
	default:
		return false
	}
}

func (m *Model) updateInputLayout() {
	if m.WindowWidth == 0 || m.WindowHeight == 0 {
		return
	}

	inputWidth := m.WindowWidth - 6
	if inputWidth < 20 {
		inputWidth = 20
	}
	contentWidth := inputWidth - 2
	if contentWidth < 1 {
		contentWidth = 1
	}

	maxInputHeight := 6
	lineCount := WrappedLineCount(m.TextInput.Value(), contentWidth)
	if lineCount < 1 {
		lineCount = 1
	}
	if lineCount > maxInputHeight {
		lineCount = maxInputHeight
	}

	m.TextInput.MaxHeight = maxInputHeight
	m.TextInput.SetWidth(inputWidth)
	m.TextInput.SetHeight(lineCount)

	inputBoxHeight := m.TextInput.Height() + 2
	reserved := inputBoxHeight + 5
	viewportHeight := m.WindowHeight - reserved
	if viewportHeight < 5 {
		viewportHeight = 5
	}
	m.Viewport.Height = viewportHeight
}

func (m *Model) ResetSession() {
	m.Messages = []string{}
	m.History = []openai.ChatCompletionMessageParamUnion{}
	m.CurrentChatID = 0
	m.InputTokens = 0
	m.OutputTokens = 0
	m.ContextTokens = 0
	m.HistoryOpen = false
	m.HistoryErr = nil
	m.Viewport.SetContent(GetWelcomeScreen(m.Viewport.Width, m.Viewport.Height))
	m.Viewport.GotoTop()
	m.TextInput.Reset()
	m.updateInputLayout()
}

func (m *Model) RefreshHistoryFromDB() {
	m.HistoryErr = nil
	m.HistoryChats = nil
	m.HistorySelectedIdx = 0

	if m.DBErr != nil {
		m.HistoryErr = m.DBErr
		return
	}
	if m.DB == nil {
		m.HistoryErr = fmt.Errorf("history database not initialized")
		return
	}

	offset := m.HistoryPage * HistoryPageSize
	count, chats, err := db.GetRecentChats(m.DB, HistoryPageSize, offset)
	if err != nil {
		m.HistoryErr = err
		return
	}
	m.HistoryChatCount = count
	m.HistoryChats = chats
}

func (m *Model) PersistUserMessage(content string) error {
	if m.DBErr != nil {
		return m.DBErr
	}
	if m.DB == nil {
		return fmt.Errorf("history database not initialized")
	}

	nowUnix := time.Now().Unix()
	if m.CurrentChatID == 0 {
		id, err := db.CreateChat(m.DB, nowUnix, m.CurrentModel.ID)
		if err != nil {
			return err
		}
		m.CurrentChatID = id
	}

	if err := db.InsertDBMessage(m.DB, m.CurrentChatID, models.RoleUser, content, nowUnix); err != nil {
		return err
	}
	return db.UpdateChatOnUser(m.DB, m.CurrentChatID, nowUnix, m.CurrentModel.ID, PromptPreview(content))
}

func (m *Model) PersistAssistantMessage(content string) error {
	if m.CurrentChatID == 0 {
		return nil
	}
	if m.DBErr != nil {
		return m.DBErr
	}
	if m.DB == nil {
		return fmt.Errorf("history database not initialized")
	}

	nowUnix := time.Now().Unix()
	if err := db.InsertDBMessage(m.DB, m.CurrentChatID, models.RoleAssistant, content, nowUnix); err != nil {
		return err
	}
	return db.TouchChat(m.DB, m.CurrentChatID, nowUnix)
}

func (m *Model) LoadChatFromDB(chatID int64, modelID string) error {
	if m.DBErr != nil {
		return m.DBErr
	}
	if m.DB == nil {
		return fmt.Errorf("history database not initialized")
	}

	msgs, err := db.GetChatMessages(m.DB, chatID)
	if err != nil {
		return err
	}

	if modelID != "" {
		if mdl, idx, ok := FindModelByID(modelID); ok {
			m.CurrentModel = mdl
			m.SelectedModelIndex = idx
		} else {
			m.CurrentModel = models.AIModel{ID: modelID, Name: modelID, Provider: "Unknown"}
			m.SelectedModelIndex = 0
		}
	}

	m.CurrentChatID = chatID
	m.Loading = false
	m.InputTokens = 0
	m.OutputTokens = 0
	m.Messages = []string{}
	m.History = []openai.ChatCompletionMessageParamUnion{}

	for _, msg := range msgs {
		switch msg.Role {
		case models.RoleUser:
			m.Messages = append(m.Messages, FormatUserMessage(msg.Content, m.Viewport.Width, len(m.Messages) == 0))
			m.History = append(m.History, openai.UserMessage(msg.Content))
		case models.RoleAssistant:
			displayContent := msg.Content
			if m.Renderer != nil {
				rendered, _ := m.Renderer.Render(msg.Content)
				displayContent = strings.TrimSpace(rendered)
			}
			m.Messages = append(m.Messages, FormatAIMessage(displayContent))
			m.History = append(m.History, openai.AssistantMessage(msg.Content))
		}
	}

	m.UpdateViewport()
	return nil
}

func (m *Model) SendMessage(input string) tea.Cmd {
	// Capture attached files before returning the command
	attachedFiles := m.AttachedFiles
	m.AttachedFiles = nil // Clear for next message

	return func() tea.Msg {
		ctx := context.Background()

		// Build system prompt based on mode
		var systemPrompt string
		if m.AppMode == models.ModeAgent {
			cwd, _ := os.Getwd()
			systemPrompt = fmt.Sprintf(AgentSystemPrompt, cwd)
		} else {
			systemPrompt = ChatSystemPrompt
		}

		// Extract clean input and build file context
		cleanInput, _ := ExtractFileMentions(input)
		fileContext := BuildFileContext(attachedFiles)

		// Combine user message with file context
		userMessage := cleanInput
		if fileContext != "" {
			userMessage = cleanInput + fileContext
		}

		// Build history with system prompt
		history := []openai.ChatCompletionMessageParamUnion{
			openai.SystemMessage(systemPrompt),
		}
		history = append(history, m.History...)
		history = append(history, openai.UserMessage(userMessage))

		var totalPromptTokens int64
		var totalCompletionTokens int64

		// Chat mode: single API call without tools
		if m.AppMode == models.ModeChat {
			resp, err := m.Client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
				Model:    m.CurrentModel.ID,
				Messages: history,
			})
			if err != nil {
				return ErrMsg(err)
			}

			if len(resp.Choices) == 0 {
				return ErrMsg(fmt.Errorf("empty response from model"))
			}

			// Remove system message from history before storing
			storedHistory := history[1:]
			storedHistory = append(storedHistory, resp.Choices[0].Message.ToParam())

			return ResponseMsg{
				Content:          resp.Choices[0].Message.Content,
				PromptTokens:     resp.Usage.PromptTokens,
				CompletionTokens: resp.Usage.CompletionTokens,
				History:          storedHistory,
				ContextTokens:    EstimateHistoryTokens(storedHistory),
			}
		}

		// Agent mode: agentic loop with tools
		var toolExecs []ToolExecRecord
		iteration := 0
		for {
			iteration++

			// Compact history if approaching context limit
			history = CompactHistory(history, m.GetMaxContextTokens())

			resp, err := m.Client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
				Model:    m.CurrentModel.ID,
				Messages: history,
				Tools:    tools.Definitions,
			})
			if err != nil {
				return ErrMsg(err)
			}

			totalPromptTokens += resp.Usage.PromptTokens
			totalCompletionTokens += resp.Usage.CompletionTokens

			if len(resp.Choices) == 0 {
				return ErrMsg(fmt.Errorf("empty response from model"))
			}

			choice := resp.Choices[0]
			inlineName, inlineArgs, inlineOK := ParseInlineToolCall(choice.Message.Content)

			// Some providers/models include speculative user-facing content alongside tool calls.
			// Keeping that content in history can anchor the model into incorrect answers even
			// after tool results arrive, so we drop it for tool-call messages.
			assistantMsg := choice.Message
			if len(assistantMsg.ToolCalls) > 0 || inlineOK {
				assistantMsg.Content = ""
			}
			history = append(history, assistantMsg.ToParam())

			if iteration >= MaxToolIterations {
				content := choice.Message.Content
				if len(choice.Message.ToolCalls) > 0 || inlineOK {
					content = content + "\n\n*[Stopped after " + fmt.Sprint(MaxToolIterations) + " tool iterations]*"
				}
				content = CoerceAgentFinalContent(content, toolExecs)
				storedHistory := history[1:]
				return ResponseMsg{
					Content:          content,
					PromptTokens:     totalPromptTokens,
					CompletionTokens: totalCompletionTokens,
					History:          storedHistory,
					ContextTokens:    EstimateHistoryTokens(storedHistory),
				}
			}

			// Handle tool calls - notify UI for each tool
			if len(choice.Message.ToolCalls) > 0 {
				for _, tc := range choice.Message.ToolCalls {
					if m.Program != nil {
						m.Program.Send(ToolCallMsg{Name: tc.Function.Name, Arguments: tc.Function.Arguments})
					}

					result, err := tools.ExecuteTool(tc.Function.Name, tc.Function.Arguments)
					if err != nil {
						result = fmt.Sprintf("error: %v", err)
					}
					toolExecs = append(toolExecs, ToolExecRecord{Name: tc.Function.Name, Args: tc.Function.Arguments, Result: result})
					history = append(history, openai.ToolMessage(tc.ID, result))

					if m.Program != nil {
						summary := tools.GenerateToolSummary(tc.Function.Name, tc.Function.Arguments, result)
						m.Program.Send(ToolResultMsg{Name: tc.Function.Name, Result: result, Summary: summary})
					}
				}
				continue
			}

			// GLM-style inline tool call fallback (e.g. `ls{}` in content with no tool_calls)
			if inlineOK {
				if m.Program != nil {
					m.Program.Send(ToolCallMsg{Name: inlineName, Arguments: inlineArgs})
				}
				result, err := tools.ExecuteTool(inlineName, inlineArgs)
				if err != nil {
					result = fmt.Sprintf("error: %v", err)
				}
				toolExecs = append(toolExecs, ToolExecRecord{Name: inlineName, Args: inlineArgs, Result: result})
				history = append(history, openai.AssistantMessage(fmt.Sprintf("Tool %s result:\n%s", inlineName, result)))
				if m.Program != nil {
					summary := tools.GenerateToolSummary(inlineName, inlineArgs, result)
					m.Program.Send(ToolResultMsg{Name: inlineName, Result: result, Summary: summary})
				}
				continue
			}

			content := CoerceAgentFinalContent(choice.Message.Content, toolExecs)
			storedHistory := history[1:]
			return ResponseMsg{
				Content:          content,
				PromptTokens:     totalPromptTokens,
				CompletionTokens: totalCompletionTokens,
				History:          storedHistory,
				ContextTokens:    EstimateHistoryTokens(storedHistory),
			}
		}
	}
}
