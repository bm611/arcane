package ui

import (
	"arcane/internal/models"
	"arcane/internal/styles"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/mattn/go-runewidth"
	"github.com/openai/openai-go/v3"
)

// GetFileSuggestions returns files/dirs matching a prefix, supporting subdirectory paths and recursive search
func GetFileSuggestions(prefix string) []string {
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

// ExtractFileMentions parses @filename mentions from input and returns clean text + file list
func ExtractFileMentions(input string) (cleanInput string, files []string) {
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

// BuildFileContext creates a context string with file contents
func BuildFileContext(files []string) string {
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

// GetAtPosition finds the @ mention being typed at cursor position
func GetAtPosition(input string, cursorPos int) (prefix string, startPos int, found bool) {
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

func TextareaCursorIndex(t textarea.Model) int {
	value := t.Value()
	row := t.Line()
	li := t.LineInfo()
	col := li.StartColumn + li.ColumnOffset
	return cursorIndexFromRowCol(value, row, col)
}

func TextareaCursorFromIndex(value string, index int) (row int, col int) {
	if index < 0 {
		index = 0
	}
	if index > len(value) {
		index = len(value)
	}

	lines := strings.Split(value, "\n")
	pos := 0
	for i, line := range lines {
		lineLen := len(line)
		if index <= pos+lineLen {
			row = i
			col = runeIndexForByteIndex(line, index-pos)
			return row, col
		}
		pos += lineLen + 1
	}

	if len(lines) == 0 {
		return 0, 0
	}
	row = len(lines) - 1
	col = utf8.RuneCountInString(lines[row])
	return row, col
}

func SetTextareaCursor(t *textarea.Model, row int, col int) {
	lineCount := t.LineCount()
	if lineCount == 0 {
		t.SetCursor(0)
		return
	}
	if row < 0 {
		row = 0
	}
	if row >= lineCount {
		row = lineCount - 1
	}

	for i := 0; i < 10000 && t.Line() > 0; i++ {
		t.CursorUp()
	}
	for i := 0; i < 10000 && t.Line() < row; i++ {
		t.CursorDown()
	}
	for i := 0; i < 10000 && t.Line() > row; i++ {
		t.CursorUp()
	}

	t.SetCursor(col)
}

func cursorIndexFromRowCol(value string, row int, col int) int {
	lines := strings.Split(value, "\n")
	if len(lines) == 0 {
		return 0
	}
	if row < 0 {
		row = 0
	}
	if row >= len(lines) {
		row = len(lines) - 1
	}

	index := 0
	for i := 0; i < row; i++ {
		index += len(lines[i]) + 1
	}
	index += byteIndexForRuneColumn(lines[row], col)
	return index
}

func byteIndexForRuneColumn(s string, col int) int {
	if col <= 0 {
		return 0
	}
	count := 0
	for i := range s {
		if count >= col {
			return i
		}
		count++
	}
	return len(s)
}

func runeIndexForByteIndex(s string, idx int) int {
	if idx <= 0 {
		return 0
	}
	count := 0
	for i := range s {
		if i >= idx {
			return count
		}
		count++
	}
	return count
}

func WrappedLineCount(value string, width int) int {
	if width <= 0 {
		return 1
	}
	lines := strings.Split(value, "\n")
	if len(lines) == 0 {
		return 1
	}
	count := 0
	for _, line := range lines {
		w := runewidth.StringWidth(line)
		if w == 0 {
			count++
			continue
		}
		count += (w-1)/width + 1
	}
	return count
}

// EstimateTokens provides a rough token count estimate based on character count
func EstimateTokens(s string) int {
	return len(s) / CharsPerToken
}

// EstimateHistoryTokens calculates approximate token count for the entire history
func EstimateHistoryTokens(history []openai.ChatCompletionMessageParamUnion) int {
	total := 0
	for _, msg := range history {
		total += EstimateMessageTokens(msg)
	}
	return total
}

// EstimateMessageTokens estimates tokens for a single message
func EstimateMessageTokens(msg openai.ChatCompletionMessageParamUnion) int {
	// Extract content based on message type using JSON marshaling
	data, err := json.Marshal(msg)
	if err != nil {
		return 0
	}
	return EstimateTokens(string(data))
}

// TruncateToolResult shortens a tool result while preserving useful info
func TruncateToolResult(toolName, result string) string {
	lines := strings.Split(result, "\n")
	lineCount := len(lines)

	if len(result) <= TruncatedResultSize {
		return result
	}

	// Create a summary based on tool type
	preview := result
	if len(preview) > TruncatedResultSize {
		preview = preview[:TruncatedResultSize]
	}

	return fmt.Sprintf("[%s: %d lines] %s...", toolName, lineCount, strings.TrimSpace(preview))
}

// CompactHistory reduces history size by truncating old tool results
func CompactHistory(history []openai.ChatCompletionMessageParamUnion, maxTokens int) []openai.ChatCompletionMessageParamUnion {
	currentTokens := EstimateHistoryTokens(history)

	// If under limit, no compaction needed
	if currentTokens < maxTokens {
		return history
	}

	// Keep first message (initial task) and last N messages intact
	if len(history) <= RecentMessagesKeep+1 {
		return history
	}

	compacted := make([]openai.ChatCompletionMessageParamUnion, 0, len(history))

	// Always keep first message
	compacted = append(compacted, history[0])

	// Process middle messages - truncate tool results
	middleEnd := len(history) - RecentMessagesKeep
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
			if content, ok := rawMsg["content"].(string); ok && len(content) > TruncatedResultSize {
				// Create truncated tool message
				truncated := TruncateToolResult("tool", content)
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

func IsKnownToolName(name string) bool {
	switch name {
	case "ls", "read", "write", "edit", "glob", "grep", "bash":
		return true
	default:
		return false
	}
}

func ParseInlineToolCall(content string) (name string, argsJSON string, ok bool) {
	content = strings.TrimSpace(content)
	if content == "" {
		return "", "", false
	}

	m := InlineToolCallRE.FindStringSubmatch(content)
	if len(m) != 3 {
		return "", "", false
	}

	name = strings.TrimSpace(m[1])
	argsJSON = strings.TrimSpace(m[2])
	if !IsKnownToolName(name) {
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

func LastToolResult(execs []ToolExecRecord, toolName string) (string, bool) {
	for i := len(execs) - 1; i >= 0; i-- {
		if execs[i].Name == toolName {
			return execs[i].Result, true
		}
	}
	return "", false
}

func FormatLsResultAsAnswer(lsResult string) string {
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

func CoerceAgentFinalContent(content string, execs []ToolExecRecord) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		if ls, ok := LastToolResult(execs, "ls"); ok {
			return FormatLsResultAsAnswer(ls)
		}
		return content
	}
	if _, _, ok := ParseInlineToolCall(trimmed); ok {
		if ls, ok := LastToolResult(execs, "ls"); ok {
			return FormatLsResultAsAnswer(ls)
		}
		return content
	}

	// If the model claims emptiness but ls found entries, prefer the ls result.
	low := strings.ToLower(trimmed)
	if strings.Contains(low, "no files") || strings.Contains(low, "directory appears to be empty") || strings.Contains(low, "directory is empty") {
		if ls, ok := LastToolResult(execs, "ls"); ok {
			ls = strings.TrimSpace(ls)
			if ls != "" && ls != "(empty directory)" {
				return FormatLsResultAsAnswer(ls)
			}
		}
	}

	return content
}

func PromptPreview(s string) string {
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

func TruncateRunes(s string, max int) string {
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

func RelativeTime(t time.Time) string {
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

func (m *Model) SyncModelViewportScroll() {
	// Height of each model item (1 line + padding)
	const itemHeight = 1
	// Header height (1 line + padding)
	const headerHeight = 1

	var currentY int
	var lastProvider string
	for i, mdl := range AvailableModels {
		itemStartY := currentY

		if mdl.Provider != lastProvider {
			if lastProvider != "" {
				currentY += 1 // Spacer
				itemStartY += 1
			}
			// Header is here, so this block starts at currentY
			itemStartY = currentY
			currentY += headerHeight
			lastProvider = mdl.Provider
		} else {
			// No header, item starts here
			itemStartY = currentY
		}

		if i == m.SelectedModelIndex {
			// If item bottom is below viewport, scroll down
			if currentY+itemHeight > m.ModelViewport.YOffset+m.ModelViewport.Height {
				m.ModelViewport.SetYOffset(currentY + itemHeight - m.ModelViewport.Height)
			}
			// If item top (or header top) is above viewport, scroll up
			if itemStartY < m.ModelViewport.YOffset {
				m.ModelViewport.SetYOffset(itemStartY)
			}
			break
		}
		currentY += itemHeight
	}
}

func FindModelByID(id string) (models.AIModel, int, bool) {
	for i, mdl := range AvailableModels {
		if mdl.ID == id {
			return mdl, i, true
		}
	}
	return models.AIModel{}, 0, false
}

func FormatUserMessage(content string, width int, isFirst bool) string {
	label := styles.UserLabelStyle.Render("YOU")
	msg := styles.UserMsgStyle.Width(width - 4).Render(content)
	if isFirst {
		return fmt.Sprintf("\n%s\n%s", label, msg)
	}
	return fmt.Sprintf("%s\n%s", label, msg)
}

func FormatAIMessage(content string) string {
	label := styles.AiLabelStyle.Render("ARCANE")
	msg := styles.AiMsgStyle.Render(content)
	return fmt.Sprintf("%s\n%s", label, msg)
}

func FormatToolActions(actions []models.ToolAction) string {
	var lines []string
	for _, action := range actions {
		icon := styles.ToolIconStyle.Render("→")
		name := styles.ToolNameStyle.Render(action.Summary)
		lines = append(lines, styles.ToolActionStyle.Render(fmt.Sprintf("%s %s", icon, name)))
	}
	return strings.Join(lines, "\n")
}

func FormatAIMessageWithTools(toolDisplay, content string) string {
	label := styles.AiLabelStyle.Render("ARCANE")
	msg := styles.AiMsgStyle.Render(content)
	return fmt.Sprintf("%s\n%s\n%s", label, toolDisplay, msg)
}
