package ai

import (
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
