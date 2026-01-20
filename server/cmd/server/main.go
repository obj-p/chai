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

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"chai/server/internal"
)

func main() {
	// Flags
	port := flag.Int("port", 8080, "port to listen on")
	dbPath := flag.String("db", "chai.db", "path to SQLite database")
	workDir := flag.String("workdir", "", "working directory for Claude CLI (defaults to current dir)")
	claudeCmd := flag.String("claude-cmd", "claude", "path to Claude CLI command")
	promptTimeout := flag.Duration("prompt-timeout", 5*time.Minute, "timeout for prompt requests")
	shutdownTimeout := flag.Duration("shutdown-timeout", 30*time.Second, "timeout for graceful shutdown")
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

	// Set up Chi router with middleware
	r := chi.NewRouter()

	// Middleware stack
	r.Use(middleware.RequestID)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	// Health check
	r.Get("/health", handlers.Health)

	// API routes with grouping
	r.Route("/api", func(r chi.Router) {
		r.Route("/sessions", func(r chi.Router) {
			r.Get("/", handlers.ListSessions)
			r.Post("/", handlers.CreateSession)

			r.Route("/{id}", func(r chi.Router) {
				r.Get("/", handlers.GetSession)
				r.Delete("/", handlers.DeleteSession)
				r.Post("/prompt", handlers.Prompt)
				r.Post("/approve", handlers.Approve)
			})
		})
	})

	// Create server
	addr := fmt.Sprintf(":%d", *port)
	server := &http.Server{
		Addr:    addr,
		Handler: r,
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
		ctx, cancel := context.WithTimeout(context.Background(), *shutdownTimeout)
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
