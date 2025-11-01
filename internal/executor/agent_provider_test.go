package executor

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/vc/internal/types"
)

// TestNewAgentProvider_Default verifies default provider selection
func TestNewAgentProvider_Default(t *testing.T) {
	// Clear any env var to test true default
	oldEnv := os.Getenv("VC_AGENT_PROVIDER")
	os.Setenv("VC_AGENT_PROVIDER", "")
	defer os.Setenv("VC_AGENT_PROVIDER", oldEnv)

	provider, err := NewAgentProvider("")
	if err != nil {
		t.Fatalf("Failed to create default provider: %v", err)
	}

	// Default should be amp
	if provider.Name() != "amp" {
		t.Errorf("Expected default provider to be 'amp', got '%s'", provider.Name())
	}
}

// TestNewAgentProvider_Amp verifies amp provider creation
func TestNewAgentProvider_Amp(t *testing.T) {
	provider, err := NewAgentProvider("amp")
	if err != nil {
		t.Fatalf("Failed to create amp provider: %v", err)
	}

	if provider.Name() != "amp" {
		t.Errorf("Expected provider name 'amp', got '%s'", provider.Name())
	}

	if !provider.SupportsStreamJSON() {
		t.Error("Amp provider should support stream JSON")
	}
}

// TestNewAgentProvider_ClaudeCode verifies claude-code provider creation
func TestNewAgentProvider_ClaudeCode(t *testing.T) {
	provider, err := NewAgentProvider("claude-code")
	if err != nil {
		t.Fatalf("Failed to create claude-code provider: %v", err)
	}

	if provider.Name() != "claude-code" {
		t.Errorf("Expected provider name 'claude-code', got '%s'", provider.Name())
	}

	if !provider.SupportsStreamJSON() {
		t.Error("Claude Code provider should support stream JSON")
	}
}

// TestNewAgentProvider_Invalid verifies error handling for unsupported provider
func TestNewAgentProvider_Invalid(t *testing.T) {
	_, err := NewAgentProvider("invalid-provider")
	if err == nil {
		t.Fatal("Expected error for invalid provider, got nil")
	}

	if !strings.Contains(err.Error(), "unsupported agent provider") {
		t.Errorf("Expected 'unsupported agent provider' error, got: %v", err)
	}
}

// TestNewAgentProvider_FromEnvVar verifies provider selection from environment variable
func TestNewAgentProvider_FromEnvVar(t *testing.T) {
	tests := []struct {
		name        string
		envValue    string
		expectedName string
	}{
		{"env var amp", "amp", "amp"},
		{"env var claude-code", "claude-code", "claude-code"},
		{"env var empty defaults to amp", "", "amp"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			oldEnv := os.Getenv("VC_AGENT_PROVIDER")
			os.Setenv("VC_AGENT_PROVIDER", tt.envValue)
			defer os.Setenv("VC_AGENT_PROVIDER", oldEnv)

			provider, err := NewAgentProvider("")
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			if provider.Name() != tt.expectedName {
				t.Errorf("Expected provider name '%s', got '%s'", tt.expectedName, provider.Name())
			}
		})
	}
}

// TestAmpProvider_BuildCommand verifies amp command construction
func TestAmpProvider_BuildCommand(t *testing.T) {
	provider := &AmpProvider{}
	ctx := context.Background()
	cfg := AgentConfig{
		Type:       AgentTypeAmp,
		WorkingDir: "/tmp/test",
		Issue:      &types.Issue{ID: "vc-1", Title: "Test"},
		StreamJSON: false,
		Timeout:    5 * time.Minute,
	}
	prompt := "Fix the bug"

	cmd, err := provider.BuildCommand(ctx, cfg, prompt)
	if err != nil {
		t.Fatalf("BuildCommand failed: %v", err)
	}

	// Verify command name
	if len(cmd.Args) < 1 {
		t.Fatal("Expected at least one argument (command name)")
	}

	// Should have: amp, --dangerously-allow-all, --execute, prompt
	expectedArgs := 4
	if len(cmd.Args) != expectedArgs {
		t.Errorf("Expected %d args, got %d: %v", expectedArgs, len(cmd.Args), cmd.Args)
	}

	// Verify --dangerously-allow-all flag
	hasPermissionFlag := false
	for _, arg := range cmd.Args {
		if arg == "--dangerously-allow-all" {
			hasPermissionFlag = true
			break
		}
	}
	if !hasPermissionFlag {
		t.Error("Expected --dangerously-allow-all flag for autonomous operation")
	}

	// Verify --execute flag
	hasExecuteFlag := false
	for i, arg := range cmd.Args {
		if arg == "--execute" {
			hasExecuteFlag = true
			// Verify prompt follows --execute
			if i+1 < len(cmd.Args) && cmd.Args[i+1] != prompt {
				t.Errorf("Expected prompt '%s' after --execute, got '%s'", prompt, cmd.Args[i+1])
			}
			break
		}
	}
	if !hasExecuteFlag {
		t.Error("Expected --execute flag for single-shot execution")
	}
}

