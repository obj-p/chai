package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"

	"chai/server/internal"
)

func main() {
	// Flags
	port := flag.Int("port", 8080, "port to listen on")
	dbPath := flag.String("db", "chai.db", "path to SQLite database")
	workDir := flag.String("workdir", "", "working directory for Claude CLI (defaults to current dir)")
	flag.Parse()

	// Default working directory to current directory
	if *workDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get working directory: %v", err)
		}
		*workDir = wd
	}

	// Make db path absolute
	if !filepath.IsAbs(*dbPath) {
		*dbPath = filepath.Join(*workDir, *dbPath)
	}

	// Initialize repository
	repo, err := internal.NewRepository(*dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer repo.Close()

	// Initialize Claude manager
	claude := internal.NewClaudeManager(*workDir)

	// Initialize handlers
	handlers := internal.NewHandlers(repo, claude)

	// Set up routes using Go 1.22+ stdlib router
	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("GET /health", handlers.Health)

	// Sessions
	mux.HandleFunc("GET /api/sessions", handlers.ListSessions)
	mux.HandleFunc("POST /api/sessions", handlers.CreateSession)
	mux.HandleFunc("GET /api/sessions/{id}", handlers.GetSession)
	mux.HandleFunc("DELETE /api/sessions/{id}", handlers.DeleteSession)

	// Session actions
	mux.HandleFunc("POST /api/sessions/{id}/prompt", handlers.Prompt)
	mux.HandleFunc("POST /api/sessions/{id}/approve", handlers.Approve)

	// Start server
	addr := fmt.Sprintf(":%d", *port)
	log.Printf("Server starting on %s", addr)
	log.Printf("Database: %s", *dbPath)
	log.Printf("Working directory: %s", *workDir)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
