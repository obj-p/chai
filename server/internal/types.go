package internal

import (
	"encoding/json"
	"time"
)

// StreamStatus represents the current streaming state of a session
type StreamStatus string

const (
	StreamStatusIdle      StreamStatus = "idle"
	StreamStatusStreaming StreamStatus = "streaming"
	StreamStatusCompleted StreamStatus = "completed"
)

// Session represents a Claude CLI session
type Session struct {
	ID               string       `json:"id"`
	ClaudeSessionID  *string      `json:"claude_session_id,omitempty"`
	Title            *string      `json:"title,omitempty"`
	WorkingDirectory *string      `json:"working_directory,omitempty"`
	StreamStatus     StreamStatus `json:"stream_status"`
	PromptSequence   int64        `json:"-"` // Internal counter, not exposed in JSON
	CreatedAt        time.Time    `json:"created_at"`
	UpdatedAt        time.Time    `json:"updated_at"`
}

// Message represents a message in a session
type Message struct {
	ID        string          `json:"id"`
	SessionID string          `json:"session_id"`
	Role      string          `json:"role"` // "user", "assistant", "system"
	Content   string          `json:"content"`
	ToolCalls json.RawMessage `json:"tool_calls,omitempty"`
	CreatedAt time.Time       `json:"created_at"`
}

// API Request/Response types

type CreateSessionRequest struct {
	Title            string `json:"title,omitempty"`
	WorkingDirectory string `json:"working_directory,omitempty"`
}

type SessionResponse struct {
	Session  Session   `json:"session"`
	Messages []Message `json:"messages,omitempty"`
}

type PromptRequest struct {
	Prompt string `json:"prompt"`
}

type ApproveRequest struct {
	ToolUseID string `json:"tool_use_id"`
	Decision  string `json:"decision"` // "allow" or "deny"
}

// SessionEvent represents a persisted SSE event for mobile backgrounding resilience
type SessionEvent struct {
	ID        int64           `json:"id"`
	SessionID string          `json:"session_id"`
	PromptID  string          `json:"prompt_id"`
	Sequence  int64           `json:"sequence"`
	EventType string          `json:"event_type"`
	Data      json.RawMessage `json:"data"`
	CreatedAt time.Time       `json:"created_at"`
}

// GetEventsResponse contains paginated events for catch-up after reconnection
type GetEventsResponse struct {
	Events       []SessionEvent `json:"events"`
	LastSequence int64          `json:"last_sequence"`
	HasMore      bool           `json:"has_more"`
	StreamStatus StreamStatus   `json:"stream_status"`
}

// Claude CLI streaming types (JSON lines from stdout)

type ClaudeEvent struct {
	Type string `json:"type"`
}

// Assistant message events
type AssistantMessage struct {
	Type    string         `json:"type"` // "assistant"
	Message AssistantMsgV1 `json:"message"`
}

type AssistantMsgV1 struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"` // "message"
	Role    string         `json:"role"` // "assistant"
	Content []ContentBlock `json:"content"`
}

type ContentBlock struct {
	Type  string `json:"type"` // "text", "tool_use", "tool_result"
	Text  string `json:"text,omitempty"`
	ID    string `json:"id,omitempty"`    // for tool_use
	Name  string `json:"name,omitempty"`  // for tool_use
	Input any    `json:"input,omitempty"` // for tool_use
}

// Content block delta events
type ContentBlockDelta struct {
	Type  string           `json:"type"` // "content_block_delta"
	Index int              `json:"index"`
	Delta ContentDeltaData `json:"delta"`
}

type ContentDeltaData struct {
	Type string `json:"type"` // "text_delta"
	Text string `json:"text"`
}

// Result event (final)
type ResultEvent struct {
	Type        string  `json:"type"` // "result"
	Subtype     string  `json:"subtype"`
	SessionID   string  `json:"session_id"`
	CostUSD     float64 `json:"cost_usd"`
	DurationMS  int64   `json:"duration_ms"`
	DurationAPI int64   `json:"duration_api_ms"`
}

// Permission request from Claude CLI
type PermissionRequest struct {
	Type      string         `json:"type"` // "permission_request" or "tool_use"
	ToolUseID string         `json:"tool_use_id"`
	ToolName  string         `json:"tool_name"`
	Input     map[string]any `json:"input"`
}

// Permission response to send to Claude CLI stdin (legacy format)
type PermissionResponse struct {
	Type      string `json:"type"` // "permission_response"
	ToolUseID string `json:"tool_use_id"`
	Decision  string `json:"decision"` // "allow" or "deny"
}

// Control response to send to Claude CLI stdin (new format for control_request)
// Based on SDK protocol: SDKControlResponse -> ControlResponse | ControlErrorResponse
type ControlResponse struct {
	Type     string                 `json:"type"` // "control_response"
	Response ControlResponsePayload `json:"response"`
}

// ControlResponsePayload wraps either success or error response
type ControlResponsePayload struct {
	Subtype   string                      `json:"subtype"`    // "success" or "error"
	RequestID string                      `json:"request_id"` // matches control_request.request_id
	Response  *PermissionResultResponse   `json:"response,omitempty"` // for success
	Error     string                      `json:"error,omitempty"`    // for error
}

// PermissionResultResponse contains the permission decision
// Matches SDK's PermissionResultAllow / PermissionResultDeny
type PermissionResultResponse struct {
	Behavior           string         `json:"behavior"`                     // "allow" or "deny"
	UpdatedInput       map[string]any `json:"updatedInput,omitempty"`       // optional modified input (camelCase!)
	UpdatedPermissions []any          `json:"updatedPermissions,omitempty"` // optional permission updates
	Message            string         `json:"message,omitempty"`            // for deny: reason
	Interrupt          bool           `json:"interrupt,omitempty"`          // for deny: interrupt session
}

// SSE Event types sent to client
type SSEEvent struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}
