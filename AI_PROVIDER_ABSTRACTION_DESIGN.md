# AI Provider Abstraction Design (vc-11)

**Status:** Design Review
**Created:** 2025-10-24
**Issue:** vc-11

## Problem Statement

The current Claude CLI integration hardcoded CLI calls throughout the AI supervisor, completely replacing the Anthropic API. This prevents merging to main because:

1. **Breaks existing workflows** - Users without Claude Code Max cannot use VC
2. **No configuration** - Hardcoded to CLI, no way to select provider
3. **Dead code** - Anthropic client created but unused
4. **Test regression** - Tests skipped instead of adapted

**Goal:** Create a pluggable abstraction that supports both Anthropic API and Claude CLI, with runtime selection via configuration.

## Design Principles

1. **Single Responsibility** - Provider handles invocation, supervisor handles business logic
2. **Extensibility** - Easy to add new providers (OpenAI, local models, etc.)
3. **Backwards Compatible** - Default to API provider for existing users
4. **Zero Overhead** - No performance penalty from abstraction
5. **Fail-Fast** - Clear errors for misconfiguration

## Interface Design

### Core Interface

```go
package ai

import "context"

// AIProvider abstracts AI model invocation (API, CLI, or other)
type AIProvider interface {
	// Invoke calls the AI model with the given parameters
	// Returns the response text, token counts, and any error
	Invoke(ctx context.Context, params InvokeParams) (*InvokeResult, error)
}

// InvokeParams contains parameters for an AI invocation
type InvokeParams struct {
	Operation string // Operation name for logging/retry (e.g., "assessment", "planning")
	Prompt    string // The prompt to send to the AI
	MaxTokens int    // Maximum tokens to generate
	Model     string // Optional model override (empty = use provider default)
}

// InvokeResult contains the AI response and metadata
type InvokeResult struct {
	Text         string // The response text
	InputTokens  int    // Number of input tokens consumed
	OutputTokens int    // Number of output tokens generated
}
```

### Design Rationale

**Why struct-based params instead of individual parameters?**
- Extensible: Can add fields (temperature, top_p, system prompt) without breaking API
- Readable: Call sites are self-documenting
- Optional fields: Model override is optional, easier with struct

**Why separate Invoke() instead of per-function methods?**
- All 11 supervisor functions follow the same pattern (prompt in, text out)
- Providers don't need to know about assessment vs planning vs recovery
- Supervisor handles prompt building and response parsing
- Simpler interface = easier to implement new providers

**Why no retry logic in provider?**
- Retry is a cross-cutting concern handled by supervisor
- Supervisor's retryWithBackoff works with any provider
- Providers just do single attempts
- Simpler provider implementations

## Provider Implementations

### 1. Anthropic API Provider

Restore original API-based implementation as a provider.

```go
package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// AnthropicAPIProvider implements AIProvider using Anthropic SDK
type AnthropicAPIProvider struct {
	client *anthropic.Client
	model  string // Default model (e.g., "claude-sonnet-4-5-20250929")
}

// NewAnthropicAPIProvider creates a new Anthropic API provider
func NewAnthropicAPIProvider(apiKey string, model string) (*AnthropicAPIProvider, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("API key is required for Anthropic API provider")
	}
	if model == "" {
		model = "claude-sonnet-4-5-20250929" // Default to Sonnet 4.5
	}

	client := anthropic.NewClient(option.WithAPIKey(apiKey))

	return &AnthropicAPIProvider{
		client: &client,
		model:  model,
	}, nil
}

// Invoke calls Anthropic API
func (p *AnthropicAPIProvider) Invoke(ctx context.Context, params InvokeParams) (*InvokeResult, error) {
	// Use model override if specified, otherwise use default
	model := params.Model
	if model == "" {
		model = p.model
	}

	// Call Anthropic API
	response, err := p.client.Messages.New(ctx, anthropic.MessageNewParams{
		Model:     anthropic.Model(model),
		MaxTokens: int64(params.MaxTokens),
		Messages: []anthropic.MessageParam{
			anthropic.NewUserMessage(anthropic.NewTextBlock(params.Prompt)),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("anthropic API call failed: %w", err)
	}

	// Extract text from response blocks
	var text strings.Builder
	for _, block := range response.Content {
		if block.Type == "text" {
			text.WriteString(block.Text)
		}
	}

	return &InvokeResult{
		Text:         text.String(),
		InputTokens:  int(response.Usage.InputTokens),
		OutputTokens: int(response.Usage.OutputTokens),
	}, nil
}
```

