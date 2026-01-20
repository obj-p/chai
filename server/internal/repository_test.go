package internal

import (
	"os"
	"testing"
)

func setupTestRepo(t *testing.T) (*Repository, func()) {
	t.Helper()

	f, err := os.CreateTemp("", "chai-test-*.db")
	if err != nil {
		t.Fatalf("Failed to create temp file: %v", err)
	}
	f.Close()

	repo, err := NewRepository(f.Name())
	if err != nil {
		os.Remove(f.Name())
		t.Fatalf("Failed to create repository: %v", err)
	}

	cleanup := func() {
		repo.Close()
		os.Remove(f.Name())
	}

	return repo, cleanup
}

func TestRepository_CreateAndGetSession(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	title := "Test Session"
	workDir := "/tmp/test"

	session, err := repo.CreateSession(&title, &workDir)
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	if session.ID == "" {
		t.Error("Session ID should not be empty")
	}
	if session.Title == nil || *session.Title != title {
		t.Errorf("Title = %v, want %v", session.Title, title)
	}
	if session.WorkingDirectory == nil || *session.WorkingDirectory != workDir {
		t.Errorf("WorkingDirectory = %v, want %v", session.WorkingDirectory, workDir)
	}

	// Get the session
	got, err := repo.GetSession(session.ID)
	if err != nil {
		t.Fatalf("GetSession failed: %v", err)
	}

	if got.ID != session.ID {
		t.Errorf("ID = %v, want %v", got.ID, session.ID)
	}
}

func TestRepository_ListSessions(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	// Initially empty
	sessions, err := repo.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 0 {
		t.Errorf("Expected 0 sessions, got %d", len(sessions))
	}

	// Create sessions
	title1 := "Session 1"
	title2 := "Session 2"
	repo.CreateSession(&title1, nil)
	repo.CreateSession(&title2, nil)

	sessions, err = repo.ListSessions()
	if err != nil {
		t.Fatalf("ListSessions failed: %v", err)
	}
	if len(sessions) != 2 {
		t.Errorf("Expected 2 sessions, got %d", len(sessions))
	}
}

func TestRepository_DeleteSession(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	title := "To Delete"
	session, _ := repo.CreateSession(&title, nil)

	deleted, err := repo.DeleteSession(session.ID)
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	if !deleted {
		t.Error("Expected DeleteSession to return true for existing session")
	}

	_, err = repo.GetSession(session.ID)
	if err == nil {
		t.Error("Expected error getting deleted session")
	}

	// Test deleting non-existent session returns false
	deleted, err = repo.DeleteSession("nonexistent")
	if err != nil {
		t.Fatalf("DeleteSession failed: %v", err)
	}
	if deleted {
		t.Error("Expected DeleteSession to return false for non-existent session")
	}
}

func TestRepository_UpdateSessionClaudeID(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	title := "Test"
	session, _ := repo.CreateSession(&title, nil)

	claudeID := "claude-123"
	err := repo.UpdateSessionClaudeID(session.ID, claudeID)
	if err != nil {
		t.Fatalf("UpdateSessionClaudeID failed: %v", err)
	}

	got, _ := repo.GetSession(session.ID)
	if got.ClaudeSessionID == nil || *got.ClaudeSessionID != claudeID {
		t.Errorf("ClaudeSessionID = %v, want %v", got.ClaudeSessionID, claudeID)
	}
}

