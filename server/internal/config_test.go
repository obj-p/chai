package internal

import (
	"os"
	"testing"
	"time"
)

func TestLoadConfig_Defaults(t *testing.T) {
	// Clear any existing env vars
	os.Unsetenv("CHAI_PORT")
	os.Unsetenv("CHAI_DB")
	os.Unsetenv("CHAI_WORKDIR")
	os.Unsetenv("CHAI_CLAUDE_CMD")
	os.Unsetenv("CHAI_PROMPT_TIMEOUT")
	os.Unsetenv("CHAI_SHUTDOWN_TIMEOUT")

	// Create mock flags with default values (simulating no flags set)
	port := defaultPort
	dbPath := defaultDBPath
	workDir := defaultWorkDir
	claudeCmd := defaultClaudeCmd
	promptTimeout := defaultPromptTimeout
	shutdownTimeout := defaultShutdownTimeout

	f := &flags{
		port:            &port,
		dbPath:          &dbPath,
		workDir:         &workDir,
		claudeCmd:       &claudeCmd,
		promptTimeout:   &promptTimeout,
		shutdownTimeout: &shutdownTimeout,
	}

	cfg, err := LoadConfig(f)
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
	defer func() {
		os.Unsetenv("CHAI_PORT")
		os.Unsetenv("CHAI_DB")
		os.Unsetenv("CHAI_WORKDIR")
		os.Unsetenv("CHAI_CLAUDE_CMD")
		os.Unsetenv("CHAI_PROMPT_TIMEOUT")
		os.Unsetenv("CHAI_SHUTDOWN_TIMEOUT")
	}()

	// Create mock flags with default values (simulating no flags set)
	port := defaultPort
	dbPath := defaultDBPath
	workDir := defaultWorkDir
	claudeCmd := defaultClaudeCmd
	promptTimeout := defaultPromptTimeout
	shutdownTimeout := defaultShutdownTimeout

	f := &flags{
		port:            &port,
		dbPath:          &dbPath,
		workDir:         &workDir,
		claudeCmd:       &claudeCmd,
		promptTimeout:   &promptTimeout,
		shutdownTimeout: &shutdownTimeout,
	}

	cfg, err := LoadConfig(f)
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

func TestLoadConfig_InvalidPort(t *testing.T) {
	os.Setenv("CHAI_PORT", "invalid")
	defer os.Unsetenv("CHAI_PORT")

	port := defaultPort
	dbPath := defaultDBPath
	workDir := defaultWorkDir
	claudeCmd := defaultClaudeCmd
	promptTimeout := defaultPromptTimeout
	shutdownTimeout := defaultShutdownTimeout

	f := &flags{
		port:            &port,
		dbPath:          &dbPath,
		workDir:         &workDir,
		claudeCmd:       &claudeCmd,
		promptTimeout:   &promptTimeout,
		shutdownTimeout: &shutdownTimeout,
	}

	_, err := LoadConfig(f)
	if err == nil {
		t.Error("LoadConfig should fail with invalid port")
	}
}

func TestLoadConfig_InvalidDuration(t *testing.T) {
	os.Setenv("CHAI_PROMPT_TIMEOUT", "not-a-duration")
	defer os.Unsetenv("CHAI_PROMPT_TIMEOUT")

	port := defaultPort
	dbPath := defaultDBPath
	workDir := defaultWorkDir
	claudeCmd := defaultClaudeCmd
	promptTimeout := defaultPromptTimeout
	shutdownTimeout := defaultShutdownTimeout

	f := &flags{
		port:            &port,
		dbPath:          &dbPath,
		workDir:         &workDir,
		claudeCmd:       &claudeCmd,
		promptTimeout:   &promptTimeout,
		shutdownTimeout: &shutdownTimeout,
	}

	_, err := LoadConfig(f)
	if err == nil {
		t.Error("LoadConfig should fail with invalid duration")
	}
}
