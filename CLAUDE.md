# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Commands

```bash
# Install dependencies
go mod tidy

# Build
go build -o arcane

# Run (requires OPENROUTER_API_KEY)
export OPENROUTER_API_KEY="your-api-key"
./arcane          # Chat mode
./arcane --agent  # Agent mode (not yet implemented as a flag — toggle with Ctrl+A in-app)

# Run directly without building
go run main.go
```

There are no tests in this codebase currently.

## Architecture

Arcane is a Bubble Tea TUI application with a single `Model` struct driving all state. The program flow is the standard Bubble Tea MVU loop: `Init` → `Update` (msg dispatch) → `View` (render).

### Package layout

- **`main.go`** — entry point; creates the program, runs it, closes the DB on exit.
- **`internal/ui/`** — all TUI logic split across four files:
  - `types.go` — the central `Model` struct, all message types (`ResponseMsg`, `ToolCallMsg`, `ToolResultMsg`, `ErrMsg`), system prompts, available models list, and constants for context management.
  - `init.go` — `InitialModel()` (builds initial state, creates OpenAI client pointed at OpenRouter), `NewProgram()`, and `(*Model).Init()`.
  - `update.go` — `(*Model).Update()` (the full key/mouse/message dispatch), `(*Model).SendMessage()` (the API call / agent loop), and all DB persistence helpers.
  - `view.go` — `(*Model).View()` and rendering helpers (`UpdateViewport`, `UpdateModelSelectorContent`).
  - `helpers.go` — pure utility functions: message formatting, file mention parsing (`@filename`), token estimation, history compaction, textarea cursor math.
- **`internal/tools/tools.go`** — tool definitions (OpenAI function-call schema) and implementations for `read`, `write`, `edit`, `glob`, `grep`, `bash`, `ls`. All tools have output limits to control context size.
- **`internal/db/db.go`** — SQLite persistence via `modernc.org/sqlite`. DB is stored at `$XDG_CONFIG_HOME/arcane/arcane.db`. Schema: `chats` + `messages` tables, created automatically on first run.
- **`internal/models/models.go`** — shared domain types: `AppMode` (Chat/Agent), `AIModel`, `ChatListItem`, `DBMessage`, `ToolAction`.
- **`internal/styles/`** — Lip Gloss style definitions and theme constants.

### Key design decisions

**API client**: Uses the OpenAI Go SDK (`github.com/openai/openai-go/v3`) pointed at `https://openrouter.ai/api/v1`. All models are OpenRouter model IDs. The `AvailableModels` slice in `types.go` is the single source of truth for the model list.

**Two modes**: `ModeChat` does a single non-streaming API call. `ModeAgent` runs an agentic loop (up to `MaxToolIterations = 15` rounds) that calls tools and feeds results back until the model stops requesting tool calls. The loop runs inside the `SendMessage` command (a `tea.Cmd`) so it doesn't block the UI — progress is surfaced via `ToolCallMsg`/`ToolResultMsg` sent through `m.Program.Send(...)`.

**Context management**: `CompactHistory` in `helpers.go` truncates old tool-result messages when the estimated token count exceeds the model's context window. `DefaultContextTokens = 80000` is the fallback; `RecentMessagesKeep = 6` messages are always preserved in full.

**Inline tool call fallback**: Some providers (e.g., GLM) emit tool calls as plain text content (`name{...}`) rather than structured `tool_calls`. `ParseInlineToolCall` in `helpers.go` detects and handles this pattern.

**File mentions**: Typing `@filename` in the input triggers an autocomplete popup. On send, matched files are read and injected into the user message as a fenced code block context (`BuildFileContext`).

**SQLite**: Chat history is persisted per-session. Each new message creates/updates a `chats` row and appends to `messages`. `LoadChatFromDB` reconstructs both the display messages and the OpenAI history slice from stored rows.