func TestRepository_Messages(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	title := "Test"
	session, _ := repo.CreateSession(&title, nil)

	// Create messages
	msg1, err := repo.CreateMessage(session.ID, "user", "Hello", nil)
	if err != nil {
		t.Fatalf("CreateMessage failed: %v", err)
	}
	if msg1.ID == "" {
		t.Error("Message ID should not be empty")
	}

	_, _ = repo.CreateMessage(session.ID, "assistant", "Hi there!", nil)

	// Get messages
	messages, err := repo.GetSessionMessages(session.ID)
	if err != nil {
		t.Fatalf("GetSessionMessages failed: %v", err)
	}
	if len(messages) != 2 {
		t.Errorf("Expected 2 messages, got %d", len(messages))
	}
	if messages[0].Content != "Hello" {
		t.Errorf("First message content = %v, want Hello", messages[0].Content)
	}
	if messages[1].Content != "Hi there!" {
		t.Errorf("Second message content = %v, want Hi there!", messages[1].Content)
	}

	// Verify cascade delete
	_, _ = repo.DeleteSession(session.ID)
	messages, _ = repo.GetSessionMessages(session.ID)
	if len(messages) != 0 {
		t.Errorf("Expected 0 messages after session delete, got %d", len(messages))
	}
}

func TestRepository_CreateEvent(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	title := "Test"
	session, _ := repo.CreateSession(&title, nil)
	promptID := session.ID + "-1"

	// Create first event
	event1, err := repo.CreateEvent(session.ID, promptID, "connected", []byte(`{"session_id":"test"}`))
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}
	if event1.Sequence != 1 {
		t.Errorf("Sequence = %d, want 1", event1.Sequence)
	}
	if event1.EventType != "connected" {
		t.Errorf("EventType = %s, want connected", event1.EventType)
	}

	// Create second event - sequence should auto-increment
	event2, err := repo.CreateEvent(session.ID, promptID, "claude", []byte(`{"type":"assistant"}`))
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}
	if event2.Sequence != 2 {
		t.Errorf("Sequence = %d, want 2", event2.Sequence)
	}

	// Create third event
	event3, err := repo.CreateEvent(session.ID, promptID, "done", []byte(`{"status":"complete"}`))
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}
	if event3.Sequence != 3 {
		t.Errorf("Sequence = %d, want 3", event3.Sequence)
	}
}

