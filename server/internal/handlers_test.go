package internal

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

// mockClaudeManager implements a testable Claude manager
type mockClaudeManager struct {
	events    []string // JSON lines to emit
	sessionID string   // Claude session ID to return
	err       error    // Error to return
}

func (m *mockClaudeManager) RunPrompt(
	ctx context.Context,
	sessionID string,
	claudeSessionID *string,
	prompt string,
	onEvent func(line []byte) error,
) (string, error) {
	if m.err != nil {
		return "", m.err
	}

	for _, event := range m.events {
		select {
		case <-ctx.Done():
			return m.sessionID, ctx.Err()
		default:
			if err := onEvent([]byte(event)); err != nil {
				return m.sessionID, err
			}
		}
	}

	return m.sessionID, nil
}

func (m *mockClaudeManager) SendPermissionResponse(sessionID, toolUseID, decision string) error {
	return nil
}

func (m *mockClaudeManager) KillProcess(sessionID string) error {
	return nil
}

// ClaudeRunner interface for dependency injection
type ClaudeRunner interface {
	RunPrompt(ctx context.Context, sessionID string, claudeSessionID *string, prompt string, onEvent func(line []byte) error) (string, error)
	SendPermissionResponse(sessionID, toolUseID, decision string) error
	KillProcess(sessionID string) error
}

