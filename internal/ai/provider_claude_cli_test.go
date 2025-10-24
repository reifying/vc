package ai

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClaudeCLIProvider(t *testing.T) {
	// Skip if Claude CLI not available
	home, _ := os.UserHomeDir()
	claudePath := filepath.Join(home, ".claude", "local", "claude")
	if _, err := os.Stat(claudePath); err != nil {
		t.Skip("Claude CLI not available - skipping tests")
	}

	t.Run("uses default path if VC_CLAUDE_PATH not set", func(t *testing.T) {
		// Clear env var
		oldPath := os.Getenv("VC_CLAUDE_PATH")
		os.Unsetenv("VC_CLAUDE_PATH")
		defer func() {
			if oldPath != "" {
				os.Setenv("VC_CLAUDE_PATH", oldPath)
			}
		}()

		provider, err := NewClaudeCLIProvider("")
		require.NoError(t, err)
		assert.Equal(t, claudePath, provider.claudePath)
	})

	t.Run("uses VC_CLAUDE_PATH if set", func(t *testing.T) {
		// Set custom path (use the same path, just testing the env var works)
		os.Setenv("VC_CLAUDE_PATH", claudePath)
		defer os.Unsetenv("VC_CLAUDE_PATH")

		provider, err := NewClaudeCLIProvider("")
		require.NoError(t, err)
		assert.Equal(t, claudePath, provider.claudePath)
	})

	t.Run("uses default model if not specified", func(t *testing.T) {
		provider, err := NewClaudeCLIProvider("")
		require.NoError(t, err)
		assert.Equal(t, "sonnet", provider.model)
	})

	t.Run("maps full model names to CLI aliases", func(t *testing.T) {
		testCases := []struct {
			input    string
			expected string
		}{
			{"claude-sonnet-4-5-20250929", "sonnet"},
			{"claude-3-5-haiku-20241022", "haiku"},
			{"claude-opus-3", "opus"},
			{"", "sonnet"}, // default
		}

		for _, tc := range testCases {
			provider, err := NewClaudeCLIProvider(tc.input)
			require.NoError(t, err)
			assert.Equal(t, tc.expected, provider.model, "input: %s", tc.input)
		}
	})

	t.Run("fails if CLI path does not exist", func(t *testing.T) {
		os.Setenv("VC_CLAUDE_PATH", "/nonexistent/path/to/claude")
		defer os.Unsetenv("VC_CLAUDE_PATH")

		provider, err := NewClaudeCLIProvider("")
		assert.Error(t, err)
		assert.Nil(t, provider)
		assert.Contains(t, err.Error(), "claude CLI not found")
	})
}

func TestClaudeCLIProvider_Invoke(t *testing.T) {
	// Skip if Claude CLI not available
	home, _ := os.UserHomeDir()
	claudePath := filepath.Join(home, ".claude", "local", "claude")
	if _, err := os.Stat(claudePath); err != nil {
		t.Skip("Claude CLI not available - skipping integration tests")
	}

	t.Run("successful invocation", func(t *testing.T) {
		provider, err := NewClaudeCLIProvider("sonnet")
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
		provider, err := NewClaudeCLIProvider("sonnet")
		require.NoError(t, err)

		// Override with haiku (faster, cheaper)
		result, err := provider.Invoke(context.Background(), InvokeParams{
			Operation: "test",
			Prompt:    "Say 'test' and nothing else.",
			MaxTokens: 50,
			Model:     "haiku",
		})

		require.NoError(t, err)
		assert.NotEmpty(t, result.Text)
		assert.Greater(t, result.InputTokens, 0)
		assert.Greater(t, result.OutputTokens, 0)
	})

	t.Run("handles context cancellation", func(t *testing.T) {
		provider, err := NewClaudeCLIProvider("sonnet")
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
		// Note: CLI might not always detect cancellation immediately
		// So we just check for error, not specific message
	})
}

func TestGetModelForCLI(t *testing.T) {
	testCases := []struct {
		input    string
		expected string
	}{
		{"claude-sonnet-4-5-20250929", "sonnet"},
		{"claude-3-5-sonnet-20241022", "sonnet"},
		{"sonnet", "sonnet"},
		{"claude-3-5-haiku-20241022", "haiku"},
		{"haiku-latest", "haiku"},
		{"claude-opus-3", "opus"},
		{"opus", "opus"},
		{"unknown-model", "sonnet"}, // default
		{"", "sonnet"},               // default
	}

	for _, tc := range testCases {
		result := getModelForCLI(tc.input)
		assert.Equal(t, tc.expected, result, "input: %s", tc.input)
	}
}
