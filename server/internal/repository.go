package internal

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
)

var (
	// ErrSessionBusy is returned when attempting to start a new prompt on a session that is already streaming
	ErrSessionBusy = errors.New("session is busy")
	// ErrSessionNotFound is returned when a session does not exist
	ErrSessionNotFound = errors.New("session not found")
)

type Repository struct {
	db *sql.DB
}

func NewRepository(dbPath string) (*Repository, error) {
	db, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on&_journal_mode=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}

	// Limit connections to 1 for writes to avoid SQLite lock contention
	// SQLite handles concurrent reads well but only allows one writer at a time
	db.SetMaxOpenConns(1)

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

// Ping verifies database connectivity
func (r *Repository) Ping() error {
	return r.db.Ping()
}

func (r *Repository) migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS sessions (
		id TEXT PRIMARY KEY,
		claude_session_id TEXT,
		title TEXT,
		working_directory TEXT,
		stream_status TEXT DEFAULT 'idle',
		prompt_sequence INTEGER DEFAULT 0,
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

	CREATE TABLE IF NOT EXISTS session_events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		prompt_id TEXT NOT NULL,
		sequence INTEGER NOT NULL,
		event_type TEXT NOT NULL,
		data TEXT NOT NULL,
		created_at INTEGER NOT NULL,
		FOREIGN KEY (session_id) REFERENCES sessions(id) ON DELETE CASCADE
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_session_events_unique
		ON session_events(session_id, prompt_id, sequence);
	CREATE INDEX IF NOT EXISTS idx_session_events_session
		ON session_events(session_id);
	CREATE INDEX IF NOT EXISTS idx_session_events_created
		ON session_events(created_at);
	`
	if _, err := r.db.Exec(schema); err != nil {
		return err
	}

	// Add columns to existing sessions table (error-tolerant for existing DBs)
	// These will fail silently if columns already exist
	r.db.Exec(`ALTER TABLE sessions ADD COLUMN stream_status TEXT DEFAULT 'idle'`)
	r.db.Exec(`ALTER TABLE sessions ADD COLUMN prompt_sequence INTEGER DEFAULT 0`)

	// Backfill existing sessions with default values
	r.db.Exec(`UPDATE sessions SET stream_status = 'idle', prompt_sequence = 0 WHERE stream_status IS NULL`)

	return nil
}

// Session operations

func (r *Repository) CreateSession(title, workingDir *string) (*Session, error) {
	now := time.Now()
	session := &Session{
		ID:               uuid.New().String(),
		Title:            title,
		WorkingDirectory: workingDir,
		StreamStatus:     StreamStatusIdle,
		PromptSequence:   0,
		CreatedAt:        now,
		UpdatedAt:        now,
	}

	_, err := r.db.Exec(
		`INSERT INTO sessions (id, claude_session_id, title, working_directory, stream_status, prompt_sequence, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID, session.ClaudeSessionID, session.Title, session.WorkingDirectory,
		string(session.StreamStatus), session.PromptSequence,
		session.CreatedAt.Unix(), session.UpdatedAt.Unix(),
	)
	if err != nil {
		return nil, err
	}

	return session, nil
}

func (r *Repository) GetSession(id string) (*Session, error) {
	row := r.db.QueryRow(
		`SELECT id, claude_session_id, title, working_directory, stream_status, prompt_sequence, created_at, updated_at
		 FROM sessions WHERE id = ?`, id,
	)

	var session Session
	var streamStatus string
	var createdAt, updatedAt int64
	err := row.Scan(
		&session.ID, &session.ClaudeSessionID, &session.Title,
		&session.WorkingDirectory, &streamStatus, &session.PromptSequence,
		&createdAt, &updatedAt,
	)
	if err != nil {
		return nil, err
	}

	session.StreamStatus = StreamStatus(streamStatus)
	session.CreatedAt = time.Unix(createdAt, 0)
	session.UpdatedAt = time.Unix(updatedAt, 0)
	return &session, nil
}

