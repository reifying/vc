package executor

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ClaudeCodeProvider implements AgentProvider for Anthropic Claude Code CLI.
// Claude Code is the official Anthropic CLI agent with rich tooling support.
//
// Command structure:
//   claude --print [--output-format=stream-json] [--dangerously-skip-permissions] "prompt"
//
// Key features:
// - Uses --print for non-interactive, single-shot execution (similar to amp's --execute)
// - Uses --dangerously-skip-permissions to bypass permission checks (safe in sandbox or VC context)
// - Supports --output-format=stream-json for structured event streaming (similar to amp's --stream-json)
//
// Environment variables:
// - VC_CLAUDE_PATH: Override path to Claude CLI (default: ~/.claude/local/claude)
// - VC_CLAUDE_ARGS: Custom arguments (default: --dangerously-skip-permissions)
//
// Note: Claude Code's --stream-json output format may differ from Amp's format.
// The event parsing logic in convertJSONToEvent() is currently tuned for Amp's format.
// Future work may be needed to handle both formats gracefully.
type ClaudeCodeProvider struct{}

// Name returns the provider name
func (p *ClaudeCodeProvider) Name() string {
	return "claude-code"
}

// BuildCommand constructs the claude CLI command
func (p *ClaudeCodeProvider) BuildCommand(ctx context.Context, cfg AgentConfig, prompt string) (*exec.Cmd, error) {
	// Get Claude CLI path from environment, default to standard location
	// VC_CLAUDE_PATH allows users to override if Claude is installed elsewhere
	claudePath := os.Getenv("VC_CLAUDE_PATH")
	if claudePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			// Fallback if we can't get home directory
			claudePath = "claude"
		} else {
			claudePath = filepath.Join(home, ".claude", "local", "claude")
		}
	}

	// Build command arguments
	// CRITICAL: --print flag makes Claude exit after completing work (non-interactive mode)
	// Without it, Claude runs interactively and never exits, causing VC to timeout
	args := []string{"--print"}

	// Add streaming JSON output if configured
	// This enables real-time event parsing (tool usage, git operations, etc.)
	if cfg.StreamJSON {
		args = append(args, "--output-format", "stream-json")
	}

	// Get additional Claude args from environment
	// VC_CLAUDE_ARGS allows users to customize (e.g., remove --dangerously-skip-permissions, add --model, etc.)
	// Default: --dangerously-skip-permissions for autonomous operation (vc-117)
	argsEnv := os.Getenv("VC_CLAUDE_ARGS")
	if argsEnv == "" {
		args = append(args, "--dangerously-skip-permissions")
	} else {
		args = append(args, strings.Fields(argsEnv)...)
	}

	// Add prompt last
	args = append(args, prompt)

	return exec.CommandContext(ctx, claudePath, args...), nil
}

// SupportsStreamJSON returns true - Claude Code supports --output-format=stream-json
// Note: The JSON event format may differ from Amp's format, requiring parser adaptation
func (p *ClaudeCodeProvider) SupportsStreamJSON() bool {
	return true
}