### 2. Claude CLI Provider

Wrap existing CLI implementation in provider interface.

```go
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
		model = mapModelToCLI(model)
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
		model = mapModelToCLI(model)
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

	// Combine all token counts
	inputTokens := result.Usage.InputTokens +
		result.Usage.CacheReadInputTokens +
		result.Usage.CacheCreationInputTokens

	return &InvokeResult{
		Text:         result.Result,
		InputTokens:  inputTokens,
		OutputTokens: result.Usage.OutputTokens,
	}, nil
}

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

// mapModelToCLI converts full model names to CLI aliases
func mapModelToCLI(supervisorModel string) string {
	switch {
	case strings.Contains(supervisorModel, "sonnet"):
		return "sonnet"
	case strings.Contains(supervisorModel, "haiku"):
		return "haiku"
	case strings.Contains(supervisorModel, "opus"):
		return "opus"
	default:
		return "sonnet" // Default to sonnet
	}
}
```

## Configuration Mechanism

### Environment Variable (Recommended)

```bash
# API provider (default)
export VC_AI_PROVIDER=api
export ANTHROPIC_API_KEY=sk-ant-...

# CLI provider
export VC_AI_PROVIDER=cli
# Optional: override CLI path
export VC_CLAUDE_PATH=/custom/path/to/claude
```

### Config Struct

```go
type Config struct {
	Provider string // "api" or "cli" (default: "api")
	APIKey   string // Required for API provider
	Model    string // Model name or alias
	Store    storage.Storage
	Retry    RetryConfig
}
```

### Provider Selection Logic

```go
func NewSupervisor(cfg *Config) (*Supervisor, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}

	// Determine provider type
	providerType := os.Getenv("VC_AI_PROVIDER")
	if providerType == "" {
		providerType = cfg.Provider
	}
	if providerType == "" {
		providerType = "api" // Default to API for backwards compatibility
	}

	// Create provider
	var provider AIProvider
	var err error

	switch providerType {
	case "api":
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY required for API provider")
		}

		model := cfg.Model
		if model == "" {
			model = "claude-sonnet-4-5-20250929"
		}

		provider, err = NewAnthropicAPIProvider(apiKey, model)
		if err != nil {
			return nil, fmt.Errorf("failed to create API provider: %w", err)
		}

		fmt.Printf("Using Anthropic API provider (model: %s)\n", model)

	case "cli":
		model := cfg.Model
		if model == "" {
			model = "sonnet"
		}

		provider, err = NewClaudeCLIProvider(model)
		if err != nil {
			return nil, fmt.Errorf("failed to create CLI provider: %w", err)
		}

		fmt.Printf("Using Claude CLI provider (model: %s)\n", model)

	default:
		return nil, fmt.Errorf("unknown provider type: %s (use 'api' or 'cli')", providerType)
	}

	// Use default retry config if not specified
	retry := cfg.Retry
	if retry.MaxRetries == 0 {
		retry = DefaultRetryConfig()
	}

	// Initialize circuit breaker if enabled
	var circuitBreaker *CircuitBreaker
	if retry.CircuitBreakerEnabled {
		circuitBreaker = NewCircuitBreaker(
			retry.FailureThreshold,
			retry.SuccessThreshold,
			retry.OpenTimeout,
		)
	}

	// Initialize concurrency limiter
	var concurrencySem *semaphore.Weighted
	if retry.MaxConcurrentCalls > 0 {
		concurrencySem = semaphore.NewWeighted(int64(retry.MaxConcurrentCalls))
	}

	return &Supervisor{
		provider:       provider,
		store:          cfg.Store,
		retry:          retry,
		circuitBreaker: circuitBreaker,
		concurrencySem: concurrencySem,
	}, nil
}
```

