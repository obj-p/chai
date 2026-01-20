package internal

import (
	"io"
	"os"
	"testing"
	"time"
)

// testOpts returns LoadConfigOptions that suppress logging during tests.
func testOpts() *LoadConfigOptions {
	return &LoadConfigOptions{Logger: io.Discard}
}

// newTestFlags creates a Flags struct with the given values for testing.
func newTestFlags(port int, dbPath, workDir, claudeCmd string, promptTimeout, shutdownTimeout time.Duration) *Flags {
	return &Flags{
		port:            &port,
		dbPath:          &dbPath,
		workDir:         &workDir,
		claudeCmd:       &claudeCmd,
		promptTimeout:   &promptTimeout,
		shutdownTimeout: &shutdownTimeout,
	}
}

// neverSet is a flagChecker that always returns false (no flags set).
func neverSet(name string) bool {
	return false
}

// alwaysSet is a flagChecker that always returns true (all flags set).
func alwaysSet(name string) bool {
	return true
}

// makeChecker creates a flagChecker that returns true only for the specified flags.
func makeChecker(setFlags ...string) flagChecker {
	set := make(map[string]bool)
	for _, f := range setFlags {
		set[f] = true
	}
	return func(name string) bool {
		return set[name]
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear any existing env vars
	clearEnvVars()

	f := newTestFlags(defaultPort, defaultDBPath, defaultWorkDir, defaultClaudeCmd, defaultPromptTimeout, defaultShutdownTimeout)

	cfg, err := loadConfigWithChecker(f, testOpts(), neverSet)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify defaults
	if cfg.Port != 8080 {
		t.Errorf("Port = %d, want 8080", cfg.Port)
	}
	if cfg.DBPath != "chai.db" {
		t.Errorf("DBPath = %s, want chai.db", cfg.DBPath)
	}
	if cfg.WorkDir != "" {
		t.Errorf("WorkDir = %s, want empty", cfg.WorkDir)
	}
	if cfg.ClaudeCmd != "claude" {
		t.Errorf("ClaudeCmd = %s, want claude", cfg.ClaudeCmd)
	}
	if cfg.PromptTimeout != 5*time.Minute {
		t.Errorf("PromptTimeout = %v, want 5m", cfg.PromptTimeout)
	}
	if cfg.ShutdownTimeout != 30*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 30s", cfg.ShutdownTimeout)
	}
}

func TestLoadConfig_EnvVars(t *testing.T) {
	// Set environment variables
	os.Setenv("CHAI_PORT", "3000")
	os.Setenv("CHAI_DB", "/data/test.db")
	os.Setenv("CHAI_WORKDIR", "/projects/myapp")
	os.Setenv("CHAI_CLAUDE_CMD", "/usr/bin/claude")
	os.Setenv("CHAI_PROMPT_TIMEOUT", "10m")
	os.Setenv("CHAI_SHUTDOWN_TIMEOUT", "1m")
	defer clearEnvVars()

	f := newTestFlags(defaultPort, defaultDBPath, defaultWorkDir, defaultClaudeCmd, defaultPromptTimeout, defaultShutdownTimeout)

	cfg, err := loadConfigWithChecker(f, testOpts(), neverSet)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify env vars are used
	if cfg.Port != 3000 {
		t.Errorf("Port = %d, want 3000", cfg.Port)
	}
	if cfg.DBPath != "/data/test.db" {
		t.Errorf("DBPath = %s, want /data/test.db", cfg.DBPath)
	}
	if cfg.WorkDir != "/projects/myapp" {
		t.Errorf("WorkDir = %s, want /projects/myapp", cfg.WorkDir)
	}
	if cfg.ClaudeCmd != "/usr/bin/claude" {
		t.Errorf("ClaudeCmd = %s, want /usr/bin/claude", cfg.ClaudeCmd)
	}
	if cfg.PromptTimeout != 10*time.Minute {
		t.Errorf("PromptTimeout = %v, want 10m", cfg.PromptTimeout)
	}
	if cfg.ShutdownTimeout != 1*time.Minute {
		t.Errorf("ShutdownTimeout = %v, want 1m", cfg.ShutdownTimeout)
	}
}

func TestLoadConfig_FlagPrecedence(t *testing.T) {
	// Set environment variables with different values
	os.Setenv("CHAI_PORT", "3000")
	os.Setenv("CHAI_DB", "/env/path.db")
	os.Setenv("CHAI_WORKDIR", "/env/workdir")
	os.Setenv("CHAI_CLAUDE_CMD", "/env/claude")
	os.Setenv("CHAI_PROMPT_TIMEOUT", "10m")
	os.Setenv("CHAI_SHUTDOWN_TIMEOUT", "1m")
	defer clearEnvVars()

	// Flag values that should take precedence
	f := newTestFlags(9000, "/flag/path.db", "/flag/workdir", "/flag/claude", 15*time.Minute, 45*time.Second)

	// All flags are "set"
	cfg, err := loadConfigWithChecker(f, testOpts(), alwaysSet)
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Verify flags take precedence over env vars
	if cfg.Port != 9000 {
		t.Errorf("Port = %d, want 9000 (flag value)", cfg.Port)
	}
	if cfg.DBPath != "/flag/path.db" {
		t.Errorf("DBPath = %s, want /flag/path.db (flag value)", cfg.DBPath)
	}
	if cfg.WorkDir != "/flag/workdir" {
		t.Errorf("WorkDir = %s, want /flag/workdir (flag value)", cfg.WorkDir)
	}
	if cfg.ClaudeCmd != "/flag/claude" {
		t.Errorf("ClaudeCmd = %s, want /flag/claude (flag value)", cfg.ClaudeCmd)
	}
	if cfg.PromptTimeout != 15*time.Minute {
		t.Errorf("PromptTimeout = %v, want 15m (flag value)", cfg.PromptTimeout)
	}
	if cfg.ShutdownTimeout != 45*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 45s (flag value)", cfg.ShutdownTimeout)
	}
}

