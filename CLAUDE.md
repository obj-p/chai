# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

Chai is a Go server for Claude CLI session management with REST API and SSE streaming, designed for iOS client integration.

## Build and Test Commands

All commands run from `server/` directory:

```bash
# Build
make build              # Build server binary
go build ./cmd/server   # Alternative

# Test
make test               # Unit tests
make test-integration   # Integration tests (requires Claude CLI installed)
go test -v ./internal/...                    # Run specific package tests
go test -v ./internal/... -run TestName      # Run single test

# Run
make run                # Build and run (port 8080)
./server -port 8080 -db chai.db -workdir /path/to/project
```

## Architecture

### Server Structure (`server/`)

```
cmd/server/main.go     - Entry point, routing (Go 1.22+ stdlib router)
internal/
  types.go             - Domain types, API DTOs, Claude CLI event types
  repository.go        - SQLite operations (sessions, messages)
  claude.go            - Claude CLI process management, stdin/stdout streaming
  handlers.go          - HTTP handlers including SSE for /prompt endpoint
```

### Key Design Decisions

- **No external router**: Uses Go 1.22+ stdlib `http.ServeMux` with pattern matching
- **SQLite**: Single-file database with foreign keys enabled
- **SSE streaming**: `/api/sessions/{id}/prompt` streams Claude CLI JSON output to client
- **stdin JSON protocol**: Claude CLI runs with `--input-format stream-json --permission-prompt-tool stdio`, prompts sent via stdin as `{"type":"user","message":{"role":"user","content":"..."}}`

### API Endpoints

| Method | Endpoint | Description |
|--------|----------|-------------|
| GET | `/health` | Health check |
| GET | `/api/sessions` | List sessions |
| POST | `/api/sessions` | Create session |
| GET | `/api/sessions/{id}` | Get session + messages |
| DELETE | `/api/sessions/{id}` | Delete session |
| POST | `/api/sessions/{id}/prompt` | Send prompt (SSE response) |
| POST | `/api/sessions/{id}/approve` | Approve/reject tool use |

### Claude CLI Integration

The server spawns Claude CLI processes with streaming JSON I/O:
- Prompts sent via stdin after process starts
- JSON events streamed from stdout, forwarded as SSE to client
- Permission responses written to stdin when tools need approval
- Process terminates after receiving "result" event
