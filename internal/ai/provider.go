package ai

import "context"

// AIProvider abstracts AI model invocation (API, CLI, or other)
//
// This interface allows switching between different AI providers
// (Anthropic API, Claude CLI, OpenAI, local models, etc.) without
// changing the supervisor business logic.
//
// The provider handles the low-level invocation details (HTTP calls,
// CLI execution, response parsing), while the supervisor handles
// the high-level logic (prompt building, response interpretation,
// retry logic, circuit breaking).
type AIProvider interface {
	// Invoke calls the AI model with the given parameters
	// Returns the response text, token counts, and any error
	//
	// The provider should NOT implement retry logic - that's handled
	// by the supervisor's retryWithBackoff() function.
	Invoke(ctx context.Context, params InvokeParams) (*InvokeResult, error)
}

// InvokeParams contains parameters for an AI invocation
type InvokeParams struct {
	// Operation is a name for logging/debugging (e.g., "assessment", "planning")
	// Not sent to the AI model, just for observability
	Operation string

	// Prompt is the text prompt to send to the AI
	Prompt string

	// MaxTokens is the maximum number of tokens to generate
	MaxTokens int

	// Model is an optional model override
	// If empty, the provider uses its default model
	// For API provider: full model name (e.g., "claude-3-5-haiku-20241022")
	// For CLI provider: alias (e.g., "sonnet", "haiku", "opus")
	Model string
}

// InvokeResult contains the AI response and metadata
type InvokeResult struct {
	// Text is the response text from the AI
	Text string

	// InputTokens is the number of input tokens consumed
	// Includes prompt tokens + any cache tokens
	InputTokens int

	// OutputTokens is the number of output tokens generated
	OutputTokens int
}
