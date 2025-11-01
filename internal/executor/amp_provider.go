package executor

import (
	"context"
	"os/exec"
)

// AmpProvider implements AgentProvider for Sourcegraph Amp.
// Amp is a CLI agent that supports structured JSON event streaming via --stream-json.
//
// Command structure:
//   amp --dangerously-allow-all --execute "prompt" [--stream-json]
//
// Key features:
// - Supports --stream-json for structured event parsing
// - Uses --execute for single-shot autonomous execution
// - Uses --dangerously-allow-all to bypass permission checks (safe in sandbox or VC context)
type AmpProvider struct{}

// Name returns the provider name
func (p *AmpProvider) Name() string {
	return "amp"
}

// BuildCommand constructs the amp CLI command
func (p *AmpProvider) BuildCommand(ctx context.Context, cfg AgentConfig, prompt string) (*exec.Cmd, error) {
	args := []string{}

	// Always bypass permission checks for autonomous agent operation (vc-117)
	// This is required for VC to operate autonomously without human intervention
	// Safe because:
	// 1. When sandboxed: Isolated environment with no risk to main codebase
	// 2. When not sandboxed: VC is designed to work autonomously on its own codebase
	//    and the results go through quality gates before being committed
	args = append(args, "--dangerously-allow-all")

	// amp requires --execute for single-shot execution mode
	args = append(args, "--execute", prompt)

	// Enable structured JSON event streaming if requested
	if cfg.StreamJSON {
		args = append(args, "--stream-json")
	}

	return exec.CommandContext(ctx, "amp", args...), nil
}

// SupportsStreamJSON returns true - Amp supports --stream-json flag
func (p *AmpProvider) SupportsStreamJSON() bool {
	return true
}
