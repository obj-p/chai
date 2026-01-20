package internal

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
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

// ClaudeManager handles Claude CLI interactions
type ClaudeManager struct {
	workingDir string
	claudeCmd  string
	processes  map[string]*ClaudeProcess // sessionID -> process
	mu         sync.RWMutex
}

func NewClaudeManager(workingDir, claudeCmd string) *ClaudeManager {
	return &ClaudeManager{
		workingDir: workingDir,
		claudeCmd:  claudeCmd,
		processes:  make(map[string]*ClaudeProcess),
	}
}

// RunPrompt executes a prompt and streams events through the callback
// The callback receives JSON lines from Claude CLI stdout
func (cm *ClaudeManager) RunPrompt(
	ctx context.Context,
	sessionID string,
	claudeSessionID *string,
	prompt string,
	onEvent func(line []byte) error,
) (string, error) {
	args := []string{
		"--print",
		"--output-format", "stream-json",
		"--permission-prompt-tool", "stdio",
	}

	if claudeSessionID != nil && *claudeSessionID != "" {
		args = append(args, "--resume", *claudeSessionID)
	}

	args = append(args, prompt)

	cmd := exec.CommandContext(ctx, "claude", args...)
	cmd.Dir = cm.workingDir

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

// SendPermissionResponse sends an approval/denial to the running Claude process
func (cm *ClaudeManager) SendPermissionResponse(sessionID, toolUseID, decision string) error {
	cm.mu.RLock()
	proc, ok := cm.processes[sessionID]
	cm.mu.RUnlock()

	if !ok {
		return fmt.Errorf("no active process for session %s", sessionID)
	}

	proc.mu.Lock()
	defer proc.mu.Unlock()

	response := PermissionResponse{
		Type:      "permission_response",
		ToolUseID: toolUseID,
		Decision:  decision,
	}

	data, err := json.Marshal(response)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	data = append(data, '\n')

	if _, err := proc.stdin.Write(data); err != nil {
		return fmt.Errorf("write: %w", err)
	}

	return nil
}

// KillProcess terminates a running Claude process
func (cm *ClaudeManager) KillProcess(sessionID string) error {
	cm.mu.RLock()
	proc, ok := cm.processes[sessionID]
	cm.mu.RUnlock()

	if !ok {
		return nil // No process running
	}

	return proc.cmd.Process.Kill()
}