## Supervisor Refactoring

### Updated Supervisor Struct

```go
type Supervisor struct {
	provider       AIProvider        // NEW: provider abstraction
	store          storage.Storage
	retry          RetryConfig
	circuitBreaker *CircuitBreaker
	concurrencySem *semaphore.Weighted
}
```

**Removed:**
- `client *anthropic.Client` - Replaced by provider
- `model string` - Moved to provider

### Function Refactoring Example

**Before (Hardcoded CLI):**

```go
func (s *Supervisor) AssessIssueState(ctx context.Context, issue *types.Issue) (*Assessment, error) {
	startTime := time.Now()

	prompt := s.buildAssessmentPrompt(issue)

	model := getModelForCLI(s.model)
	responseText, inputTokens, outputTokens, err := s.invokeCLIWithRetry(ctx, "assessment", prompt, model)
	if err != nil {
		return nil, fmt.Errorf("claude CLI call failed: %w", err)
	}

	assessment, err := s.parseAssessmentResponse(responseText)
	// ...
}
```

**After (Provider Abstraction):**

```go
func (s *Supervisor) AssessIssueState(ctx context.Context, issue *types.Issue) (*Assessment, error) {
	startTime := time.Now()

	// Build prompt (unchanged)
	prompt := s.buildAssessmentPrompt(issue)

	// Call provider with retry
	var result *InvokeResult
	err := s.retryWithBackoff(ctx, "assessment", func(attemptCtx context.Context) error {
		res, providerErr := s.provider.Invoke(attemptCtx, InvokeParams{
			Operation: "assessment",
			Prompt:    prompt,
			MaxTokens: 4096,
			Model:     "", // Use provider default
		})
		if providerErr != nil {
			return providerErr
		}
		result = res
		return nil
	})

	if err != nil {
		return nil, fmt.Errorf("AI provider call failed: %w", err)
	}

	// Parse response (unchanged)
	assessment, err := s.parseAssessmentResponse(result.Text)
	if err != nil {
		return nil, fmt.Errorf("failed to parse assessment: %w", err)
	}

	// Log usage (unchanged)
	duration := time.Since(startTime)
	fmt.Printf("AI Assessment for %s: confidence=%.2f, effort=%s, duration=%v\n",
		issue.ID, assessment.Confidence, assessment.EstimatedEffort, duration)

	if err := s.logAIUsage(ctx, issue.ID, "assessment", int64(result.InputTokens), int64(result.OutputTokens), duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return assessment, nil
}
```

### Key Changes Per Function

All 11 functions follow this pattern:

1. **Remove:** Direct API/CLI calls
2. **Replace:** With `s.provider.Invoke()` wrapped in `retryWithBackoff`
3. **Keep:** Prompt building, response parsing, logging
4. **Update:** Log messages (remove "via Claude CLI" suffix, provider-agnostic now)

**Functions to update:**
- `assessment.go`: AssessIssueState, AssessCompletion
- `analysis.go`: AnalyzeExecution
- `recovery.go`: SuggestRecoveryStrategy
- `code_review.go`: AnalyzeCodeQuality, AnalyzeTestCoverage, AnalyzeCodeReviewNeed
- `planning.go`: GeneratePlan, RefinePhase, ValidatePhaseStructure
- `utils.go`: SummarizeAgentOutput, DraftClarifyingQuestions, TruncateWithAI (via CallAI)

## Testing Strategy

### Provider-Specific Tests