// TestAmpProvider_BuildCommand_StreamJSON verifies stream JSON flag
func TestAmpProvider_BuildCommand_StreamJSON(t *testing.T) {
	provider := &AmpProvider{}
	ctx := context.Background()
	cfg := AgentConfig{
		Type:       AgentTypeAmp,
		WorkingDir: "/tmp/test",
		Issue:      &types.Issue{ID: "vc-1", Title: "Test"},
		StreamJSON: true, // Enable stream JSON
		Timeout:    5 * time.Minute,
	}
	prompt := "Fix the bug"

	cmd, err := provider.BuildCommand(ctx, cfg, prompt)
	if err != nil {
		t.Fatalf("BuildCommand failed: %v", err)
	}

	// Verify --stream-json flag is present
	hasStreamJSONFlag := false
	for _, arg := range cmd.Args {
		if arg == "--stream-json" {
			hasStreamJSONFlag = true
			break
		}
	}
	if !hasStreamJSONFlag {
		t.Error("Expected --stream-json flag when StreamJSON is true")
	}

	// Should have: amp, --dangerously-allow-all, --execute, prompt, --stream-json
	expectedArgs := 5
	if len(cmd.Args) != expectedArgs {
		t.Errorf("Expected %d args with stream-json, got %d: %v", expectedArgs, len(cmd.Args), cmd.Args)
	}
}

// TestClaudeCodeProvider_BuildCommand verifies claude command construction
func TestClaudeCodeProvider_BuildCommand(t *testing.T) {
	provider := &ClaudeCodeProvider{}
	ctx := context.Background()
	cfg := AgentConfig{
		Type:       AgentTypeClaudeCode,
		WorkingDir: "/tmp/test",
		Issue:      &types.Issue{ID: "vc-1", Title: "Test"},
		StreamJSON: false,
		Timeout:    5 * time.Minute,
	}
	prompt := "Fix the bug"

	cmd, err := provider.BuildCommand(ctx, cfg, prompt)
	if err != nil {
		t.Fatalf("BuildCommand failed: %v", err)
	}

	// Verify command name
	if len(cmd.Args) < 1 {
		t.Fatal("Expected at least one argument (command name)")
	}

	// Verify --print flag (required for non-interactive mode)
	hasPrintFlag := false
	for _, arg := range cmd.Args {
		if arg == "--print" {
			hasPrintFlag = true
			break
		}
	}
	if !hasPrintFlag {
		t.Error("Expected --print flag for non-interactive execution")
	}

	// Verify --dangerously-skip-permissions flag (default when VC_CLAUDE_ARGS not set)
	hasPermissionFlag := false
	for _, arg := range cmd.Args {
		if arg == "--dangerously-skip-permissions" {
			hasPermissionFlag = true
			break
		}
	}
	if !hasPermissionFlag {
		t.Error("Expected --dangerously-skip-permissions flag for autonomous operation")
	}

	// Verify prompt is last argument
	if cmd.Args[len(cmd.Args)-1] != prompt {
		t.Errorf("Expected last arg to be prompt '%s', got '%s'", prompt, cmd.Args[len(cmd.Args)-1])
	}
}

// TestClaudeCodeProvider_BuildCommand_StreamJSON verifies stream JSON output format
func TestClaudeCodeProvider_BuildCommand_StreamJSON(t *testing.T) {
	provider := &ClaudeCodeProvider{}
	ctx := context.Background()
	cfg := AgentConfig{
		Type:       AgentTypeClaudeCode,
		WorkingDir: "/tmp/test",
		Issue:      &types.Issue{ID: "vc-1", Title: "Test"},
		StreamJSON: true, // Enable stream JSON
		Timeout:    5 * time.Minute,
	}
	prompt := "Fix the bug"

	cmd, err := provider.BuildCommand(ctx, cfg, prompt)
	if err != nil {
		t.Fatalf("BuildCommand failed: %v", err)
	}

	// Verify --output-format stream-json
	hasStreamJSON := false
	for i, arg := range cmd.Args {
		if arg == "--output-format" {
			if i+1 < len(cmd.Args) && cmd.Args[i+1] == "stream-json" {
				hasStreamJSON = true
			}
			break
		}
	}
	if !hasStreamJSON {
		t.Error("Expected --output-format stream-json when StreamJSON is true")
	}
}