func (r *Repository) ListSessions() ([]Session, error) {
	rows, err := r.db.Query(
		`SELECT id, claude_session_id, title, working_directory, stream_status, prompt_sequence, created_at, updated_at
		 FROM sessions ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []Session
	for rows.Next() {
		var s Session
		var streamStatus string
		var createdAt, updatedAt int64
		if err := rows.Scan(
			&s.ID, &s.ClaudeSessionID, &s.Title,
			&s.WorkingDirectory, &streamStatus, &s.PromptSequence,
			&createdAt, &updatedAt,
		); err != nil {
			return nil, err
		}
		s.StreamStatus = StreamStatus(streamStatus)
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

func (r *Repository) DeleteSession(id string) (bool, error) {
	result, err := r.db.Exec(`DELETE FROM sessions WHERE id = ?`, id)
	if err != nil {
		return false, err
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
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
	if _, err := r.db.Exec(`UPDATE sessions SET updated_at = ? WHERE id = ?`, now.Unix(), sessionID); err != nil {
		log.Printf("Warning: failed to update session updated_at for session %s: %v", sessionID, err)
	}

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

// Event operations for mobile backgrounding resilience
//
// Performance note: Each event is persisted in its own transaction to ensure
// atomic sequence generation. While this adds overhead, it's acceptable because:
// 1. SQLite with WAL mode handles small writes efficiently
// 2. SetMaxOpenConns(1) serializes writes, preventing lock contention
// 3. Events arrive sequentially from Claude CLI, not in bursts
// 4. Mobile catch-up requires complete event replay for UI reconstruction
//
// If performance becomes an issue with high-frequency events, consider:
// - Batching events (persist every N events or every Xms)
// - Using auto-increment ID as sequence instead of SELECT MAX + 1

// UpdateSessionStreamStatus updates the streaming status of a session
func (r *Repository) UpdateSessionStreamStatus(id string, status StreamStatus) error {
	_, err := r.db.Exec(
		`UPDATE sessions SET stream_status = ?, updated_at = ? WHERE id = ?`,
		string(status), time.Now().Unix(), id,
	)
	return err
}

// StartNewPrompt atomically starts a new prompt for a session.
// Returns the prompt ID (format: sessionID-sequence) or ErrSessionBusy if already streaming.
func (r *Repository) StartNewPrompt(sessionID string) (string, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()

	// Atomic update: only succeeds if not already streaming
	result, err := tx.Exec(
		`UPDATE sessions SET stream_status = 'streaming',
		 prompt_sequence = prompt_sequence + 1, updated_at = ?
		 WHERE id = ? AND stream_status != 'streaming'`,
		time.Now().Unix(), sessionID)
	if err != nil {
		return "", err
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return "", err
	}
	if rows == 0 {
		// Check if session exists
		var exists int
		err = tx.QueryRow(`SELECT 1 FROM sessions WHERE id = ?`, sessionID).Scan(&exists)
		if err == sql.ErrNoRows {
			return "", ErrSessionNotFound
		}
		return "", ErrSessionBusy
	}

	var seq int64
	if err := tx.QueryRow(`SELECT prompt_sequence FROM sessions WHERE id = ?`, sessionID).Scan(&seq); err != nil {
		return "", err
	}

	if err := tx.Commit(); err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%d", sessionID, seq), nil
}

// CreateEvent persists a single event with atomic sequence generation.
// Returns the created event with its assigned sequence number.
func (r *Repository) CreateEvent(sessionID, promptID, eventType string, data []byte) (*SessionEvent, error) {
	tx, err := r.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Get next sequence atomically
	var seq int64
	err = tx.QueryRow(
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM session_events
		 WHERE session_id = ? AND prompt_id = ?`, sessionID, promptID).Scan(&seq)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	result, err := tx.Exec(
		`INSERT INTO session_events (session_id, prompt_id, sequence, event_type, data, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, promptID, seq, eventType, string(data), now.Unix())
	if err != nil {
		return nil, err
	}

	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return &SessionEvent{
		ID:        id,
		SessionID: sessionID,
		PromptID:  promptID,
		Sequence:  seq,
		EventType: eventType,
		Data:      json.RawMessage(data),
		CreatedAt: now,
	}, nil
}

// GetEventsSince retrieves events after a given sequence number.
// If promptID is empty, returns events for all prompts in the session.
func (r *Repository) GetEventsSince(sessionID string, sinceSequence int64, promptID string, limit int) ([]SessionEvent, error) {
	var rows *sql.Rows
	var err error

	if promptID != "" {
		rows, err = r.db.Query(
			`SELECT id, session_id, prompt_id, sequence, event_type, data, created_at
			 FROM session_events
			 WHERE session_id = ? AND prompt_id = ? AND sequence > ?
			 ORDER BY sequence ASC
			 LIMIT ?`,
			sessionID, promptID, sinceSequence, limit)
	} else {
		rows, err = r.db.Query(
			`SELECT id, session_id, prompt_id, sequence, event_type, data, created_at
			 FROM session_events
			 WHERE session_id = ? AND sequence > ?
			 ORDER BY prompt_id, sequence ASC
			 LIMIT ?`,
			sessionID, sinceSequence, limit)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []SessionEvent
	for rows.Next() {
		var e SessionEvent
		var dataStr string
		var createdAt int64
		if err := rows.Scan(&e.ID, &e.SessionID, &e.PromptID, &e.Sequence, &e.EventType, &dataStr, &createdAt); err != nil {
			return nil, err
		}
		e.Data = json.RawMessage(dataStr)
		e.CreatedAt = time.Unix(createdAt, 0)
		events = append(events, e)
	}

	return events, rows.Err()
}

// GetLatestEventSequence returns the highest sequence number for a session/prompt.
// If promptID is empty, returns the highest sequence across all prompts.
func (r *Repository) GetLatestEventSequence(sessionID, promptID string) (int64, error) {
	var maxSeq sql.NullInt64
	var err error

	if promptID != "" {
		err = r.db.QueryRow(
			`SELECT MAX(sequence) FROM session_events WHERE session_id = ? AND prompt_id = ?`,
			sessionID, promptID).Scan(&maxSeq)
	} else {
		err = r.db.QueryRow(
			`SELECT MAX(sequence) FROM session_events WHERE session_id = ?`,
			sessionID).Scan(&maxSeq)
	}
	if err != nil {
		return 0, err
	}

	if !maxSeq.Valid {
		return 0, nil
	}
	return maxSeq.Int64, nil
}

// DeleteEventsForCompletedSessions deletes events for sessions that have completed streaming
// and are older than the specified duration.
func (r *Repository) DeleteEventsForCompletedSessions(olderThan time.Duration) (int64, error) {
	cutoff := time.Now().Add(-olderThan)
	result, err := r.db.Exec(
		`DELETE FROM session_events
		 WHERE session_id IN (
			 SELECT id FROM sessions
			 WHERE stream_status = ? OR stream_status = ?
		 )
		 AND created_at < ?`,
		string(StreamStatusCompleted), string(StreamStatusIdle), cutoff.Unix())
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}
