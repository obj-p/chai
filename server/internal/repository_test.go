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
