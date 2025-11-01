package executor

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// AgentProvider abstracts the underlying agent execution mechanism.
// This allows runtime selection between different agent backends (amp, claude-code, etc.)
// without changing the core executor logic.
//
// Similar to AIProvider pattern, this enables:
// - Experimentation with different agent implementations
// - Testing with mock providers
// - Graceful fallback if a provider is unavailable
//
// Providers are selected via VC_AGENT_PROVIDER environment variable:
// - "amp" (default): Sourcegraph Amp agent
// - "claude-code": Anthropic Claude Code CLI
type AgentProvider interface {
	// Name returns the provider name for logging and debugging
	Name() string

	// BuildCommand constructs the command to execute the agent
	// Returns an exec.Cmd configured with the appropriate arguments
	BuildCommand(ctx context.Context, cfg AgentConfig, prompt string) (*exec.Cmd, error)

	// SupportsStreamJSON returns true if the provider supports structured JSON event streaming
	// If true, the executor will attempt to parse JSON events from stdout
	SupportsStreamJSON() bool
}

// NewAgentProvider creates an agent provider based on configuration.
// The provider type is determined by the VC_AGENT_PROVIDER environment variable:
// - "amp": Use Sourcegraph Amp (default)
// - "claude-code": Use Anthropic Claude Code CLI
//
// If VC_AGENT_PROVIDER is not set or empty, defaults to "amp".
func NewAgentProvider(providerType string) (AgentProvider, error) {
	// Default to amp if not specified
	if providerType == "" {
		providerType = os.Getenv("VC_AGENT_PROVIDER")
		if providerType == "" {
			providerType = "amp"
		}
	}

	switch providerType {
	case "amp":
		return &AmpProvider{}, nil
	case "claude-code":
		return &ClaudeCodeProvider{}, nil
	default:
		return nil, fmt.Errorf("unsupported agent provider: %s (supported: amp, claude-code)", providerType)
	}
}
