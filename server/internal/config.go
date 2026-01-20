package internal

import (
	"flag"
	"fmt"
	"io"
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

// Flags holds the command-line flag pointers.
type Flags struct {
	port            *int
	dbPath          *string
	workDir         *string
	claudeCmd       *string
	promptTimeout   *time.Duration
	shutdownTimeout *time.Duration
}

// LoadConfigOptions configures the behavior of LoadConfig.
type LoadConfigOptions struct {
	// Logger for outputting configuration info. If nil, logs to stderr.
	// Set to io.Discard to suppress logging (useful for tests).
	Logger io.Writer
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

// flagChecker is a function type for checking if a flag was set.
// This allows injection of a test implementation.
type flagChecker func(name string) bool

// RegisterFlags registers command-line flags and returns flag pointers.
func RegisterFlags() *Flags {
	return &Flags{
		port:            flag.Int("port", defaultPort, "HTTP port (env: CHAI_PORT)"),
		dbPath:          flag.String("db", defaultDBPath, "SQLite database path (env: CHAI_DB)"),
		workDir:         flag.String("workdir", defaultWorkDir, "working directory for Claude CLI (env: CHAI_WORKDIR)"),
		claudeCmd:       flag.String("claude-cmd", defaultClaudeCmd, "path to Claude CLI command (env: CHAI_CLAUDE_CMD)"),
		promptTimeout:   flag.Duration("prompt-timeout", defaultPromptTimeout, "timeout for prompt requests (env: CHAI_PROMPT_TIMEOUT)"),
		shutdownTimeout: flag.Duration("shutdown-timeout", defaultShutdownTimeout, "timeout for graceful shutdown (env: CHAI_SHUTDOWN_TIMEOUT)"),
	}
}

// defaultFlagChecker uses flag.Visit to check if a flag was explicitly set.
func defaultFlagChecker(name string) bool {
	found := false
	flag.Visit(func(f *flag.Flag) {
		if f.Name == name {
			found = true
		}
	})
	return found
}

// validatePort checks that a port number is in the valid range 1-65535.
func validatePort(port int, source string) error {
	if port < 1 || port > 65535 {
		return fmt.Errorf("invalid port %d (from %s): must be between 1 and 65535", port, source)
	}
	return nil
}

// validatePositiveDuration checks that a duration is positive.
func validatePositiveDuration(d time.Duration, name, source string) error {
	if d <= 0 {
		return fmt.Errorf("invalid %s value %v (from %s): must be positive", name, d, source)
	}
	return nil
}

// LoadConfig loads configuration with precedence: flag > env > default.
// Must be called after flag.Parse().
func LoadConfig(f *Flags, opts *LoadConfigOptions) (*Config, error) {
	return loadConfigWithChecker(f, opts, defaultFlagChecker)
}

// loadConfigWithChecker is the internal implementation that accepts a custom flag checker.
// This allows tests to inject a mock flag checker.
func loadConfigWithChecker(f *Flags, opts *LoadConfigOptions, wasSet flagChecker) (*Config, error) {
	cfg := &Config{}
	source := &configSource{}

	// Port
	if wasSet("port") {
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
	if err := validatePort(cfg.Port, source.Port); err != nil {
		return nil, err
	}

	// DBPath
	if wasSet("db") {
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
	if wasSet("workdir") {
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
	if wasSet("claude-cmd") {
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
	if wasSet("prompt-timeout") {
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
	if err := validatePositiveDuration(cfg.PromptTimeout, "CHAI_PROMPT_TIMEOUT", source.PromptTimeout); err != nil {
		return nil, err
	}

	// ShutdownTimeout
	if wasSet("shutdown-timeout") {
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
	if err := validatePositiveDuration(cfg.ShutdownTimeout, "CHAI_SHUTDOWN_TIMEOUT", source.ShutdownTimeout); err != nil {
		return nil, err
	}

	// Log effective configuration with sources
	logConfig(cfg, source, opts)

	return cfg, nil
}

// logConfig logs the effective configuration if logging is enabled.
func logConfig(cfg *Config, source *configSource, opts *LoadConfigOptions) {
	var w io.Writer = os.Stderr
	if opts != nil && opts.Logger != nil {
		w = opts.Logger
	}

	logger := log.New(w, "", log.LstdFlags)
	logger.Printf("Configuration loaded:")
	logger.Printf("  Port: %d (from %s)", cfg.Port, source.Port)
	logger.Printf("  DB: %s (from %s)", cfg.DBPath, source.DBPath)
	logger.Printf("  WorkDir: %s (from %s)", cfg.WorkDir, source.WorkDir)
	logger.Printf("  ClaudeCmd: %s (from %s)", cfg.ClaudeCmd, source.ClaudeCmd)
	logger.Printf("  PromptTimeout: %s (from %s)", cfg.PromptTimeout, source.PromptTimeout)
	logger.Printf("  ShutdownTimeout: %s (from %s)", cfg.ShutdownTimeout, source.ShutdownTimeout)
}
