package db

import (
	"database/sql"
	"os"
	"path/filepath"

	"arcane/internal/models"
	_ "modernc.org/sqlite"
)

func OpenArcaneDB() (*sql.DB, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		homeDir, herr := os.UserHomeDir()
		if herr != nil {
			return nil, err
		}
		configDir = filepath.Join(homeDir, ".config")
	}

	dbDir := filepath.Join(configDir, "arcane")
	if err := os.MkdirAll(dbDir, 0o700); err != nil {
		return nil, err
	}

	dbPath := filepath.Join(dbDir, "arcane.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON;"); err != nil {
		_ = db.Close()
		return nil, err
	}

	schema := []string{
		`CREATE TABLE IF NOT EXISTS chats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			model_id TEXT NOT NULL,
			last_user_prompt TEXT NOT NULL DEFAULT ''
		);`,
		`CREATE TABLE IF NOT EXISTS messages (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			chat_id INTEGER NOT NULL,
			role TEXT NOT NULL,
			content TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			FOREIGN KEY(chat_id) REFERENCES chats(id) ON DELETE CASCADE
		);`,
		`CREATE INDEX IF NOT EXISTS idx_chats_updated_at ON chats(updated_at DESC);`,
		`CREATE INDEX IF NOT EXISTS idx_messages_chat_id ON messages(chat_id, id);`,
	}

	for _, stmt := range schema {
		if _, err := db.Exec(stmt); err != nil {
			_ = db.Close()
			return nil, err
		}
	}

	return db, nil
}

func CreateChat(db *sql.DB, nowUnix int64, modelID string) (int64, error) {
	res, err := db.Exec(
		"INSERT INTO chats(created_at, updated_at, model_id, last_user_prompt) VALUES(?, ?, ?, '')",
		nowUnix,
		nowUnix,
		modelID,
	)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func InsertDBMessage(db *sql.DB, chatID int64, role, content string, nowUnix int64) error {
	_, err := db.Exec(
		"INSERT INTO messages(chat_id, role, content, created_at) VALUES(?, ?, ?, ?)",
		chatID,
		role,
		content,
		nowUnix,
	)
	return err
}

func UpdateChatOnUser(db *sql.DB, chatID int64, nowUnix int64, modelID, lastUserPrompt string) error {
	_, err := db.Exec(
		"UPDATE chats SET updated_at = ?, model_id = ?, last_user_prompt = ? WHERE id = ?",
		nowUnix,
		modelID,
		lastUserPrompt,
		chatID,
	)
	return err
}

func TouchChat(db *sql.DB, chatID int64, nowUnix int64) error {
	_, err := db.Exec(
		"UPDATE chats SET updated_at = ? WHERE id = ?",
		nowUnix,
		chatID,
	)
	return err
}

func GetRecentChats(db *sql.DB, limit int) (int, []models.ChatListItem, error) {
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM chats").Scan(&count); err != nil {
		return 0, nil, err
	}

	rows, err := db.Query(
		"SELECT id, updated_at, last_user_prompt, model_id FROM chats ORDER BY updated_at DESC LIMIT ?",
		limit,
	)
	if err != nil {
		return 0, nil, err
	}
	defer rows.Close()

	items := make([]models.ChatListItem, 0, limit)
	for rows.Next() {
		var it models.ChatListItem
		if err := rows.Scan(&it.ID, &it.UpdatedAtUnix, &it.LastUserPrompt, &it.ModelID); err != nil {
			return 0, nil, err
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return 0, nil, err
	}

	return count, items, nil
}

func GetChatMessages(db *sql.DB, chatID int64) ([]models.DBMessage, error) {
	rows, err := db.Query(
		"SELECT role, content FROM messages WHERE chat_id = ? ORDER BY id ASC",
		chatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	msgs := []models.DBMessage{}
	for rows.Next() {
		var m models.DBMessage
		if err := rows.Scan(&m.Role, &m.Content); err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return msgs, nil
}
