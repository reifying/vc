package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ClaudeCLIProvider implements AIProvider using Claude CLI
type ClaudeCLIProvider struct {
	claudePath string // Path to claude binary (e.g., ~/.claude/local/claude)
	model      string // Default model alias (sonnet, haiku, opus)
}

// NewClaudeCLIProvider creates a new Claude CLI provider
func NewClaudeCLIProvider(model string) (*ClaudeCLIProvider, error) {
	// Get Claude CLI path from environment or use default
	claudePath := os.Getenv("VC_CLAUDE_PATH")
	if claudePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
		claudePath = filepath.Join(home, ".claude", "local", "claude")
	}

	// Verify Claude CLI exists
	if _, err := os.Stat(claudePath); err != nil {
		return nil, fmt.Errorf("claude CLI not found at %s: %w (set VC_CLAUDE_PATH to override)", claudePath, err)
	}

	// Map model to CLI alias
	if model == "" {
		model = "sonnet" // Default to Sonnet
	} else {
		model = getModelForCLI(model)
	}

	return &ClaudeCLIProvider{
		claudePath: claudePath,
		model:      model,
	}, nil
}

// Invoke calls Claude CLI
func (p *ClaudeCLIProvider) Invoke(ctx context.Context, params InvokeParams) (*InvokeResult, error) {
	// Use model override if specified, otherwise use default
	model := params.Model
	if model == "" {
		model = p.model
	} else {
		model = getModelForCLI(model)
	}

	// Build command arguments
	args := []string{
		"--print",
		"--output-format", "json",
		"--dangerously-skip-permissions",
	}

	// Add model if not default
	if model != "" {
		args = append(args, "--model", model)
	}

	// Add prompt
	args = append(args, params.Prompt)

	// Execute command
	cmd := exec.CommandContext(ctx, p.claudePath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("claude CLI failed: %w (output: %s)", err, string(output))
	}

	// Parse JSON response array
	var responses []ClaudeCLIResponse
	if err := json.Unmarshal(output, &responses); err != nil {
		return nil, fmt.Errorf("failed to parse CLI response: %w (output: %s)", err, string(output))
	}

	// Find result object
	var result *ClaudeCLIResponse
	for i := range responses {
		if responses[i].Type == "result" {
			result = &responses[i]
			break
		}
	}

	if result == nil {
		return nil, fmt.Errorf("no result object found in CLI response")
	}

	if result.IsError {
		return nil, fmt.Errorf("claude CLI returned error: %s", result.Result)
	}

	// Combine all token counts (including cache tokens)
	inputTokens := result.Usage.InputTokens +
		result.Usage.CacheReadInputTokens +
		result.Usage.CacheCreationInputTokens

	return &InvokeResult{
		Text:         result.Result,
		InputTokens:  inputTokens,
		OutputTokens: result.Usage.OutputTokens,
	}, nil
}
