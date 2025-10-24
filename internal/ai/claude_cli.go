package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ClaudeCLIResponse represents the JSON response from Claude CLI
type ClaudeCLIResponse struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	Result    string `json:"result"`
	IsError   bool   `json:"is_error"`
	Usage     struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	} `json:"usage"`
	TotalCostUSD float64 `json:"total_cost_usd"`
}

// invokeClaudeCLI invokes Claude CLI and returns the parsed response.
// This is used by AI supervisor to use Max plan instead of API billing.
func (s *Supervisor) invokeClaudeCLI(ctx context.Context, prompt string, model string) (string, int, int, error) {
	// Get Claude CLI path from environment, default to standard location
	claudePath := os.Getenv("VC_CLAUDE_PATH")
	if claudePath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", 0, 0, fmt.Errorf("failed to get home directory: %w", err)
		}
		claudePath = filepath.Join(home, ".claude", "local", "claude")
	}

	// Build command arguments
	args := []string{
		"--print",
		"--output-format", "json",
		"--dangerously-skip-permissions",
	}

	// Add model if specified
	if model != "" {
		args = append(args, "--model", model)
	}

	// Add prompt
	args = append(args, prompt)

	// Create command with context for timeout support
	cmd := exec.CommandContext(ctx, claudePath, args...)

	// Run command and capture output
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", 0, 0, fmt.Errorf("claude CLI failed: %w (output: %s)", err, string(output))
	}

	// Parse JSON response array
	var responses []ClaudeCLIResponse
	if err := json.Unmarshal(output, &responses); err != nil {
		return "", 0, 0, fmt.Errorf("failed to parse CLI response: %w (output: %s)", err, string(output))
	}

	// Find the result object
	var result *ClaudeCLIResponse
	for i := range responses {
		if responses[i].Type == "result" {
			result = &responses[i]
			break
		}
	}

	if result == nil {
		return "", 0, 0, fmt.Errorf("no result object found in CLI response")
	}

	if result.IsError {
		return "", 0, 0, fmt.Errorf("claude CLI returned error: %s", result.Result)
	}

	// Extract token counts (combining cache tokens with regular tokens)
	inputTokens := result.Usage.InputTokens + result.Usage.CacheReadInputTokens + result.Usage.CacheCreationInputTokens
	outputTokens := result.Usage.OutputTokens

	return result.Result, inputTokens, outputTokens, nil
}

// invokeCLIWithRetry invokes Claude CLI with retry logic, similar to API retry behavior
func (s *Supervisor) invokeCLIWithRetry(ctx context.Context, operation string, prompt string, model string) (string, int, int, error) {
	var responseText string
	var inputTokens, outputTokens int
	var lastErr error

	// Use the existing retry mechanism
	err := s.retryWithBackoff(ctx, operation, func(attemptCtx context.Context) error {
		text, inTokens, outTokens, cliErr := s.invokeClaudeCLI(attemptCtx, prompt, model)
		if cliErr != nil {
			lastErr = cliErr
			return cliErr
		}
		responseText = text
		inputTokens = inTokens
		outputTokens = outTokens
		return nil
	})

	if err != nil {
		if lastErr != nil {
			return "", 0, 0, fmt.Errorf("claude CLI call failed after retries: %w", lastErr)
		}
		return "", 0, 0, fmt.Errorf("claude CLI call failed: %w", err)
	}

	return responseText, inputTokens, outputTokens, nil
}

// getModelForCLI converts supervisor model to CLI model alias
func getModelForCLI(supervisorModel string) string {
	// Map full model names to CLI aliases
	switch {
	case strings.Contains(supervisorModel, "sonnet"):
		return "sonnet"
	case strings.Contains(supervisorModel, "haiku"):
		return "haiku"
	case strings.Contains(supervisorModel, "opus"):
		return "opus"
	default:
		// Default to sonnet for AI supervision
		return "sonnet"
	}
}