```go
// Test API provider
func TestAnthropicAPIProvider_Invoke(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	provider, err := NewAnthropicAPIProvider(apiKey, "claude-sonnet-4-5-20250929")
	require.NoError(t, err)

	result, err := provider.Invoke(context.Background(), InvokeParams{
		Operation: "test",
		Prompt:    "Say hello",
		MaxTokens: 100,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, result.Text)
	assert.Greater(t, result.InputTokens, 0)
	assert.Greater(t, result.OutputTokens, 0)
}

// Test CLI provider
func TestClaudeCLIProvider_Invoke(t *testing.T) {
	// Skip if CLI not available
	claudePath := os.Getenv("VC_CLAUDE_PATH")
	if claudePath == "" {
		home, _ := os.UserHomeDir()
		claudePath = filepath.Join(home, ".claude", "local", "claude")
	}
	if _, err := os.Stat(claudePath); err != nil {
		t.Skip("Claude CLI not available")
	}

	provider, err := NewClaudeCLIProvider("sonnet")
	require.NoError(t, err)

	result, err := provider.Invoke(context.Background(), InvokeParams{
		Operation: "test",
		Prompt:    "Say hello",
		MaxTokens: 100,
	})

	require.NoError(t, err)
	assert.NotEmpty(t, result.Text)
	assert.Greater(t, result.InputTokens, 0)
	assert.Greater(t, result.OutputTokens, 0)
}
```

### Integration Tests

```go
// Test supervisor with API provider
func TestSupervisor_WithAPIProvider(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set")
	}

	os.Setenv("VC_AI_PROVIDER", "api")
	defer os.Unsetenv("VC_AI_PROVIDER")

	cfg := &Config{
		APIKey: apiKey,
		Store:  store,
		Retry:  DefaultRetryConfig(),
	}

	supervisor, err := NewSupervisor(cfg)
	require.NoError(t, err)

	// Test assessment
	assessment, err := supervisor.AssessIssueState(ctx, testIssue)
	require.NoError(t, err)
	assert.NotNil(t, assessment)
}

// Test supervisor with CLI provider
func TestSupervisor_WithCLIProvider(t *testing.T) {
	// Skip if CLI not available
	home, _ := os.UserHomeDir()
	claudePath := filepath.Join(home, ".claude", "local", "claude")
	if _, err := os.Stat(claudePath); err != nil {
		t.Skip("Claude CLI not available")
	}

	os.Setenv("VC_AI_PROVIDER", "cli")
	defer os.Unsetenv("VC_AI_PROVIDER")

	cfg := &Config{
		Store: store,
		Retry: DefaultRetryConfig(),
	}

	supervisor, err := NewSupervisor(cfg)
	require.NoError(t, err)

	// Test assessment
	assessment, err := supervisor.AssessIssueState(ctx, testIssue)
	require.NoError(t, err)
	assert.NotNil(t, assessment)
}
```

### Restoring Skipped Tests

```go
// Previously skipped, now conditional
func TestAssessCompletion_ErrorHandling(t *testing.T) {
	providerType := os.Getenv("VC_AI_PROVIDER")
	if providerType == "cli" {
		t.Skip("CLI provider doesn't validate API keys - session auth instead")
	}

	// Create supervisor with invalid API key
	cfg := &Config{
		Store:  store,
		APIKey: "invalid-key",
		Retry:  DefaultRetryConfig(),
	}

	supervisor, err := NewSupervisor(cfg)
	require.NoError(t, err)

	// Should fail with authentication error
	_, err = supervisor.AssessCompletion(ctx, testIssue, nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "authentication")
}
```

## Migration Path

### For Existing Users (API - No Changes)

```bash
# No changes required - API is default
export ANTHROPIC_API_KEY=sk-ant-...
./vc daemon
```

### For Max Plan Users (CLI - Opt-In)

```bash
# Switch to CLI provider
export VC_AI_PROVIDER=cli
./vc daemon
```

### For CI/CD

```bash
# Use API in CI (pay-per-use, no CLI dependency)
export VC_AI_PROVIDER=api
export ANTHROPIC_API_KEY=${{ secrets.ANTHROPIC_API_KEY }}
./vc execute --once
```

### For Development/Testing

```bash
# Test with API
export VC_AI_PROVIDER=api
export ANTHROPIC_API_KEY=sk-ant-...
go test ./internal/ai/...

# Test with CLI
export VC_AI_PROVIDER=cli
go test ./internal/ai/...
```

## Cost Comparison

| Provider | Cost Structure | Best For | Monthly Estimate |
|----------|----------------|----------|------------------|
| **Anthropic API** | Pay-per-token (~$3/M input, ~$15/M output) | Casual use, CI/CD, teams | $2,000+ for heavy dogfooding |
| **Claude CLI (Max)** | $90/month flat (unlimited) | Heavy dogfooding, personal projects | $90 (fixed) |