func setupTestServer(t *testing.T) (*Repository, *Handlers, func()) {
	t.Helper()

	f, err := os.CreateTemp("", "chai-handlers-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	f.Close()

	repo, err := NewRepository(f.Name())
	if err != nil {
		os.Remove(f.Name())
		t.Fatalf("Failed to create repository: %v", err)
	}

	claude := NewClaudeManager("/tmp", "claude")
	handlers := NewHandlers(repo, claude)

	cleanup := func() {
		repo.Close()
		os.Remove(f.Name())
	}

	return repo, handlers, cleanup
}

func TestHandlers_Health(t *testing.T) {
	_, handlers, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()

	handlers.Health(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
}

func TestHandlers_CreateSession(t *testing.T) {
	_, handlers, cleanup := setupTestServer(t)
	defer cleanup()

	body := `{"title":"Test Session","working_directory":"/tmp"}`
	req := httptest.NewRequest("POST", "/api/sessions", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	handlers.CreateSession(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var session Session
	json.NewDecoder(resp.Body).Decode(&session)
	if session.ID == "" {
		t.Error("Session ID should not be empty")
	}
	if session.Title == nil || *session.Title != "Test Session" {
		t.Errorf("Title = %v, want Test Session", session.Title)
	}
}

func TestHandlers_ListSessions(t *testing.T) {
	repo, handlers, cleanup := setupTestServer(t)
	defer cleanup()

	// Create some sessions
	title1 := "Session 1"
	title2 := "Session 2"
	repo.CreateSession(&title1, nil)
	repo.CreateSession(&title2, nil)

	req := httptest.NewRequest("GET", "/api/sessions", nil)
	w := httptest.NewRecorder()

	handlers.ListSessions(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var sessions []Session
	json.NewDecoder(resp.Body).Decode(&sessions)
	if len(sessions) != 2 {
		t.Errorf("Got %d sessions, want 2", len(sessions))
	}
}

func TestHandlers_GetSession(t *testing.T) {
	repo, handlers, cleanup := setupTestServer(t)
	defer cleanup()

	title := "Test"
	session, _ := repo.CreateSession(&title, nil)
	repo.CreateMessage(session.ID, "user", "Hello", nil)

	req := httptest.NewRequest("GET", "/api/sessions/"+session.ID, nil)
	req.SetPathValue("id", session.ID)
	w := httptest.NewRecorder()

	handlers.GetSession(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var result SessionResponse
	json.NewDecoder(resp.Body).Decode(&result)
	if result.Session.ID != session.ID {
		t.Errorf("Session ID = %v, want %v", result.Session.ID, session.ID)
	}
	if len(result.Messages) != 1 {
		t.Errorf("Got %d messages, want 1", len(result.Messages))
	}
}

func TestHandlers_GetSession_NotFound(t *testing.T) {
	_, handlers, cleanup := setupTestServer(t)
	defer cleanup()

	req := httptest.NewRequest("GET", "/api/sessions/nonexistent", nil)
	req.SetPathValue("id", "nonexistent")
	w := httptest.NewRecorder()

	handlers.GetSession(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
}

func TestHandlers_DeleteSession(t *testing.T) {
	repo, handlers, cleanup := setupTestServer(t)
	defer cleanup()

	title := "To Delete"
	session, _ := repo.CreateSession(&title, nil)

	req := httptest.NewRequest("DELETE", "/api/sessions/"+session.ID, nil)
	req.SetPathValue("id", session.ID)
	w := httptest.NewRecorder()

	handlers.DeleteSession(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	// Verify deleted
	_, err := repo.GetSession(session.ID)
	if err == nil {
		t.Error("Session should be deleted")
	}
}

func TestHandlers_Prompt_ValidationErrors(t *testing.T) {
	_, handlers, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"invalid json", "not json", http.StatusBadRequest},
		{"empty prompt", `{"prompt":""}`, http.StatusBadRequest},
		{"whitespace prompt", `{"prompt":"   "}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/sessions/test/prompt", strings.NewReader(tt.body))
			req.SetPathValue("id", "test")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.Prompt(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

func TestHandlers_Approve_ValidationErrors(t *testing.T) {
	_, handlers, cleanup := setupTestServer(t)
	defer cleanup()

	tests := []struct {
		name       string
		body       string
		wantStatus int
	}{
		{"invalid json", "not json", http.StatusBadRequest},
		{"missing tool_use_id", `{"decision":"allow"}`, http.StatusBadRequest},
		{"invalid decision", `{"tool_use_id":"123","decision":"maybe"}`, http.StatusBadRequest},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("POST", "/api/sessions/test/approve", strings.NewReader(tt.body))
			req.SetPathValue("id", "test")
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			handlers.Approve(w, req)

			if w.Code != tt.wantStatus {
				t.Errorf("Status = %d, want %d", w.Code, tt.wantStatus)
			}
		})
	}
}

// SSE parsing helper
type sseEvent struct {
	Event string
	Data  string
}

func parseSSEEvents(r io.Reader) []sseEvent {
	var events []sseEvent
	scanner := bufio.NewScanner(r)
	var currentEvent sseEvent

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			currentEvent.Event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			currentEvent.Data = strings.TrimPrefix(line, "data: ")
		} else if line == "" && currentEvent.Event != "" {
			events = append(events, currentEvent)
			currentEvent = sseEvent{}
		}
	}

	return events
}

func TestSSE_EventFormat(t *testing.T) {
	// Test SSE event formatting
	var buf bytes.Buffer
	w := &buf

	event := "test"
	data := map[string]string{"message": "hello"}
	jsonData, _ := json.Marshal(data)

	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, jsonData)

	events := parseSSEEvents(&buf)
	if len(events) != 1 {
		t.Fatalf("Got %d events, want 1", len(events))
	}
	if events[0].Event != "test" {
		t.Errorf("Event = %v, want test", events[0].Event)
	}

	var parsed map[string]string
	json.Unmarshal([]byte(events[0].Data), &parsed)
	if parsed["message"] != "hello" {
		t.Errorf("Data.message = %v, want hello", parsed["message"])
	}
}

// Integration test helper - tests full SSE flow with mock
func TestHandlers_Prompt_SSEFlow(t *testing.T) {
	repo, handlers, cleanup := setupTestServer(t)
	defer cleanup()

	// Create session
	title := "SSE Test"
	session, _ := repo.CreateSession(&title, nil)

	// We can't easily test the full SSE flow with the real Claude manager
	// because it spawns a process. Instead, verify the setup works.
	req := httptest.NewRequest("POST", "/api/sessions/"+session.ID+"/prompt",
		strings.NewReader(`{"prompt":"test"}`))
	req.SetPathValue("id", session.ID)
	req.Header.Set("Content-Type", "application/json")

	// Create a context with timeout to prevent hanging
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	req = req.WithContext(ctx)

	w := httptest.NewRecorder()
	handlers.Prompt(w, req)

	// Check that SSE headers were set
	resp := w.Result()
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Content-Type = %v, want text/event-stream", contentType)
	}

	// The request will timeout/error because Claude CLI isn't running,
	// but we should at least get the connected event
	body := w.Body.String()
	if !strings.Contains(body, "event: connected") {
		t.Error("Expected connected event in response")
	}
}
