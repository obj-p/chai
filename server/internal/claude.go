package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
)

// ClaudeProcess manages a running Claude CLI instance
type ClaudeProcess struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	mu     sync.Mutex
}

// PendingRequest stores data from a control_request for later response
type PendingRequest struct {
	RequestID string
	SessionID string
	ToolInput map[string]any
}

// ClaudeManager handles Claude CLI interactions
type ClaudeManager struct {
	workingDir      string
	claudeCmd       string
	processes       map[string]*ClaudeProcess  // sessionID -> process
	pendingRequests map[string]*PendingRequest // requestID -> pending request data
	mu              sync.RWMutex
}

func NewClaudeManager(workingDir, claudeCmd string) *ClaudeManager {
	return &ClaudeManager{
		workingDir:      workingDir,
		claudeCmd:       claudeCmd,
		processes:       make(map[string]*ClaudeProcess),
		pendingRequests: make(map[string]*PendingRequest),
	}
}

// StorePendingRequest saves control_request data for later response
func (cm *ClaudeManager) StorePendingRequest(sessionID, requestID string, toolInput map[string]any) {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	cm.pendingRequests[requestID] = &PendingRequest{
		RequestID: requestID,
		SessionID: sessionID,
		ToolInput: toolInput,
	}
}

// GetPendingRequest retrieves and removes a pending request
func (cm *ClaudeManager) GetPendingRequest(requestID string) *PendingRequest {
	cm.mu.Lock()
	defer cm.mu.Unlock()
	req, ok := cm.pendingRequests[requestID]
	if ok {
		delete(cm.pendingRequests, requestID)
	}
	return req
}

// UserMessage is the JSON format for sending prompts via stdin
type UserMessage struct {
	Type    string         `json:"type"`
	Message UserMessageMsg `json:"message"`
}

type UserMessageMsg struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// RunPrompt executes a prompt and streams events through the callback
// The callback receives JSON lines from Claude CLI stdout
func (cm *ClaudeManager) RunPrompt(
	ctx context.Context,
	sessionID string,
	claudeSessionID *string,
	prompt string,
	workingDir *string,
	onEvent func(line []byte) error,
) (string, error) {
	args := []string{
		"--verbose",
		"--output-format", "stream-json",
		"--input-format", "stream-json",
		"--permission-prompt-tool", "stdio",
	}

	if claudeSessionID != nil && *claudeSessionID != "" {
		args = append(args, "--resume", *claudeSessionID)
	}

	cmd := exec.CommandContext(ctx, cm.claudeCmd, args...)
	// Use session working directory if provided, otherwise use default
	if workingDir != nil && *workingDir != "" {
		cmd.Dir = *workingDir
	} else {
		cmd.Dir = cm.workingDir
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", fmt.Errorf("stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("start: %w", err)
	}

	proc := &ClaudeProcess{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		stderr: stderr,
	}

	cm.mu.Lock()
	cm.processes[sessionID] = proc
	cm.mu.Unlock()

	defer func() {
		cm.mu.Lock()
		delete(cm.processes, sessionID)
		cm.mu.Unlock()
		stdin.Close()
	}()

	// Send the prompt via stdin as JSON
	userMsg := UserMessage{
		Type: "user",
		Message: UserMessageMsg{
			Role:    "user",
			Content: prompt,
		},
	}
	msgData, err := json.Marshal(userMsg)
	if err != nil {
		return "", fmt.Errorf("marshal prompt: %w", err)
	}
	msgData = append(msgData, '\n')
	if _, err := stdin.Write(msgData); err != nil {
		return "", fmt.Errorf("write prompt: %w", err)
	}

	// Read stderr in background for debugging
	go func() {
		scanner := bufio.NewScanner(stderr)
		for scanner.Scan() {
			// Log stderr but don't fail - Claude CLI writes debug info here
			fmt.Printf("[claude stderr] %s\n", scanner.Text())
		}
	}()

	// Process stdout JSON lines
	var resultSessionID string
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024) // 1MB buffer for large responses

	for scanner.Scan() {
		line := scanner.Bytes()

		// Try to extract session ID from result event
		var event ClaudeEvent
		if err := json.Unmarshal(line, &event); err == nil {
			if event.Type == "result" {
				var result ResultEvent
				if err := json.Unmarshal(line, &result); err == nil {
					resultSessionID = result.SessionID
				}
				// Result received - send to callback, close stdin to signal done, and exit loop
				onEvent(line)
				stdin.Close()
				break
			}
		}

		// Send event to callback
		if err := onEvent(line); err != nil {
			// Client disconnected, kill the process
			cmd.Process.Kill()
			return resultSessionID, err
		}
	}

	if err := scanner.Err(); err != nil {
		return resultSessionID, fmt.Errorf("scanner: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		// Check if context was cancelled
		if ctx.Err() != nil {
			return resultSessionID, ctx.Err()
		}
		return resultSessionID, fmt.Errorf("wait: %w", err)
	}

	return resultSessionID, nil
}

