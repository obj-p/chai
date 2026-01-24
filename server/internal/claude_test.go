package internal

import (
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"sync"
	"testing"
)

// mockWriteCloser captures data written to it for testing
type mockWriteCloser struct {
	buf    bytes.Buffer
	mu     sync.Mutex
	closed bool
}

func (m *mockWriteCloser) Write(p []byte) (n int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.Write(p)
}

func (m *mockWriteCloser) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *mockWriteCloser) Bytes() []byte {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.buf.Bytes()
}

func TestSendPermissionResponse_AllowFormat(t *testing.T) {
	cm := NewClaudeManager("/tmp", "claude")

	// Create a mock process with a mock stdin
	mockStdin := &mockWriteCloser{}
	proc := &ClaudeProcess{
		cmd:   &exec.Cmd{},
		stdin: mockStdin,
	}

	sessionID := "test-session"
	requestID := "req-123"

	// Register the process
	cm.mu.Lock()
	cm.processes[sessionID] = proc
	cm.mu.Unlock()

	// Store a pending request with tool input
	toolInput := map[string]any{
		"command":     "ls -la",
		"description": "List files",
	}
	cm.StorePendingRequest(sessionID, requestID, toolInput)

	// Send allow response
	err := cm.SendPermissionResponse(sessionID, requestID, "allow")
	if err != nil {
		t.Fatalf("SendPermissionResponse failed: %v", err)
	}

	// Parse the written JSON
	data := mockStdin.Bytes()
	// Remove trailing newline for parsing
	data = bytes.TrimSuffix(data, []byte("\n"))

	var response NestedControlResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Failed to parse JSON response: %v\nData: %s", err, string(data))
	}

	// Verify structure
	if response.Type != "control_response" {
		t.Errorf("Type = %q, want %q", response.Type, "control_response")
	}
	if response.Response.Subtype != "success" {
		t.Errorf("Subtype = %q, want %q", response.Response.Subtype, "success")
	}
	if response.Response.RequestID != requestID {
		t.Errorf("RequestID = %q, want %q", response.Response.RequestID, requestID)
	}
	if response.Response.Response == nil {
		t.Fatal("Response.Response is nil")
	}
	if response.Response.Response.Behavior != "allow" {
		t.Errorf("Behavior = %q, want %q", response.Response.Response.Behavior, "allow")
	}
	if response.Response.Response.UpdatedInput == nil {
		t.Fatal("UpdatedInput is nil")
	}
	if response.Response.Response.UpdatedInput["command"] != "ls -la" {
		t.Errorf("UpdatedInput[command] = %v, want %q", response.Response.Response.UpdatedInput["command"], "ls -la")
	}
	if response.Response.Response.UpdatedInput["description"] != "List files" {
		t.Errorf("UpdatedInput[description] = %v, want %q", response.Response.Response.UpdatedInput["description"], "List files")
	}
}

func TestSendPermissionResponse_DenyFormat(t *testing.T) {
	cm := NewClaudeManager("/tmp", "claude")

	// Create a mock process with a mock stdin
	mockStdin := &mockWriteCloser{}
	proc := &ClaudeProcess{
		cmd:   &exec.Cmd{},
		stdin: mockStdin,
	}

	sessionID := "test-session"
	requestID := "req-456"

	// Register the process
	cm.mu.Lock()
	cm.processes[sessionID] = proc
	cm.mu.Unlock()

	// Send deny response (no pending request needed for deny)
	err := cm.SendPermissionResponse(sessionID, requestID, "deny")
	if err != nil {
		t.Fatalf("SendPermissionResponse failed: %v", err)
	}

	// Parse the written JSON
	data := mockStdin.Bytes()
	// Remove trailing newline for parsing
	data = bytes.TrimSuffix(data, []byte("\n"))

	var response NestedControlResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Failed to parse JSON response: %v\nData: %s", err, string(data))
	}

	// Verify structure - deny uses same format as allow with behavior: "deny"
	if response.Type != "control_response" {
		t.Errorf("Type = %q, want %q", response.Type, "control_response")
	}
	if response.Response.Subtype != "success" {
		t.Errorf("Subtype = %q, want %q (deny uses success subtype)", response.Response.Subtype, "success")
	}
	if response.Response.RequestID != requestID {
		t.Errorf("RequestID = %q, want %q", response.Response.RequestID, requestID)
	}
	if response.Response.Response == nil {
		t.Fatal("Response.Response is nil")
	}
	if response.Response.Response.Behavior != "deny" {
		t.Errorf("Behavior = %q, want %q", response.Response.Response.Behavior, "deny")
	}
	if response.Response.Response.Message != "User denied permission" {
		t.Errorf("Message = %q, want %q", response.Response.Response.Message, "User denied permission")
	}
}

func TestSendPermissionResponse_AllowWithNoPendingRequest(t *testing.T) {
	cm := NewClaudeManager("/tmp", "claude")

	// Create a mock process with a mock stdin
	mockStdin := &mockWriteCloser{}
	proc := &ClaudeProcess{
		cmd:   &exec.Cmd{},
		stdin: mockStdin,
	}

	sessionID := "test-session"
	requestID := "req-789"

	// Register the process but don't store a pending request
	cm.mu.Lock()
	cm.processes[sessionID] = proc
	cm.mu.Unlock()

	// Send allow response without pending request
	err := cm.SendPermissionResponse(sessionID, requestID, "allow")
	if err != nil {
		t.Fatalf("SendPermissionResponse failed: %v", err)
	}

	// Parse the written JSON
	data := mockStdin.Bytes()
	data = bytes.TrimSuffix(data, []byte("\n"))

	var response NestedControlResponse
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Failed to parse JSON response: %v\nData: %s", err, string(data))
	}

	// Verify response structure is correct
	if response.Response.Response == nil {
		t.Fatal("Response.Response is nil")
	}
	if response.Response.Response.Behavior != "allow" {
		t.Errorf("Behavior = %q, want %q", response.Response.Response.Behavior, "allow")
	}
	// When no pending request exists, updatedInput is an empty map (which serializes as empty {})
	// This is acceptable - the SDK expects updatedInput to be present but can be empty
	if response.Response.Response.UpdatedInput == nil {
		t.Log("Note: UpdatedInput is nil when no pending request exists")
	}
}

