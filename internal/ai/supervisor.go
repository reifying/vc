package ai

import (
	"fmt"
	"os"

	"github.com/steveyegge/vc/internal/storage"
	"github.com/steveyegge/vc/internal/types"
	"golang.org/x/sync/semaphore"
)

// Supervisor handles AI-powered assessment and analysis of issues
// It also implements the MissionPlanner interface for mission orchestration
//
// The Supervisor's responsibilities are distributed across multiple files:
// - supervisor.go: Core struct and constructor (this file)
// - provider.go: AI provider abstraction interface
// - provider_anthropic.go: Anthropic API provider implementation
// - provider_claude_cli.go: Claude CLI provider implementation
// - retry.go: Circuit breaker and retry logic
// - assessment.go: Pre-execution assessment and completion assessment
// - analysis.go: Post-execution analysis
// - recovery.go: Quality gate failure recovery strategies
// - code_review.go: Code quality and test coverage analysis
// - deduplication.go: Duplicate issue detection
// - translation.go: Discovered issue creation
// - planning.go: Mission planning and phase refinement
// - utils.go: Shared utilities (logging, summarization, truncation)
type Supervisor struct {
	provider       AIProvider          // AI provider (API or CLI)
	store          storage.Storage
	retry          RetryConfig
	circuitBreaker *CircuitBreaker
	concurrencySem *semaphore.Weighted // Limits concurrent AI API calls (vc-220)
}

// Compile-time check that Supervisor implements MissionPlanner
var _ types.MissionPlanner = (*Supervisor)(nil)

// Config holds supervisor configuration
type Config struct {
	Provider string          // AI provider type: "api" or "cli" (default: "api", or from VC_AI_PROVIDER env var)
	APIKey   string          // Anthropic API key (required for API provider, reads from ANTHROPIC_API_KEY env var if empty)
	Model    string          // Model to use (default: claude-sonnet-4-5-20250929 for API, sonnet for CLI)
	Store    storage.Storage // Storage backend (required)
	Retry    RetryConfig     // Retry configuration (uses defaults if not specified)
}

// NewSupervisor creates a new AI supervisor with the configured provider
func NewSupervisor(cfg *Config) (*Supervisor, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("storage is required")
	}

	// Determine provider type from config or environment
	providerType := cfg.Provider
	if providerType == "" {
		providerType = os.Getenv("VC_AI_PROVIDER")
	}
	if providerType == "" {
		providerType = "api" // Default to API for backwards compatibility
	}

	// Create the appropriate provider
	var provider AIProvider
	var err error

	switch providerType {
	case "api":
		// Get API key from config or environment
		apiKey := cfg.APIKey
		if apiKey == "" {
			apiKey = os.Getenv("ANTHROPIC_API_KEY")
		}
		if apiKey == "" {
			return nil, fmt.Errorf("ANTHROPIC_API_KEY is required for API provider (set via config or environment variable)")
		}

		// Get model, default to Sonnet 4.5 for API
		model := cfg.Model
		if model == "" {
			model = "claude-sonnet-4-5-20250929"
		}

		provider, err = NewAnthropicAPIProvider(apiKey, model)
		if err != nil {
			return nil, fmt.Errorf("failed to create Anthropic API provider: %w", err)
		}

		fmt.Printf("Using Anthropic API provider (model: %s)\n", model)

	case "cli":
		// Get model, default to sonnet for CLI
		model := cfg.Model
		if model == "" {
			model = "sonnet"
		}

		provider, err = NewClaudeCLIProvider(model)
		if err != nil {
			return nil, fmt.Errorf("failed to create Claude CLI provider: %w", err)
		}

		fmt.Printf("Using Claude CLI provider (model: %s)\n", model)

	default:
		return nil, fmt.Errorf("unknown provider type: %q (use 'api' or 'cli', set via VC_AI_PROVIDER env var or config)", providerType)
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
		fmt.Printf("Circuit breaker initialized: threshold=%d failures, recovery=%d successes, timeout=%v\n",
			retry.FailureThreshold, retry.SuccessThreshold, retry.OpenTimeout)
	}

	// Initialize concurrency limiter (vc-220)
	var concurrencySem *semaphore.Weighted
	if retry.MaxConcurrentCalls > 0 {
		concurrencySem = semaphore.NewWeighted(int64(retry.MaxConcurrentCalls))
		fmt.Printf("AI concurrency limiter initialized: max_concurrent=%d calls\n", retry.MaxConcurrentCalls)
	}

	return &Supervisor{
		provider:       provider,
		store:          cfg.Store,
		retry:          retry,
		circuitBreaker: circuitBreaker,
		concurrencySem: concurrencySem,
	}, nil
}