func TestRepository_GetEventsSince(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	title := "Test"
	session, _ := repo.CreateSession(&title, nil)
	promptID := session.ID + "-1"

	// Create events
	repo.CreateEvent(session.ID, promptID, "connected", []byte(`{}`))
	repo.CreateEvent(session.ID, promptID, "claude", []byte(`{}`))
	repo.CreateEvent(session.ID, promptID, "done", []byte(`{}`))

	// Get all events
	events, err := repo.GetEventsSince(session.ID, 0, promptID, 100)
	if err != nil {
		t.Fatalf("GetEventsSince failed: %v", err)
	}
	if len(events) != 3 {
		t.Errorf("Got %d events, want 3", len(events))
	}

	// Get events since sequence 1
	events, err = repo.GetEventsSince(session.ID, 1, promptID, 100)
	if err != nil {
		t.Fatalf("GetEventsSince failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("Got %d events, want 2", len(events))
	}
	if events[0].Sequence != 2 {
		t.Errorf("First event sequence = %d, want 2", events[0].Sequence)
	}

	// Test limit
	events, err = repo.GetEventsSince(session.ID, 0, promptID, 2)
	if err != nil {
		t.Fatalf("GetEventsSince failed: %v", err)
	}
	if len(events) != 2 {
		t.Errorf("Got %d events, want 2 (limited)", len(events))
	}
}

func TestRepository_StartNewPrompt(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	title := "Test"
	session, _ := repo.CreateSession(&title, nil)

	// Initial state should be idle
	if session.StreamStatus != StreamStatusIdle {
		t.Errorf("Initial StreamStatus = %s, want idle", session.StreamStatus)
	}

	// Start first prompt
	promptID1, err := repo.StartNewPrompt(session.ID)
	if err != nil {
		t.Fatalf("StartNewPrompt failed: %v", err)
	}
	if promptID1 != session.ID+"-1" {
		t.Errorf("PromptID = %s, want %s", promptID1, session.ID+"-1")
	}

	// Verify status changed to streaming
	updated, _ := repo.GetSession(session.ID)
	if updated.StreamStatus != StreamStatusStreaming {
		t.Errorf("StreamStatus = %s, want streaming", updated.StreamStatus)
	}

	// Try to start another prompt while streaming - should fail
	_, err = repo.StartNewPrompt(session.ID)
	if err != ErrSessionBusy {
		t.Errorf("Expected ErrSessionBusy, got %v", err)
	}

	// Complete the first prompt
	repo.UpdateSessionStreamStatus(session.ID, StreamStatusCompleted)

	// Start second prompt should work now
	promptID2, err := repo.StartNewPrompt(session.ID)
	if err != nil {
		t.Fatalf("StartNewPrompt failed: %v", err)
	}
	if promptID2 != session.ID+"-2" {
		t.Errorf("PromptID = %s, want %s", promptID2, session.ID+"-2")
	}
}

func TestRepository_StartNewPrompt_NotFound(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	_, err := repo.StartNewPrompt("nonexistent")
	if err != ErrSessionNotFound {
		t.Errorf("Expected ErrSessionNotFound, got %v", err)
	}
}

func TestRepository_SessionEvents_CascadeDelete(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	title := "Test"
	session, _ := repo.CreateSession(&title, nil)
	promptID := session.ID + "-1"

	// Create events
	repo.CreateEvent(session.ID, promptID, "connected", []byte(`{}`))
	repo.CreateEvent(session.ID, promptID, "done", []byte(`{}`))

	// Delete session
	repo.DeleteSession(session.ID)

	// Verify events are deleted
	events, _ := repo.GetEventsSince(session.ID, 0, "", 100)
	if len(events) != 0 {
		t.Errorf("Expected 0 events after cascade delete, got %d", len(events))
	}
}

func TestRepository_UpdateSessionStreamStatus(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	title := "Test"
	session, _ := repo.CreateSession(&title, nil)

	// Default should be idle
	if session.StreamStatus != StreamStatusIdle {
		t.Errorf("Initial StreamStatus = %s, want idle", session.StreamStatus)
	}

	// Update to streaming
	if err := repo.UpdateSessionStreamStatus(session.ID, StreamStatusStreaming); err != nil {
		t.Fatalf("UpdateSessionStreamStatus failed: %v", err)
	}

	updated, _ := repo.GetSession(session.ID)
	if updated.StreamStatus != StreamStatusStreaming {
		t.Errorf("StreamStatus = %s, want streaming", updated.StreamStatus)
	}

	// Update to completed
	if err := repo.UpdateSessionStreamStatus(session.ID, StreamStatusCompleted); err != nil {
		t.Fatalf("UpdateSessionStreamStatus failed: %v", err)
	}

	updated, _ = repo.GetSession(session.ID)
	if updated.StreamStatus != StreamStatusCompleted {
		t.Errorf("StreamStatus = %s, want completed", updated.StreamStatus)
	}
}

func TestRepository_GetLatestEventSequence(t *testing.T) {
	repo, cleanup := setupTestRepo(t)
	defer cleanup()

	title := "Test"
	session, _ := repo.CreateSession(&title, nil)
	promptID := session.ID + "-1"

	// No events yet
	seq, err := repo.GetLatestEventSequence(session.ID, promptID)
	if err != nil {
		t.Fatalf("GetLatestEventSequence failed: %v", err)
	}
	if seq != 0 {
		t.Errorf("Sequence = %d, want 0", seq)
	}

	// Create events
	repo.CreateEvent(session.ID, promptID, "connected", []byte(`{}`))
	repo.CreateEvent(session.ID, promptID, "claude", []byte(`{}`))
	repo.CreateEvent(session.ID, promptID, "done", []byte(`{}`))

	// Should be 3 now
	seq, err = repo.GetLatestEventSequence(session.ID, promptID)
	if err != nil {
		t.Fatalf("GetLatestEventSequence failed: %v", err)
	}
	if seq != 3 {
		t.Errorf("Sequence = %d, want 3", seq)
	}
}
