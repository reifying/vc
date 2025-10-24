package ai

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewAnthropicAPIProvider(t *testing.T) {
	t.Run("requires API key", func(t *testing.T) {
		provider, err := NewAnthropicAPIProvider("", "claude-sonnet-4-5-20250929")
		assert.Error(t, err)
		assert.Nil(t, provider)
		assert.Contains(t, err.Error(), "API key is required")
	})

	t.Run("uses default model if not specified", func(t *testing.T) {
		provider, err := NewAnthropicAPIProvider("test-key", "")
		require.NoError(t, err)
		assert.Equal(t, "claude-sonnet-4-5-20250929", provider.model)
	})

	t.Run("uses specified model", func(t *testing.T) {
		provider, err := NewAnthropicAPIProvider("test-key", "claude-3-5-haiku-20241022")
		require.NoError(t, err)
		assert.Equal(t, "claude-3-5-haiku-20241022", provider.model)
	})
}

func TestAnthropicAPIProvider_Invoke(t *testing.T) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		t.Skip("ANTHROPIC_API_KEY not set - skipping integration test")
	}

	t.Run("successful invocation", func(t *testing.T) {
		provider, err := NewAnthropicAPIProvider(apiKey, "claude-sonnet-4-5-20250929")
		require.NoError(t, err)

		result, err := provider.Invoke(context.Background(), InvokeParams{
			Operation: "test",
			Prompt:    "Say 'Hello, World!' and nothing else.",
			MaxTokens: 100,
			Model:     "", // Use default
		})

		require.NoError(t, err)
		assert.NotEmpty(t, result.Text)
		assert.Contains(t, result.Text, "Hello")
		assert.Greater(t, result.InputTokens, 0, "Should have input tokens")
		assert.Greater(t, result.OutputTokens, 0, "Should have output tokens")

		t.Logf("Response: %s", result.Text)
		t.Logf("Tokens: %d in, %d out", result.InputTokens, result.OutputTokens)
	})

	t.Run("model override", func(t *testing.T) {
		provider, err := NewAnthropicAPIProvider(apiKey, "claude-sonnet-4-5-20250929")
		require.NoError(t, err)

		// Override with Haiku (faster, cheaper)
		result, err := provider.Invoke(context.Background(), InvokeParams{
			Operation: "test",
			Prompt:    "Say 'test' and nothing else.",
			MaxTokens: 50,
			Model:     "claude-3-5-haiku-20241022",
		})

		require.NoError(t, err)
		assert.NotEmpty(t, result.Text)
		assert.Greater(t, result.InputTokens, 0)
		assert.Greater(t, result.OutputTokens, 0)
	})

	t.Run("respects maxTokens", func(t *testing.T) {
		provider, err := NewAnthropicAPIProvider(apiKey, "claude-sonnet-4-5-20250929")
		require.NoError(t, err)

		result, err := provider.Invoke(context.Background(), InvokeParams{
			Operation: "test",
			Prompt:    "Count from 1 to 100.",
			MaxTokens: 10, // Very limited
			Model:     "",
		})

		require.NoError(t, err)
		assert.NotEmpty(t, result.Text)
		// Should have stopped early due to token limit
		assert.LessOrEqual(t, result.OutputTokens, 15, "Should respect maxTokens limit")
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		provider, err := NewAnthropicAPIProvider(apiKey, "claude-sonnet-4-5-20250929")
		require.NoError(t, err)

		// Create a cancelled context
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, err = provider.Invoke(ctx, InvokeParams{
			Operation: "test",
			Prompt:    "This should be cancelled.",
			MaxTokens: 100,
		})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	})
}

func TestAnthropicAPIProvider_Invoke_ErrorHandling(t *testing.T) {
	t.Run("invalid API key", func(t *testing.T) {
		provider, err := NewAnthropicAPIProvider("invalid-key-should-fail", "claude-sonnet-4-5-20250929")
		require.NoError(t, err)

		_, err = provider.Invoke(context.Background(), InvokeParams{
			Operation: "test",
			Prompt:    "This should fail.",
			MaxTokens: 100,
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "anthropic API call failed")
	})

	t.Run("invalid model name", func(t *testing.T) {
		apiKey := os.Getenv("ANTHROPIC_API_KEY")
		if apiKey == "" {
			t.Skip("ANTHROPIC_API_KEY not set")
		}

		provider, err := NewAnthropicAPIProvider(apiKey, "invalid-model-name")
		require.NoError(t, err)

		_, err = provider.Invoke(context.Background(), InvokeParams{
			Operation: "test",
			Prompt:    "This should fail.",
			MaxTokens: 100,
		})

		require.Error(t, err)
		assert.Contains(t, err.Error(), "anthropic API call failed")
	})
}