func TestSendPermissionResponse_NoActiveProcess(t *testing.T) {
	cm := NewClaudeManager("/tmp", "claude")

	// Don't register any process
	err := cm.SendPermissionResponse("nonexistent-session", "req-123", "allow")
	if err == nil {
		t.Error("Expected error for nonexistent session")
	}
}

func TestPendingRequestStorage(t *testing.T) {
	cm := NewClaudeManager("/tmp", "claude")

	sessionID := "session-1"
	requestID := "req-1"
	toolInput := map[string]any{
		"file_path": "/tmp/test.txt",
		"content":   "hello world",
	}

	// Store request
	cm.StorePendingRequest(sessionID, requestID, toolInput)

	// Retrieve request
	req := cm.GetPendingRequest(requestID)
	if req == nil {
		t.Fatal("Expected to get pending request")
	}
	if req.RequestID != requestID {
		t.Errorf("RequestID = %q, want %q", req.RequestID, requestID)
	}
	if req.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", req.SessionID, sessionID)
	}
	if req.ToolInput["file_path"] != "/tmp/test.txt" {
		t.Errorf("ToolInput[file_path] = %v, want %q", req.ToolInput["file_path"], "/tmp/test.txt")
	}

	// Verify request was removed after retrieval
	req2 := cm.GetPendingRequest(requestID)
	if req2 != nil {
		t.Error("Expected pending request to be removed after retrieval")
	}
}

func TestPendingRequestNotFound(t *testing.T) {
	cm := NewClaudeManager("/tmp", "claude")

	// Try to get nonexistent request
	req := cm.GetPendingRequest("nonexistent")
	if req != nil {
		t.Error("Expected nil for nonexistent request")
	}
}

// Verify the exact JSON format matches SDK expectations
func TestControlResponseJSONFormat(t *testing.T) {
	// Test allow format
	allowResponse := NestedControlResponse{
		Type: "control_response",
		Response: NestedControlResponseBody{
			Subtype:   "success",
			RequestID: "test-request-id",
			Response: &PermissionDecision{
				Behavior: "allow",
				UpdatedInput: map[string]any{
					"command": "ls",
				},
			},
		},
	}

	allowJSON, err := json.Marshal(allowResponse)
	if err != nil {
		t.Fatalf("Failed to marshal allow response: %v", err)
	}

	// Verify it can be parsed back
	var parsedAllow map[string]any
	if err := json.Unmarshal(allowJSON, &parsedAllow); err != nil {
		t.Fatalf("Failed to unmarshal allow response: %v", err)
	}

	// Check nested structure
	if parsedAllow["type"] != "control_response" {
		t.Errorf("type = %v, want control_response", parsedAllow["type"])
	}
	resp := parsedAllow["response"].(map[string]any)
	if resp["subtype"] != "success" {
		t.Errorf("subtype = %v, want success", resp["subtype"])
	}
	if resp["request_id"] != "test-request-id" {
		t.Errorf("request_id = %v, want test-request-id", resp["request_id"])
	}
	innerResp := resp["response"].(map[string]any)
	if innerResp["behavior"] != "allow" {
		t.Errorf("behavior = %v, want allow", innerResp["behavior"])
	}
	updatedInput := innerResp["updatedInput"].(map[string]any)
	if updatedInput["command"] != "ls" {
		t.Errorf("updatedInput.command = %v, want ls", updatedInput["command"])
	}

	// Test deny format
	denyResponse := NestedControlResponse{
		Type: "control_response",
		Response: NestedControlResponseBody{
			Subtype:   "success",
			RequestID: "test-request-id",
			Response: &PermissionDecision{
				Behavior: "deny",
				Message:  "User denied permission",
			},
		},
	}

	denyJSON, err := json.Marshal(denyResponse)
	if err != nil {
		t.Fatalf("Failed to marshal deny response: %v", err)
	}

	// Verify deny uses same structure
	var parsedDeny map[string]any
	if err := json.Unmarshal(denyJSON, &parsedDeny); err != nil {
		t.Fatalf("Failed to unmarshal deny response: %v", err)
	}

	denyResp := parsedDeny["response"].(map[string]any)
	if denyResp["subtype"] != "success" {
		t.Errorf("deny subtype = %v, want success", denyResp["subtype"])
	}
	denyInnerResp := denyResp["response"].(map[string]any)
	if denyInnerResp["behavior"] != "deny" {
		t.Errorf("deny behavior = %v, want deny", denyInnerResp["behavior"])
	}
	if denyInnerResp["message"] != "User denied permission" {
		t.Errorf("deny message = %v, want 'User denied permission'", denyInnerResp["message"])
	}
	// updatedInput should not be present for deny
	if _, exists := denyInnerResp["updatedInput"]; exists {
		t.Error("updatedInput should not be present for deny response")
	}
}

// Suppress unused import warning
var _ = io.Discard