// NestedControlResponse has request_id inside response object
type NestedControlResponse struct {
	Type     string                    `json:"type"` // "control_response"
	Response NestedControlResponseBody `json:"response"`
}

type NestedControlResponseBody struct {
	Subtype      string                  `json:"subtype"`              // "success" or "error"
	RequestID    string                  `json:"request_id"`           // matches control_request.request_id
	Response     *PermissionDecision     `json:"response,omitempty"`   // for success
	Error        string                  `json:"error,omitempty"`      // for error
}

type PermissionDecision struct {
	Behavior     string         `json:"behavior"`               // "allow" or "deny"
	UpdatedInput map[string]any `json:"updatedInput,omitempty"` // for allow
	Message      string         `json:"message,omitempty"`      // for deny
}

// SendPermissionResponse sends an approval/denial to the running Claude process
// The requestID is the request_id from control_request events
func (cm *ClaudeManager) SendPermissionResponse(sessionID, requestID, decision string) error {
	cm.mu.RLock()
	proc, ok := cm.processes[sessionID]
	cm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no active process for session %s", sessionID)
	}

	// Get the pending request to include the original input
	pendingReq := cm.GetPendingRequest(requestID)

	proc.mu.Lock()
	defer proc.mu.Unlock()

	// Format: {"type":"control_response","response":{"subtype":"success","request_id":"...","response":{"behavior":"allow","updatedInput":{...}}}}
	var response NestedControlResponse
	if decision == "allow" {
		var updatedInput map[string]any
		if pendingReq != nil && pendingReq.ToolInput != nil {
			updatedInput = pendingReq.ToolInput
		} else {
			updatedInput = make(map[string]any)
		}
		response = NestedControlResponse{
			Type: "control_response",
			Response: NestedControlResponseBody{
				Subtype:   "success",
				RequestID: requestID,
				Response: &PermissionDecision{
					Behavior:     "allow",
					UpdatedInput: updatedInput,
				},
			},
		}
	} else {
		response = NestedControlResponse{
			Type: "control_response",
			Response: NestedControlResponseBody{
				Subtype:   "error",
				RequestID: requestID,
				Error:     "User denied permission",
			},
		}
	}

	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	data = append(data, '\n')

	log.Printf("[claude stdin] %s", string(data))

	if _, err := proc.stdin.Write(data); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	return nil
}

// KillProcess terminates a running Claude process
func (cm *ClaudeManager) KillProcess(sessionID string) error {
	cm.mu.Lock()
	proc, ok := cm.processes[sessionID]
	// Clean up any pending requests for this session
	for reqID, req := range cm.pendingRequests {
		if req.SessionID == sessionID {
			delete(cm.pendingRequests, reqID)
		}
	}
	cm.mu.Unlock()

	if !ok {
		return nil // No process running
	}

	return proc.cmd.Process.Kill()
}

// Shutdown terminates all running Claude processes
func (cm *ClaudeManager) Shutdown() {
	cm.mu.RLock()
	sessionIDs := make([]string, 0, len(cm.processes))
	for id := range cm.processes {
		sessionIDs = append(sessionIDs, id)
	}
	cm.mu.RUnlock()

	for _, id := range sessionIDs {
		cm.KillProcess(id)
	}
}
