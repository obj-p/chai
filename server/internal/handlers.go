package internal

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"
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
	id := r.PathValue("id")
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
	id := r.PathValue("id")
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
	id := r.PathValue("id")
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

	// Save user message
	if _, err := h.repo.CreateMessage(id, "user", req.Prompt, nil); err != nil {
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
		writeError(w, http.StatusInternalServerError, "streaming not supported")
		return
	}

	// Flush headers immediately
	flusher.Flush()

	// Helper to send SSE events
	sendEvent := func(event string, data any) error {
		jsonData, err := json.Marshal(data)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	// Send initial connected event
	if err := sendEvent("connected", map[string]string{"session_id": id}); err != nil {
		log.Printf("Failed to send connected event: %v", err)
		return
	}

	log.Printf("Starting Claude CLI for session %s", id)

	// Accumulate assistant content for saving
	var assistantContent strings.Builder
	var toolCalls []json.RawMessage

	// Create context with timeout
	ctx, cancel := context.WithTimeout(r.Context(), h.promptTimeout)
	defer cancel()

	// Run prompt with streaming
	claudeSessionID, err := h.claude.RunPrompt(
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

			// Forward the raw event
			if err := sendEvent("claude", json.RawMessage(line)); err != nil {
				return err
			}

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

	log.Printf("Claude CLI finished for session %s, claudeSessionID=%s, err=%v", id, claudeSessionID, err)

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

	if err != nil {
		log.Printf("Claude CLI error: %v", err)
		sendEvent("error", map[string]string{"error": err.Error()})
		return
	}

	sendEvent("done", map[string]string{"status": "complete"})
}

func (h *Handlers) Approve(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
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
