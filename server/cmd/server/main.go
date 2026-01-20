package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"chai/server/internal"
)

func main() {
	// Flags
	port := flag.Int("port", 8080, "port to listen on")
	dbPath := flag.String("db", "chai.db", "path to SQLite database")
	workDir := flag.String("workdir", "", "working directory for Claude CLI (defaults to current dir)")
	claudeCmd := flag.String("claude-cmd", "claude", "path to Claude CLI command")
	promptTimeout := flag.Duration("prompt-timeout", 5*time.Minute, "timeout for prompt requests")
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
	claude := internal.NewClaudeManager(*workDir, *claudeCmd)

	// Initialize handlers
	handlers := internal.NewHandlers(repo, claude, *promptTimeout)

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

	// Create server
	addr := fmt.Sprintf(":%d", *port)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		sig := <-sigChan
		log.Printf("Received signal %v, shutting down...", sig)

		// Kill all Claude processes
		claude.Shutdown()

		// Graceful HTTP shutdown with timeout
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	// Start server
	log.Printf("Server starting on %s", addr)
	log.Printf("Database: %s", *dbPath)
	log.Printf("Working directory: %s", *workDir)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}
