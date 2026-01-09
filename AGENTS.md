# Arcane Agent Guide

This project is a Terminal User Interface (TUI) chat application built with Go, interfacing with the Gemini AI model.

## 🛠 Essential Commands

- **Run**: `go run main.go`
- **Build**: `go build -o arcane`
- **Tidy Modules**: `go mod tidy`

## 🕹 Keyboard Shortcuts / Commands

- **Ctrl+C / Esc**: Quit application.
- **Ctrl+N**: Start a new chat session and clear the screen.
- **/clear** or **/reset**: (Typed in input) Start a new chat session and clear the screen.

## ⚙️ Environment Variables

- `GEMINI_API_KEY`: **Required**. Must be set to interact with the Google Generative AI API.

## 🏗 Project Structure

- `main.go`: Contains the entire TUI logic, Bubble Tea model, and Gemini integration.
- `go.mod` / `go.sum`: Go module definitions and dependencies.
- `arcane`: Compiled binary (ignored by git usually, but present in this environment).

## 🧩 Key Technologies

- **[Bubble Tea](https://github.com/charmbracelet/bubbletea)**: The TUI framework used for the application lifecycle.
- **[Lip Gloss](https://github.com/charmbracelet/lipgloss)**: Used for terminal styling and layout.
- **[Glamour](https://github.com/charmbracelet/glamour)**: Used for rendering Markdown in the terminal.
- **[Google Generative AI Go SDK](https://github.com/google/generative-ai-go)**: SDK for interacting with the Gemini API.

## 💡 Important Context

- **Model**: Default model is `gemini-2.5-flash` (defined as `modelName` in `main.go`).
- **TUI Components**:
    - `textinput`: Used for user input.
    - `viewport`: Used for displaying chat history with scrolling.
- **Styling**: Styles for User and AI responses are defined globally in `main.go`.

## ⚠️ Gotchas

- **Alt Screen**: The application uses `tea.WithAltScreen()`.
- **Terminal Handshake**: Some terminals respond to background color queries with "garbage" text like `]11;rgb:...` or cursor reports `[1;1R`. `main.go` includes a filter in the `Update` loop to detect and clear this from the text input automatically.
- Markdown rendering is handled by Glamour and includes word-wrapping based on window size.
- Errors are handled within the Bubble Tea `Update` loop and displayed in the chat viewport.
