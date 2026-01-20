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
	// Register flags
	f := internal.RegisterFlags()
	flag.Parse()

	// Load configuration (flag > env > default)
	cfg, err := internal.LoadConfig(f, nil)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// Default working directory to current directory
	if cfg.WorkDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			log.Fatalf("Failed to get working directory: %v", err)
		}
		cfg.WorkDir = wd
	}

	// Make db path absolute
	if !filepath.IsAbs(cfg.DBPath) {
		cfg.DBPath = filepath.Join(cfg.WorkDir, cfg.DBPath)
	}

	// Initialize repository
	repo, err := internal.NewRepository(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer repo.Close()

	// Initialize Claude manager
	claude := internal.NewClaudeManager(cfg.WorkDir, cfg.ClaudeCmd)

	// Initialize handlers
	handlers := internal.NewHandlers(repo, claude, cfg.PromptTimeout)

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
				r.Get("/events", handlers.GetEvents)
			})
		})
	})

	// Create server
	addr := fmt.Sprintf(":%d", cfg.Port)
	server := &http.Server{
		Addr:        addr,
		Handler:     r,
		IdleTimeout: 10 * time.Minute,
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
		ctx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
		defer cancel()
		if err := server.Shutdown(ctx); err != nil {
			log.Printf("HTTP server shutdown error: %v", err)
		}
	}()

	// Start server
	log.Printf("Server starting on %s", addr)
	log.Printf("Database: %s", cfg.DBPath)
	log.Printf("Working directory: %s", cfg.WorkDir)

	if err := server.ListenAndServe(); err != http.ErrServerClosed {
		log.Fatalf("Server error: %v", err)
	}

	log.Println("Server stopped")
}