// TestClaudeCodeProvider_BuildCommand_CustomPath verifies VC_CLAUDE_PATH env var
func TestClaudeCodeProvider_BuildCommand_CustomPath(t *testing.T) {
	oldPath := os.Getenv("VC_CLAUDE_PATH")
	os.Setenv("VC_CLAUDE_PATH", "/custom/path/to/claude")
	defer os.Setenv("VC_CLAUDE_PATH", oldPath)

	provider := &ClaudeCodeProvider{}
	ctx := context.Background()
	cfg := AgentConfig{
		Type:       AgentTypeClaudeCode,
		WorkingDir: "/tmp/test",
		Issue:      &types.Issue{ID: "vc-1", Title: "Test"},
		Timeout:    5 * time.Minute,
	}
	prompt := "Fix the bug"

	cmd, err := provider.BuildCommand(ctx, cfg, prompt)
	if err != nil {
		t.Fatalf("BuildCommand failed: %v", err)
	}

	// Verify custom path is used
	if cmd.Path != "/custom/path/to/claude" {
		t.Errorf("Expected custom path '/custom/path/to/claude', got '%s'", cmd.Path)
	}
}

// TestClaudeCodeProvider_BuildCommand_CustomArgs verifies VC_CLAUDE_ARGS env var
func TestClaudeCodeProvider_BuildCommand_CustomArgs(t *testing.T) {
	oldArgs := os.Getenv("VC_CLAUDE_ARGS")
	os.Setenv("VC_CLAUDE_ARGS", "--model sonnet --verbose")
	defer os.Setenv("VC_CLAUDE_ARGS", oldArgs)

	provider := &ClaudeCodeProvider{}
	ctx := context.Background()
	cfg := AgentConfig{
		Type:       AgentTypeClaudeCode,
		WorkingDir: "/tmp/test",
		Issue:      &types.Issue{ID: "vc-1", Title: "Test"},
		Timeout:    5 * time.Minute,
	}
	prompt := "Fix the bug"

	cmd, err := provider.BuildCommand(ctx, cfg, prompt)
	if err != nil {
		t.Fatalf("BuildCommand failed: %v", err)
	}

	// Verify custom args are present
	hasModelFlag := false
	hasVerboseFlag := false
	for i, arg := range cmd.Args {
		if arg == "--model" {
			if i+1 < len(cmd.Args) && cmd.Args[i+1] == "sonnet" {
				hasModelFlag = true
			}
		}
		if arg == "--verbose" {
			hasVerboseFlag = true
		}
	}
	if !hasModelFlag {
		t.Error("Expected --model sonnet from VC_CLAUDE_ARGS")
	}
	if !hasVerboseFlag {
		t.Error("Expected --verbose from VC_CLAUDE_ARGS")
	}

	// Verify default --dangerously-skip-permissions is NOT present (replaced by custom args)
	hasDefaultPermissionFlag := false
	for _, arg := range cmd.Args {
		if arg == "--dangerously-skip-permissions" {
			hasDefaultPermissionFlag = true
			break
		}
	}
	if hasDefaultPermissionFlag {
		t.Error("Expected custom args to replace default --dangerously-skip-permissions")
	}
}

// TestSpawnAgent_WithProvider verifies agent spawning with custom provider
func TestSpawnAgent_WithProvider(t *testing.T) {
	// Create a custom provider
	provider := &AmpProvider{}

	ctx := context.Background()
	cfg := AgentConfig{
		Type:       AgentTypeAmp,
		WorkingDir: ".",
		Issue:      &types.Issue{ID: "vc-test", Title: "Test"},
		Timeout:    1 * time.Second, // Short timeout for test
		Provider:   provider,        // Use custom provider
	}
	prompt := "echo test"

	// Note: This will fail to spawn because we're not actually running amp
	// We're just testing that the provider is used correctly
	_, err := SpawnAgent(ctx, cfg, prompt)

	// We expect an error because amp binary doesn't exist
	// But we can verify the error is from starting the process, not provider creation
	if err == nil {
		t.Fatal("Expected error when spawning non-existent amp binary")
	}

	// The error should be about starting the agent, not about provider
	if !strings.Contains(err.Error(), "failed to start agent") {
		t.Errorf("Expected 'failed to start agent' error, got: %v", err)
	}
}