**Breakeven:** ~45 AI calls per day OR ~1M tokens per month

## Implementation Checklist

- [ ] vc-11: Design pluggable AI provider abstraction (this document)
- [ ] vc-12: Implement AnthropicAPIProvider
  - [ ] Create `internal/ai/provider_anthropic.go`
  - [ ] Implement Invoke() method
  - [ ] Add unit tests
- [ ] vc-13: Implement ClaudeCLIProvider
  - [ ] Create `internal/ai/provider_claude_cli.go`
  - [ ] Move claude_cli.go code into provider
  - [ ] Add unit tests
- [ ] vc-14: Add provider selection configuration
  - [ ] Update Config struct
  - [ ] Add environment variable support
  - [ ] Update NewSupervisor()
  - [ ] Add validation and error messages
- [ ] vc-15: Update Supervisor to use provider abstraction
  - [ ] Update Supervisor struct (remove client, add provider)
  - [ ] Refactor all 11 AI functions
  - [ ] Update tests to pass provider type
  - [ ] Remove dead code (unused client creation)
- [ ] vc-16: Restore API-based tests
  - [ ] Un-skip error handling tests (API provider only)
  - [ ] Add provider selection to test suite
  - [ ] Add integration tests for both providers
- [ ] vc-17: Clean up temporary files
  - [ ] Remove manual_test_cli.go
  - [ ] Remove manual_test_cli_simple.go
  - [ ] Remove test_e2e_cli.sh
  - [ ] Remove test artifacts
  - [ ] Archive or remove planning docs
- [ ] vc-18: Add documentation
  - [ ] Update README with provider configuration
  - [ ] Add migration guide
  - [ ] Document when to use each provider
  - [ ] Add troubleshooting guide

## Success Criteria

- ✅ Both providers work with all 11 supervisor functions
- ✅ Default to API provider (backwards compatible)
- ✅ CLI provider opt-in via VC_AI_PROVIDER=cli
- ✅ All existing tests pass with API provider
- ✅ New tests verify CLI provider works
- ✅ No dead code (unused client, unused API key checks)
- ✅ Documentation explains when to use each provider
- ✅ Easy to add new providers in future (interface is clean)

## Future Extensions

This design enables future providers:

- **OpenAI Provider** - Use OpenAI API instead of Anthropic
- **Local Model Provider** - Use Ollama, LM Studio, etc.
- **Mock Provider** - For testing without API calls
- **Hybrid Provider** - Use different providers for different operations
- **Fallback Provider** - Try CLI, fall back to API on error

Example future provider:

```go
type OpenAIProvider struct {
	client *openai.Client
	model  string
}

func (p *OpenAIProvider) Invoke(ctx context.Context, params InvokeParams) (*InvokeResult, error) {
	// Call OpenAI API
	// ...
}
```

## Questions & Answers

**Q: Why not use per-function methods like AssessIssue(), GeneratePlan(), etc.?**
A: All functions follow the same pattern (prompt in, text out). Provider doesn't need business logic knowledge. Simpler interface = easier to implement new providers.

**Q: Should providers handle retry logic?**
A: No. Retry is a cross-cutting concern handled by supervisor's retryWithBackoff. Providers just do single attempts.

**Q: What if I want to use API for some functions and CLI for others?**
A: Not supported in v1. All functions use the same provider. Could add per-function override in future if needed (YAGNI for now).

**Q: How do I test without making real API calls?**
A: Create a MockProvider that implements the interface and returns canned responses. Add in vc-16.

**Q: What about prompt caching?**
A: Both providers support caching (API: automatic, CLI: session-based). No changes needed.

**Q: Can I switch providers at runtime?**
A: No. Provider is selected once at supervisor creation. Would require new supervisor instance to switch.

## Design Review Notes

- **Date:** 2025-10-24
- **Reviewer:** Travis (via Claude Code)
- **Status:** Approved for implementation
- **Next Steps:** Implement vc-12 (API provider) and vc-13 (CLI provider) in parallel
