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

# Server flags
./server \
  -port 8080 \                    # HTTP port (default: 8080)
  -db chai.db \                   # SQLite database path (default: chai.db)
  -workdir /path/to/project \     # Default working directory for Claude CLI
  -claude-cmd claude \            # Path to Claude CLI command (default: claude)
  -prompt-timeout 5m \            # Timeout for prompt requests (default: 5m)
  -shutdown-timeout 30s           # Graceful shutdown timeout (default: 30s)
```

## Configuration

Configuration can be set via command-line flags or environment variables.

**Precedence (highest to lowest):**
1. Command-line flags
2. Environment variables
3. Default values

| Flag | Environment Variable | Default | Description |
|------|---------------------|---------|-------------|
| `-port` | `CHAI_PORT` | `8080` | HTTP port |
| `-db` | `CHAI_DB` | `chai.db` | SQLite database path |
| `-workdir` | `CHAI_WORKDIR` | (current dir) | Working directory for Claude CLI |
| `-claude-cmd` | `CHAI_CLAUDE_CMD` | `claude` | Path to Claude CLI command |
| `-prompt-timeout` | `CHAI_PROMPT_TIMEOUT` | `5m` | Timeout for prompt requests |
| `-shutdown-timeout` | `CHAI_SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown timeout |

**Path resolution:** If `CHAI_DB` is a relative path, it is resolved relative to `CHAI_WORKDIR`.

**Example with environment variables:**
```bash
export CHAI_PORT=3000
export CHAI_DB=/data/chai.db
export CHAI_WORKDIR=/projects/myapp
./server
```

**Example with Docker:**
```bash
docker run -e CHAI_PORT=3000 -e CHAI_WORKDIR=/project -v $(pwd):/project chai
```

See `server/.env.example` for a template configuration file.

## Architecture

### Server Structure (`server/`)

```
cmd/server/main.go     - Entry point, Chi routing with middleware
internal/
  config.go            - Configuration struct, flag/env parsing with precedence
  types.go             - Domain types, API DTOs, Claude CLI event types
  repository.go        - SQLite operations (sessions, messages)
  claude.go            - Claude CLI process management, stdin/stdout streaming
  handlers.go          - HTTP handlers including SSE for /prompt endpoint
```

### Key Design Decisions

- **Chi router**: Uses github.com/go-chi/chi/v5 for routing with built-in middleware (RequestID, Logger, Recoverer)
- **SQLite**: Single-file database with foreign keys enabled
- **SSE streaming**: `/api/sessions/{id}/prompt` streams Claude CLI JSON output to client
- **stdin JSON protocol**: Claude CLI runs with `--input-format stream-json --permission-prompt-tool stdio`, prompts sent via stdin as `{"type":"user","message":{"role":"user","content":"..."}}`
- **Graceful shutdown**: Handles SIGINT/SIGTERM, kills Claude processes, then shuts down HTTP server
- **Per-session working directory**: Sessions can override the default working directory

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

## iOS App (`ios/`)

### Prerequisites

```bash
# One-time system setup
brew bundle                      # Install xcodegen, caddy, mkcert (from repo root)
mkcert -install                  # Install local CA (requires sudo)
```

### Build Commands

All commands run from `ios/` directory. **Important:** Source env vars before running make commands:

```bash
set -a && source ../.env && set +a
```

```bash
# Setup (first time)
bundle install                   # Install Fastlane
security unlock-keychain ~/Library/Keychains/login.keychain-db  # Unlock keychain
bundle exec fastlane match adhoc # Set up certificates/profiles

# Development
make generate                    # Generate Xcode project
make build                       # Build ad-hoc IPA
make distribute                  # Build IPA + generate distribution files

# Distribution server
make certs                       # Generate TLS certs with mkcert
make serve                       # Start Caddy HTTPS server
```

**Note:** For non-interactive builds (CI, scripts), the keychain must be unlocked and `MATCH_KEYCHAIN_PASSWORD` should be set in `.env`.

### Installing on iOS Device

With Caddy running (`make serve`), on your iOS device:

1. **Install mkcert CA** (one-time per device):
   - Visit `https://<CHAI_DISTRIBUTION_DOMAIN>/ca.pem` in Safari
   - Tap "Allow" to download the profile
   - Go to Settings → General → VPN & Device Management
   - Tap the downloaded profile and install it
   - Go to Settings → General → About → Certificate Trust Settings
   - Enable full trust for the mkcert certificate

2. **Install the app**:
   - Visit `https://<CHAI_DISTRIBUTION_DOMAIN>/ios/`
   - Tap "Install App"
   - After installation, trust the developer certificate if prompted:
     Settings → General → VPN & Device Management → tap developer profile → Trust

The mkcert CA is named "mkcert [username]@[hostname]" based on the machine that generated it.

### Configuration

iOS-specific environment variables (in `.env`):

| Variable | Description |
|----------|-------------|
| `CHAI_DISTRIBUTION_DOMAIN` | Domain for ad-hoc distribution (e.g., `machine.tailnet.ts.net`) |
| `CHAI_TEAM_ID` | Apple Developer Team ID |
| `MATCH_GIT_URL` | Git URL for certificates repo (private) |
| `MATCH_PASSWORD` | Encryption password for match |
| `FASTLANE_USER` | Apple ID email |
| `FASTLANE_PASSWORD` | App-specific password for Apple ID |
| `MATCH_KEYCHAIN_PASSWORD` | macOS login password (for non-interactive builds) |

### Structure

```
ios/
  Chai/                  - App source code
  project.yml            - XcodeGen project spec
  fastlane/
    Appfile              - App identifier, team ID
    Matchfile            - Certificate/profile config
    Fastfile             - Build and distribution lanes
  Caddyfile              - HTTPS server for ad-hoc distribution
  Makefile               - Build automation
```
