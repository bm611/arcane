# Arcane

A minimal AI chat TUI built with Go using the [Bubble Tea](https://github.com/charmbracelet/bubbletea) framework.

![Go](https://img.shields.io/badge/Go-00ADD8?style=flat&logo=go&logoColor=white)

## Features

- Clean, responsive terminal interface with Markdown rendering
- Multi-model AI support with 7 different models to choose from
- Interactive model selector modal with provider color coding
- Theme-aware background colors for light/dark terminals
- Conversation history with token usage tracking
- Scrollable chat viewport with styled messages
- Optimized startup time
- Proper message history management across chat sessions

## Installation

```bash
git clone https://github.com/bm611/arcane.git
cd arcane
go mod tidy
go build -o arcane
```

## Usage

Set your OpenRouter API key:

```bash
export OPENROUTER_API_KEY="your-api-key"
```

Run the application:

```bash
./arcane
```

## Keyboard Shortcuts

| Key | Action |
|-----|--------|
| `Enter` | Send message |
| `Ctrl+B` | Toggle model selector modal |
| `↑` / `↓` | Navigate model selector (when open) |
| `Ctrl+N` | Start new chat session |
| `Ctrl+C` / `Esc` | Quit (or close modal) |

You can also type `/clear` or `/reset` to start a new session.

## Dependencies

- [Bubble Tea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lip Gloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [Glamour](https://github.com/charmbracelet/glamour) - Markdown rendering
- [OpenAI Go SDK](https://github.com/openai/openai-go) - API client (used with OpenRouter)

## License

MIT