func TestLoadConfig_PartialFlagOverride(t *testing.T) {
	// Set all env vars
	os.Setenv("CHAI_PORT", "3000")
	os.Setenv("CHAI_DB", "/env/path.db")
	defer clearEnvVars()

	// Flag values
	f := newTestFlags(9000, "/flag/path.db", defaultWorkDir, defaultClaudeCmd, defaultPromptTimeout, defaultShutdownTimeout)

	// Only port flag is set
	cfg, err := loadConfigWithChecker(f, testOpts(), makeChecker("port"))
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}

	// Port should come from flag, DB from env
	if cfg.Port != 9000 {
		t.Errorf("Port = %d, want 9000 (flag value)", cfg.Port)
	}
	if cfg.DBPath != "/env/path.db" {
		t.Errorf("DBPath = %s, want /env/path.db (env value)", cfg.DBPath)
	}
}

func TestLoadConfig_InvalidPort(t *testing.T) {
	clearEnvVars()
	os.Setenv("CHAI_PORT", "invalid")
	defer clearEnvVars()

	f := newTestFlags(defaultPort, defaultDBPath, defaultWorkDir, defaultClaudeCmd, defaultPromptTimeout, defaultShutdownTimeout)

	_, err := loadConfigWithChecker(f, testOpts(), neverSet)
	if err == nil {
		t.Error("LoadConfig should fail with invalid port")
	}
}

func TestLoadConfig_PortOutOfRange(t *testing.T) {
	tests := []struct {
		name string
		port string
	}{
		{"negative", "-1"},
		{"zero", "0"},
		{"too high", "65536"},
		{"way too high", "100000"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnvVars()
			os.Setenv("CHAI_PORT", tt.port)
			defer clearEnvVars()

			f := newTestFlags(defaultPort, defaultDBPath, defaultWorkDir, defaultClaudeCmd, defaultPromptTimeout, defaultShutdownTimeout)

			_, err := loadConfigWithChecker(f, testOpts(), neverSet)
			if err == nil {
				t.Errorf("LoadConfig should fail with port %s", tt.port)
			}
		})
	}
}

func TestLoadConfig_ValidPortBoundaries(t *testing.T) {
	tests := []struct {
		name string
		port string
		want int
	}{
		{"min valid", "1", 1},
		{"max valid", "65535", 65535},
		{"common http", "80", 80},
		{"common https", "443", 443},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnvVars()
			os.Setenv("CHAI_PORT", tt.port)
			defer clearEnvVars()

			f := newTestFlags(defaultPort, defaultDBPath, defaultWorkDir, defaultClaudeCmd, defaultPromptTimeout, defaultShutdownTimeout)

			cfg, err := loadConfigWithChecker(f, testOpts(), neverSet)
			if err != nil {
				t.Fatalf("LoadConfig failed for port %s: %v", tt.port, err)
			}
			if cfg.Port != tt.want {
				t.Errorf("Port = %d, want %d", cfg.Port, tt.want)
			}
		})
	}
}

func TestLoadConfig_InvalidDuration(t *testing.T) {
	clearEnvVars()
	os.Setenv("CHAI_PROMPT_TIMEOUT", "not-a-duration")
	defer clearEnvVars()

	f := newTestFlags(defaultPort, defaultDBPath, defaultWorkDir, defaultClaudeCmd, defaultPromptTimeout, defaultShutdownTimeout)

	_, err := loadConfigWithChecker(f, testOpts(), neverSet)
	if err == nil {
		t.Error("LoadConfig should fail with invalid duration")
	}
}

func TestLoadConfig_NegativeDuration(t *testing.T) {
	tests := []struct {
		name   string
		envVar string
		value  string
	}{
		{"negative prompt timeout", "CHAI_PROMPT_TIMEOUT", "-5m"},
		{"zero prompt timeout", "CHAI_PROMPT_TIMEOUT", "0s"},
		{"negative shutdown timeout", "CHAI_SHUTDOWN_TIMEOUT", "-30s"},
		{"zero shutdown timeout", "CHAI_SHUTDOWN_TIMEOUT", "0"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearEnvVars()
			os.Setenv(tt.envVar, tt.value)
			defer clearEnvVars()

			f := newTestFlags(defaultPort, defaultDBPath, defaultWorkDir, defaultClaudeCmd, defaultPromptTimeout, defaultShutdownTimeout)

			_, err := loadConfigWithChecker(f, testOpts(), neverSet)
			if err == nil {
				t.Errorf("LoadConfig should fail with %s=%s", tt.envVar, tt.value)
			}
		})
	}
}

func clearEnvVars() {
	os.Unsetenv("CHAI_PORT")
	os.Unsetenv("CHAI_DB")
	os.Unsetenv("CHAI_WORKDIR")
	os.Unsetenv("CHAI_CLAUDE_CMD")
	os.Unsetenv("CHAI_PROMPT_TIMEOUT")
	os.Unsetenv("CHAI_SHUTDOWN_TIMEOUT")
}
