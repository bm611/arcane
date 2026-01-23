# Codebase Overview

This document provides a high-level overview of the file structure and the purpose of each file in the `arcane` project.

## Root Directory

| File | Description |
| :--- | :--- |
| `AGENTS.md` | Guide for AI agents working on this project. Contains essential commands, shortcuts, and environment setup. |
| `README.md` | General project documentation, features, installation, and usage instructions. |
| `main.go` | The entry point of the application. Initializes the Bubble Tea program and handles graceful shutdown/cleanup. |
| `go.mod` / `go.sum` | Go module definitions and dependency lock files. |

## `internal/`

### `internal/db/`

| File | Description |
| :--- | :--- |
| `db.go` | Handles SQLite database initialization, schema migration, and all database operations (creating chats, storing messages, retrieving history). |

### `internal/models/`

| File | Description |
| :--- | :--- |
| `models.go` | Defines core data structures shared across the application, such as `AIModel`, `ChatListItem`, `DBMessage`, `ToolAction`, and application modes (`AppMode`). |

### `internal/styles/`

| File | Description |
| :--- | :--- |
| `styles.go` | Centralized definition of Lip Gloss styles for the TUI, including colors, borders, and text formatting for various UI elements. |

### `internal/tools/`

| File | Description |
| :--- | :--- |
| `tools.go` | Defines the tools available to the AI agent (read, write, edit, glob, grep, bash, ls) and implements their execution logic. Includes OpenAI tool definitions. |

### `internal/ui/`

| File | Description |
| :--- | :--- |
| `init.go` | Contains `InitialModel` setup, environment variable checks (API keys), and `NewProgram` initialization. |
| `types.go` | Defines UI-specific types (`Model` struct, messages like `ResponseMsg`, `ToolResultMsg`), constants (prompts, configuration), and the list of `AvailableModels`. |
| `update.go` | The core event loop (`Update` function) for Bubble Tea. Handles keyboard input, state transitions, tool execution flows, and API response processing. |
| `view.go` | Handles the rendering logic (`View` function) for the application. Includes methods for rendering the chat viewport, modals (history, model selector, shortcuts), bottom bar, and file suggestion popups. |
| `helpers.go` | Utility functions for file suggestions (`GetFileSuggestions`), token estimation, text formatting, and helper logic for tool result presentation. |

## Key Concepts

- **Bubble Tea**: The application uses the [Bubble Tea](https://github.com/charmbracelet/bubbletea) framework (ELM architecture).
    - **Model**: Stored in `internal/ui/types.go` (`Model` struct).
    - **Update**: Handled in `internal/ui/update.go`.
    - **View**: Handled in `internal/ui/view.go`.
- **Agent Mode**: The application supports an "Agent Mode" (toggled via `Ctrl+A`) where the AI has access to the tools defined in `internal/tools/tools.go`.
- **Database**: A local SQLite database is used to persist chat history.
