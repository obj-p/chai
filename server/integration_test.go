// Integration tests for the Chai server.
//
// These tests require the Claude CLI to be installed and configured.
// Run with: go test -tags=integration -v
//
// For manual testing without the test framework:
//
//	# Terminal 1: Start the server
//	go run ./cmd/server -port 8080
//
//	# Terminal 2: Test endpoints
//	curl http://localhost:8080/health
//	SESSION=$(curl -s -X POST http://localhost:8080/api/sessions \
//	  -H "Content-Type: application/json" \
//	  -d '{"title":"Test"}' | jq -r '.id')
//	curl -N http://localhost:8080/api/sessions/$SESSION/prompt \
//	  -H "Content-Type: application/json" \
//	  -d '{"prompt":"Say hello in exactly 3 words"}'

//go:build integration

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"chai/server/internal"
)

var serverURL = "http://localhost:18080"

func TestMain(m *testing.M) {
	// Check if Claude CLI is available
	if _, err := exec.LookPath("claude"); err != nil {
		fmt.Println("Skipping integration tests: Claude CLI not found")
		os.Exit(0)
	}

	// Build the server binary first
	buildCmd := exec.Command("go", "build", "-o", "/tmp/chai-test-server", "./cmd/server")
	buildCmd.Stdout = os.Stdout
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		fmt.Printf("Failed to build server: %v\n", err)
		os.Exit(1)
	}
	defer os.Remove("/tmp/chai-test-server")

	// Start the server
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	tmpDB, _ := os.CreateTemp("", "chai-integration-*.db")
	tmpDB.Close()
	defer os.Remove(tmpDB.Name())

	cmd := exec.CommandContext(ctx, "/tmp/chai-test-server",
		"-port", "18080",
		"-db", tmpDB.Name(),
		"-workdir", os.TempDir(),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		fmt.Printf("Failed to start server: %v\n", err)
		os.Exit(1)
	}

	// Wait for server to be ready
	ready := false
	for i := 0; i < 30; i++ {
		resp, err := http.Get(serverURL + "/health")
		if err == nil && resp.StatusCode == 200 {
			resp.Body.Close()
			ready = true
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !ready {
		fmt.Println("Server failed to start")
		cancel()
		os.Exit(1)
	}

	// Run tests
	code := m.Run()

	// Cleanup
	cancel()
	cmd.Wait()

	os.Exit(code)
}

func TestIntegration_Health(t *testing.T) {
	resp, err := http.Get(serverURL + "/health")
	if err != nil {
		t.Fatalf("Health check failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	var body map[string]string
	json.NewDecoder(resp.Body).Decode(&body)
	if body["status"] != "ok" {
		t.Errorf("status = %v, want ok", body["status"])
	}
}

func TestIntegration_SessionCRUD(t *testing.T) {
	// Create session
	createBody := `{"title":"Integration Test","working_directory":"/tmp"}`
	resp, err := http.Post(serverURL+"/api/sessions", "application/json",
		strings.NewReader(createBody))
	if err != nil {
		t.Fatalf("Create session failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Errorf("Create status = %d, want %d", resp.StatusCode, http.StatusCreated)
	}

	var session internal.Session
	json.NewDecoder(resp.Body).Decode(&session)
	if session.ID == "" {
		t.Fatal("Session ID should not be empty")
	}

	// List sessions
	resp, err = http.Get(serverURL + "/api/sessions")
	if err != nil {
		t.Fatalf("List sessions failed: %v", err)
	}
	resp.Body.Close()

	// Get session
	resp, err = http.Get(serverURL + "/api/sessions/" + session.ID)
	if err != nil {
		t.Fatalf("Get session failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Get status = %d, want %d", resp.StatusCode, http.StatusOK)
	}

	// Delete session
	req, _ := http.NewRequest("DELETE", serverURL+"/api/sessions/"+session.ID, nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("Delete session failed: %v", err)
	}
	resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("Delete status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}

	// Verify deleted
	resp, _ = http.Get(serverURL + "/api/sessions/" + session.ID)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Get deleted status = %d, want %d", resp.StatusCode, http.StatusNotFound)
	}
	resp.Body.Close()
}

func TestIntegration_SSE_Prompt(t *testing.T) {
	// Create session
	createBody := `{"title":"SSE Test"}`
	resp, err := http.Post(serverURL+"/api/sessions", "application/json",
		strings.NewReader(createBody))
	if err != nil {
		t.Fatalf("Create session failed: %v", err)
	}

	var session internal.Session
	json.NewDecoder(resp.Body).Decode(&session)
	resp.Body.Close()

	// Send prompt and read SSE stream
	promptBody := `{"prompt":"Say exactly: Hello World"}`
	req, _ := http.NewRequest("POST", serverURL+"/api/sessions/"+session.ID+"/prompt",
		strings.NewReader(promptBody))
	req.Header.Set("Content-Type", "application/json")

	// Use a client with timeout
	client := &http.Client{Timeout: 60 * time.Second}
	resp, err = client.Do(req)
	if err != nil {
		t.Fatalf("Prompt request failed: %v", err)
	}
	defer resp.Body.Close()

	// Check SSE headers
	contentType := resp.Header.Get("Content-Type")
	if contentType != "text/event-stream" {
		t.Errorf("Content-Type = %v, want text/event-stream", contentType)
	}

	// Parse SSE events
	events := readSSEEvents(resp.Body)

	// Should have at least connected and done events
	if len(events) < 2 {
		t.Errorf("Got %d events, want at least 2", len(events))
	}

	// First event should be connected
	if len(events) > 0 && events[0].Event != "connected" {
		t.Errorf("First event = %v, want connected", events[0].Event)
	}

	// Last event should be done or error
	lastEvent := events[len(events)-1]
	if lastEvent.Event != "done" && lastEvent.Event != "error" {
		t.Errorf("Last event = %v, want done or error", lastEvent.Event)
	}

	// Should have claude events in between
	hasClaudeEvent := false
	for _, e := range events {
		if e.Event == "claude" {
			hasClaudeEvent = true
			break
		}
	}
	if !hasClaudeEvent {
		t.Log("Warning: No claude events received (Claude CLI may not have responded)")
	}

	// Cleanup
	req, _ = http.NewRequest("DELETE", serverURL+"/api/sessions/"+session.ID, nil)
	http.DefaultClient.Do(req)
}

type sseEvent struct {
	Event string
	Data  string
}

func readSSEEvents(r io.Reader) []sseEvent {
	var events []sseEvent
	scanner := bufio.NewScanner(r)
	var current sseEvent

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "event: ") {
			current.Event = strings.TrimPrefix(line, "event: ")
		} else if strings.HasPrefix(line, "data: ") {
			current.Data = strings.TrimPrefix(line, "data: ")
		} else if line == "" && current.Event != "" {
			events = append(events, current)
			current = sseEvent{}
		}
	}

	return events
}

// TestIntegration_SSE_PermissionFlow tests the permission approval flow.
// This test requires a prompt that triggers a tool use (like file operations).
func TestIntegration_SSE_PermissionFlow(t *testing.T) {
	t.Skip("Skipping permission flow test - requires interactive tool approval")

	// This test would:
	// 1. Create a session
	// 2. Send a prompt that requires tool approval (e.g., "create a file called test.txt")
	// 3. Read SSE events until we get a permission_request
	// 4. POST to /approve with the tool_use_id
	// 5. Continue reading events until done
}

// Benchmark for SSE event parsing
func BenchmarkSSEParsing(b *testing.B) {
	data := `event: claude
data: {"type":"assistant","message":{"id":"msg_1","content":[{"type":"text","text":"Hello"}]}}

event: claude
data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":" World"}}

event: done
data: {"status":"complete"}

`
	for i := 0; i < b.N; i++ {
		readSSEEvents(bytes.NewReader([]byte(data)))
	}
}
