package internal

import (
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"
)

// Config holds all server configuration options.
type Config struct {
	Port            int
	DBPath          string
	WorkDir         string
	ClaudeCmd       string
	PromptTimeout   time.Duration
	ShutdownTimeout time.Duration
}

// configSource tracks where each config value came from.
type configSource struct {
	Port            string
	DBPath          string
	WorkDir         string
	ClaudeCmd       string
	PromptTimeout   string
	ShutdownTimeout string
}

// flags holds the command-line flag pointers.
type flags struct {
	port            *int
	dbPath          *string
	workDir         *string
	claudeCmd       *string
	promptTimeout   *time.Duration
	shutdownTimeout *time.Duration
}

// defaults for configuration.
const (
	defaultPort            = 8080
	defaultDBPath          = "chai.db"
	defaultWorkDir         = ""
	defaultClaudeCmd       = "claude"
	defaultPromptTimeout   = 5 * time.Minute
	defaultShutdownTimeout = 30 * time.Second
)

// RegisterFlags registers command-line flags and returns flag pointers.
func RegisterFlags() *flags {
	return &flags{
		port:            flag.Int("port", defaultPort, "HTTP port (env: CHAI_PORT)"),
		dbPath:          flag.String("db", defaultDBPath, "SQLite database path (env: CHAI_DB)"),
		workDir:         flag.String("workdir", defaultWorkDir, "working directory for Claude CLI (env: CHAI_WORKDIR)"),
		claudeCmd:       flag.String("claude-cmd", defaultClaudeCmd, "path to Claude CLI command (env: CHAI_CLAUDE_CMD)"),
		promptTimeout:   flag.Duration("prompt-timeout", defaultPromptTimeout, "timeout for prompt requests (env: CHAI_PROMPT_TIMEOUT)"),
		shutdownTimeout: flag.Duration("shutdown-timeout", defaultShutdownTimeout, "timeout for graceful shutdown (env: CHAI_SHUTDOWN_TIMEOUT)"),
	}
}

// flagWasSet returns true if the named flag was explicitly set on the command line.
func flagWasSet(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// LoadConfig loads configuration with precedence: flag > env > default.
// Must be called after flag.Parse().
func LoadConfig(f *flags) (*Config, error) {
	cfg := &Config{}
	source := &configSource{}

	// Port
	if flagWasSet("port") {
		cfg.Port = *f.port
		source.Port = "flag"
	} else if env := os.Getenv("CHAI_PORT"); env != "" {
		p, err := strconv.Atoi(env)
		if err != nil {
			return nil, fmt.Errorf("invalid CHAI_PORT value %q: %w", env, err)
		}
		cfg.Port = p
		source.Port = "env"
	} else {
		cfg.Port = defaultPort
		source.Port = "default"
	}

	// DBPath
	if flagWasSet("db") {
		cfg.DBPath = *f.dbPath
		source.DBPath = "flag"
	} else if env := os.Getenv("CHAI_DB"); env != "" {
		cfg.DBPath = env
		source.DBPath = "env"
	} else {
		cfg.DBPath = defaultDBPath
		source.DBPath = "default"
	}

	// WorkDir
	if flagWasSet("workdir") {
		cfg.WorkDir = *f.workDir
		source.WorkDir = "flag"
	} else if env := os.Getenv("CHAI_WORKDIR"); env != "" {
		cfg.WorkDir = env
		source.WorkDir = "env"
	} else {
		cfg.WorkDir = defaultWorkDir
		source.WorkDir = "default"
	}

	// ClaudeCmd
	if flagWasSet("claude-cmd") {
		cfg.ClaudeCmd = *f.claudeCmd
		source.ClaudeCmd = "flag"
	} else if env := os.Getenv("CHAI_CLAUDE_CMD"); env != "" {
		cfg.ClaudeCmd = env
		source.ClaudeCmd = "env"
	} else {
		cfg.ClaudeCmd = defaultClaudeCmd
		source.ClaudeCmd = "default"
	}

	// PromptTimeout
	if flagWasSet("prompt-timeout") {
		cfg.PromptTimeout = *f.promptTimeout
		source.PromptTimeout = "flag"
	} else if env := os.Getenv("CHAI_PROMPT_TIMEOUT"); env != "" {
		d, err := time.ParseDuration(env)
		if err != nil {
			return nil, fmt.Errorf("invalid CHAI_PROMPT_TIMEOUT value %q: %w", env, err)
		}
		cfg.PromptTimeout = d
		source.PromptTimeout = "env"
	} else {
		cfg.PromptTimeout = defaultPromptTimeout
		source.PromptTimeout = "default"
	}

	// ShutdownTimeout
	if flagWasSet("shutdown-timeout") {
		cfg.ShutdownTimeout = *f.shutdownTimeout
		source.ShutdownTimeout = "flag"
	} else if env := os.Getenv("CHAI_SHUTDOWN_TIMEOUT"); env != "" {
		d, err := time.ParseDuration(env)
		if err != nil {
			return nil, fmt.Errorf("invalid CHAI_SHUTDOWN_TIMEOUT value %q: %w", env, err)
		}
		cfg.ShutdownTimeout = d
		source.ShutdownTimeout = "env"
	} else {
		cfg.ShutdownTimeout = defaultShutdownTimeout
		source.ShutdownTimeout = "default"
	}

	// Log effective configuration with sources
	log.Printf("Configuration loaded:")
	log.Printf("  Port: %d (from %s)", cfg.Port, source.Port)
	log.Printf("  DB: %s (from %s)", cfg.DBPath, source.DBPath)
	log.Printf("  WorkDir: %s (from %s)", cfg.WorkDir, source.WorkDir)
	log.Printf("  ClaudeCmd: %s (from %s)", cfg.ClaudeCmd, source.ClaudeCmd)
	log.Printf("  PromptTimeout: %s (from %s)", cfg.PromptTimeout, source.PromptTimeout)
	log.Printf("  ShutdownTimeout: %s (from %s)", cfg.ShutdownTimeout, source.ShutdownTimeout)

	return cfg, nil
}
