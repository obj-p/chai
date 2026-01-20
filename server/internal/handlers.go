package internal

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// ClaudeRunner interface for dependency injection
type ClaudeRunner interface {
	RunPrompt(ctx context.Context, sessionID string, claudeSessionID *string, prompt string, workingDir *string, onEvent func(line []byte) error) (string, error)
	SendPermissionResponse(sessionID, toolUseID, decision string) error
	KillProcess(sessionID string) error
}

type Handlers struct {
	repo          *Repository
	claude        ClaudeRunner
	promptTimeout time.Duration
}

func NewHandlers(repo *Repository, claude ClaudeRunner, promptTimeout time.Duration) *Handlers {
	return &Handlers{
		repo:          repo,
		claude:        claude,
		promptTimeout: promptTimeout,
	}
}

// Helper functions

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func parseJSON(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}

// Handlers

func (h *Handlers) Health(w http.ResponseWriter, r *http.Request) {
	if err := h.repo.Ping(); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "error", "error": "database unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handlers) ListSessions(w http.ResponseWriter, r *http.Request) {
	sessions, err := h.repo.ListSessions()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if sessions == nil {
		sessions = []Session{}
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (h *Handlers) CreateSession(w http.ResponseWriter, r *http.Request) {
	var req CreateSessionRequest
	if err := parseJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	var title, workDir *string
	if req.Title != "" {
		title = &req.Title
	}
	if req.WorkingDirectory != "" {
		workDir = &req.WorkingDirectory
	}

	session, err := h.repo.CreateSession(title, workDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, session)
}

func (h *Handlers) GetSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}

	session, err := h.repo.GetSession(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	messages, err := h.repo.GetSessionMessages(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if messages == nil {
		messages = []Message{}
	}

	writeJSON(w, http.StatusOK, SessionResponse{
		Session:  *session,
		Messages: messages,
	})
}

func (h *Handlers) DeleteSession(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}

	// Kill any running process
	h.claude.KillProcess(id)

	deleted, err := h.repo.DeleteSession(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if !deleted {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) Prompt(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}

	var req PromptRequest
	if err := parseJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if strings.TrimSpace(req.Prompt) == "" {
		writeError(w, http.StatusBadRequest, "prompt is required")
		return
	}

	// Get session to check if it exists and get claude session ID
	session, err := h.repo.GetSession(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Start new prompt - this handles concurrent request blocking atomically
	promptID, err := h.repo.StartNewPrompt(id)
	if err != nil {
		if errors.Is(err, ErrSessionBusy) {
			writeError(w, http.StatusConflict, "session is already streaming")
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Save user message
	if _, err := h.repo.CreateMessage(id, "user", req.Prompt, nil); err != nil {
		h.repo.UpdateSessionStreamStatus(id, StreamStatusIdle)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Set up SSE
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	flusher, ok := w.(http.Flusher)
	if !ok {
		h.repo.UpdateSessionStreamStatus(id, StreamStatusIdle)
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Flush headers immediately
	flusher.Flush()

	// Helper to persist and send SSE events
	sendEvent := func(eventType string, data any) error {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return err
		}

		// Persist the event first
		if _, err := h.repo.CreateEvent(id, promptID, eventType, jsonData); err != nil {
			log.Printf("Warning: failed to persist event for session %s: %v", id, err)
			// Continue even if persistence fails - client should still get the event
		}

		// Send to client
		_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", eventType, jsonData)
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	// Send initial connected event with prompt_id for reconnection
	if err := sendEvent("connected", map[string]string{"session_id": id, "prompt_id": promptID}); err != nil {
		log.Printf("Failed to send connected event: %v", err)
		h.repo.UpdateSessionStreamStatus(id, StreamStatusIdle)
		return
	}

	log.Printf("Starting Claude CLI for session %s, prompt %s", id, promptID)

	// Accumulate assistant content for saving
	var assistantContent strings.Builder
	var toolCalls []json.RawMessage

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), h.promptTimeout)
	defer cancel()

	// Run prompt with streaming
	claudeSessionID, runErr := h.claude.RunPrompt(
		ctx,
		id,
		session.ClaudeSessionID,
		req.Prompt,
		session.WorkingDirectory,
		func(line []byte) error {
			// Parse event type
			var event ClaudeEvent
			if err := json.Unmarshal(line, &event); err != nil {
				return sendEvent("error", map[string]string{"error": "invalid JSON from Claude"})
			}

			// Persist and forward the raw event
			if _, err := h.repo.CreateEvent(id, promptID, "claude", line); err != nil {
				log.Printf("Warning: failed to persist claude event for session %s: %v", id, err)
			}

			// Send to client
			_, writeErr := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", "claude", line)
			if writeErr != nil {
				return writeErr
			}
			flusher.Flush()

			// Accumulate content for assistant message
			switch event.Type {
			case "assistant":
				var msg AssistantMessage
				if err := json.Unmarshal(line, &msg); err == nil {
					for _, block := range msg.Message.Content {
						if block.Type == "text" {
							assistantContent.WriteString(block.Text)
						} else if block.Type == "tool_use" {
							toolCalls = append(toolCalls, line)
						}
					}
				}
			case "content_block_delta":
				var delta ContentBlockDelta
				if err := json.Unmarshal(line, &delta); err == nil {
					if delta.Delta.Type == "text_delta" {
						assistantContent.WriteString(delta.Delta.Text)
					}
				}
			}

			return nil
		},
	)

	log.Printf("Claude CLI finished for session %s, claudeSessionID=%s, err=%v", id, claudeSessionID, runErr)

	// Save assistant message if we got content
	if assistantContent.Len() > 0 {
		var toolCallsJSON json.RawMessage
		if len(toolCalls) > 0 {
			data, _ := json.Marshal(toolCalls)
			toolCallsJSON = data
		}
		if _, err := h.repo.CreateMessage(id, "assistant", assistantContent.String(), toolCallsJSON); err != nil {
			log.Printf("Warning: failed to save assistant message for session %s: %v", id, err)
		}
	}

	// Update Claude session ID if we got a new one
	if claudeSessionID != "" && (session.ClaudeSessionID == nil || *session.ClaudeSessionID != claudeSessionID) {
		if err := h.repo.UpdateSessionClaudeID(id, claudeSessionID); err != nil {
			log.Printf("Warning: failed to update Claude session ID for session %s: %v", id, err)
		}
	}

	// Handle errors and send final event
	if runErr != nil {
		log.Printf("Claude CLI error: %v", runErr)
		sendEvent("error", map[string]string{"error": runErr.Error()})
		h.repo.UpdateSessionStreamStatus(id, StreamStatusIdle)
		return
	}

	sendEvent("done", map[string]string{"status": "complete"})
	h.repo.UpdateSessionStreamStatus(id, StreamStatusCompleted)
}

func (h *Handlers) Approve(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}

	var req ApproveRequest
	if err := parseJSON(r, &req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}

	if req.ToolUseID == "" {
		writeError(w, http.StatusBadRequest, "tool_use_id is required")
		return
	}

	if req.Decision != "allow" && req.Decision != "deny" {
		writeError(w, http.StatusBadRequest, "decision must be 'allow' or 'deny'")
		return
	}

	if err := h.claude.SendPermissionResponse(id, req.ToolUseID, req.Decision); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}

// GetEvents retrieves persisted events for reconnection after mobile backgrounding
func (h *Handlers) GetEvents(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "missing session id")
		return
	}

	// Parse and validate query params
	sinceSeq, _ := strconv.ParseInt(r.URL.Query().Get("since_sequence"), 10, 64)
	promptID := r.URL.Query().Get("prompt_id")

	// Validate limit (default 100, max 1000)
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil {
			limit = parsed
		}
	}
	if limit < 1 {
		limit = 1
	}
	if limit > 1000 {
		limit = 1000
	}

	// Verify session exists
	session, err := h.repo.GetSession(id)
	if err == sql.ErrNoRows {
		writeError(w, http.StatusNotFound, "session not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Fetch events (request limit+1 to detect has_more)
	events, err := h.repo.GetEventsSince(id, sinceSeq, promptID, limit+1)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	hasMore := len(events) > limit
	if hasMore {
		events = events[:limit]
	}

	var lastSeq int64
	if len(events) > 0 {
		lastSeq = events[len(events)-1].Sequence
	}

	// Ensure events is not nil for JSON
	if events == nil {
		events = []SessionEvent{}
	}

	writeJSON(w, http.StatusOK, GetEventsResponse{
		Events:       events,
		LastSequence: lastSeq,
		HasMore:      hasMore,
		StreamStatus: session.StreamStatus,
	})
}
