package internal

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(dbPath string) (*Repository, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, err
	}

	repo := &Repository{db: db}
	if err := repo.migrate(); err != nil {
		db.Close()
		return nil, err
	}

	return repo, nil
}

func (r *Repository) Close() error {
	return r.db.Close()
}

func (r *Repository) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		claude_session_id TEXT,
		title TEXT,
		working_directory TEXT,
		created_at INTEGER NOT NULL,
		updated_at INTEGER NOT NULL
	);

	CREATE TABLE IF NOT EXISTS messages (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		role TEXT NOT NULL,
		content TEXT NOT NULL,
		tool_calls TEXT,
		created_at INTEGER NOT NULL,
		FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
	);

	CREATE INDEX IF NOT EXISTS idx_messages_session_id ON messages(session_id);
	`
	_, err := r.db.Exec(schema)
	return err
}

// Session operations

func (r *Repository) CreateSession(title, workingDir *string) (*Session, error) {
	now := time.Now()
	session := &Session{
		ID:               uuid.New().String(),
		Title:            title,
		WorkingDirectory: workingDir,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	_, err := r.db.Exec(
		`INSERT INTO sessions (id, claude_session_id, title, working_directory, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		session.ID, session.ClaudeSessionID, session.Title, session.WorkingDirectory,
		session.CreatedAt.Unix(), session.UpdatedAt.Unix(),
	)
	if err != nil {
		return nil, err
	}

	return session, nil
}

func (r *Repository) GetSession(id string) (*Session, error) {
	row := r.db.QueryRow(
		`SELECT id, claude_session_id, title, working_directory, created_at, updated_at
		 FROM sessions WHERE id = ?`, id,
	)

	var session Session
	var createdAt, updatedAt int64
	err := row.Scan(
		&session.ID, &session.ClaudeSessionID, &session.Title,
		&session.WorkingDirectory, &createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	session.CreatedAt = time.Unix(createdAt, 0)
	session.UpdatedAt = time.Unix(updatedAt, 0)
	return &session, nil
}

func (r *Repository) ListSessions() ([]Session, error) {
	rows, err := r.db.Query(
		`SELECT id, claude_session_id, title, working_directory, created_at, updated_at
		 FROM sessions ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		var createdAt, updatedAt int64
		if err := rows.Scan(
			&s.ID, &s.ClaudeSessionID, &s.Title,
			&s.WorkingDirectory, &createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		s.CreatedAt = time.Unix(createdAt, 0)
		s.UpdatedAt = time.Unix(updatedAt, 0)
		sessions = append(sessions, s)
	}

	return sessions, rows.Err()
}

func (r *Repository) UpdateSessionClaudeID(id, claudeSessionID string) error {
	_, err := r.db.Exec(
		`UPDATE sessions SET claude_session_id = ?, updated_at = ? WHERE id = ?`,
		claudeSessionID, time.Now().Unix(), id,
	)
	return err
}

func (r *Repository) DeleteSession(id string) error {
	_, err := r.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	return err
}

// Message operations

func (r *Repository) CreateMessage(sessionID, role, content string, toolCalls json.RawMessage) (*Message, error) {
	now := time.Now()
	msg := &Message{
		ID:        uuid.New().String(),
		SessionID: sessionID,
		Role:      role,
		Content:   content,
		ToolCalls: toolCalls,
		CreatedAt: now,
	}

	var toolCallsStr *string
	if toolCalls != nil {
		s := string(toolCalls)
		toolCallsStr = &s
	}

	_, err := r.db.Exec(
		`INSERT INTO messages (id, session_id, role, content, tool_calls, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		msg.ID, msg.SessionID, msg.Role, msg.Content, toolCallsStr, msg.CreatedAt.Unix(),
	)
	if err != nil {
		return nil, err
	}

	// Update session's updated_at
	r.db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, now.Unix(), sessionID)

	return msg, nil
}

func (r *Repository) GetSessionMessages(sessionID string) ([]Message, error) {
	rows, err := r.db.Query(
		`SELECT id, session_id, role, content, tool_calls, created_at
		 FROM messages WHERE session_id = ? ORDER BY created_at ASC`, sessionID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []Message
	for rows.Next() {
		var m Message
		var toolCallsStr *string
		var createdAt int64
		if err := rows.Scan(&m.ID, &m.SessionID, &m.Role, &m.Content, &toolCallsStr, &createdAt); err != nil {
			return nil, err
		}
		m.CreatedAt = time.Unix(createdAt, 0)
		if toolCallsStr != nil {
			m.ToolCalls = json.RawMessage(*toolCallsStr)
		}
		messages = append(messages, m)
	}

	return messages, rows.Err()
}
