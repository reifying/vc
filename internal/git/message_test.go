package git

import (
	"context"
	"fmt"
	"testing"

	"github.com/steveyegge/vc/internal/ai"
)

// mockAIProvider is a test double for AIProvider
type mockAIProvider struct {
	responseText string
	inputTokens  int
	outputTokens int
	err          error
}

func (m *mockAIProvider) Invoke(ctx context.Context, params ai.InvokeParams) (*ai.InvokeResult, error) {
	if m.err != nil {
		return nil, m.err
	}
	return &ai.InvokeResult{
		Text:         m.responseText,
		InputTokens:  m.inputTokens,
		OutputTokens: m.outputTokens,
	}, nil
}

// retryMockProvider is a test double that allows custom retry logic
type retryMockProvider struct {
	attemptFunc func() (*ai.InvokeResult, error)
}

func (m *retryMockProvider) Invoke(ctx context.Context, params ai.InvokeParams) (*ai.InvokeResult, error) {
	return m.attemptFunc()
}

// capturePromptProvider is a test double that captures the prompt for inspection
type capturePromptProvider struct {
	promptCapture *string
	responseText  string
}

func (m *capturePromptProvider) Invoke(ctx context.Context, params ai.InvokeParams) (*ai.InvokeResult, error) {
	*m.promptCapture = params.Prompt
	return &ai.InvokeResult{
		Text:         m.responseText,
		InputTokens:  100,
		OutputTokens: 50,
	}, nil
}

func TestMessageGenerator_GenerateCommitMessage(t *testing.T) {
	ctx := context.Background()

	t.Run("SuccessfulGeneration", func(t *testing.T) {
		// Mock AI provider that returns valid JSON
		mockProvider := &mockAIProvider{
			responseText: `{
				"subject": "feat(git): implement auto-commit (vc-119)",
				"body": "Add automatic commit generation using AI.\n\nThis enables the executor to automatically commit successful agent work.",
				"reasoning": "The changes implement auto-commit functionality as described in the issue"
			}`,
			inputTokens:  100,
			outputTokens: 50,
		}

		generator := NewMessageGenerator(mockProvider)

		req := CommitMessageRequest{
			IssueID:          "vc-119",
			IssueTitle:       "Implement auto-commit",
			IssueDescription: "Add automatic commit generation",
			ChangedFiles:     []string{"internal/git/message.go", "internal/executor/executor.go"},
			Diff:             "diff --git a/internal/git/message.go ...",
		}

		resp, err := generator.GenerateCommitMessage(ctx, req)
		if err != nil {
			t.Fatalf("GenerateCommitMessage failed: %v", err)
		}

		if resp.Subject != "feat(git): implement auto-commit (vc-119)" {
			t.Errorf("Expected subject 'feat(git): implement auto-commit (vc-119)', got %q", resp.Subject)
		}

		if resp.Body == "" {
			t.Error("Expected non-empty body")
		}

		if resp.Reasoning == "" {
			t.Error("Expected non-empty reasoning")
		}
	})

	t.Run("ProviderError", func(t *testing.T) {
		// Mock AI provider that returns an error
		mockProvider := &mockAIProvider{
			err: fmt.Errorf("API rate limit exceeded"),
		}

		generator := NewMessageGenerator(mockProvider)

		req := CommitMessageRequest{
			IssueID:    "vc-119",
			IssueTitle: "Test issue",
		}

		_, err := generator.GenerateCommitMessage(ctx, req)
		if err == nil {
			t.Fatal("Expected error from provider failure")
		}

		if !contains(err.Error(), "failed to generate commit message") {
			t.Errorf("Expected error message to mention 'failed to generate commit message', got: %v", err)
		}
	})

	t.Run("InvalidJSONResponse", func(t *testing.T) {
		// Mock AI provider that returns invalid JSON
		mockProvider := &mockAIProvider{
			responseText: `This is not valid JSON`,
			inputTokens:  100,
			outputTokens: 50,
		}

		generator := NewMessageGenerator(mockProvider)

		req := CommitMessageRequest{
			IssueID:    "vc-119",
			IssueTitle: "Test issue",
		}

		_, err := generator.GenerateCommitMessage(ctx, req)
		if err == nil {
			t.Fatal("Expected error from invalid JSON")
		}

		if !contains(err.Error(), "failed to parse commit message response") {
			t.Errorf("Expected error message to mention parsing failure, got: %v", err)
		}
	})

	t.Run("RetryOnTransientFailure", func(t *testing.T) {
		// Mock AI provider that fails twice then succeeds
		callCount := 0
		mockProvider := &retryMockProvider{
			attemptFunc: func() (*ai.InvokeResult, error) {
				callCount++
				if callCount < 3 {
					return nil, fmt.Errorf("transient error %d", callCount)
				}
				return &ai.InvokeResult{
					Text: `{
						"subject": "test: verify retry logic",
						"body": "This should succeed on third attempt",
						"reasoning": "Testing retry behavior"
					}`,
					InputTokens:  100,
					OutputTokens: 50,
				}, nil
			},
		}

		generator := NewMessageGenerator(mockProvider)

		req := CommitMessageRequest{
			IssueID:    "vc-test",
			IssueTitle: "Test retry",
		}

		resp, err := generator.GenerateCommitMessage(ctx, req)
		if err != nil {
			t.Fatalf("Expected success after retries, got error: %v", err)
		}

		if callCount != 3 {
			t.Errorf("Expected 3 attempts (2 failures + 1 success), got %d", callCount)
		}

		if resp.Subject != "test: verify retry logic" {
			t.Errorf("Expected subject 'test: verify retry logic', got %q", resp.Subject)
		}
	})

	t.Run("PromptIncludesIssueContext", func(t *testing.T) {
		// Verify that the prompt includes issue details
		var capturedPrompt string
		mockProvider := &capturePromptProvider{
			promptCapture: &capturedPrompt,
			responseText: `{
				"subject": "test: check prompt",
				"body": "Testing prompt content",
				"reasoning": "Verifying prompt includes context"
			}`,
		}

		generator := NewMessageGenerator(mockProvider)

		req := CommitMessageRequest{
			IssueID:          "vc-123",
			IssueTitle:       "Fix authentication bug",
			IssueDescription: "Users cannot log in after password reset",
			ChangedFiles:     []string{"auth.go", "auth_test.go"},
			Diff:             "diff --git a/auth.go ...",
		}

		_, err := generator.GenerateCommitMessage(ctx, req)
		if err != nil {
			t.Fatalf("GenerateCommitMessage failed: %v", err)
		}

		// Verify prompt contains issue context
		if !contains(capturedPrompt, "vc-123") {
			t.Error("Expected prompt to include issue ID")
		}
		if !contains(capturedPrompt, "Fix authentication bug") {
			t.Error("Expected prompt to include issue title")
		}
		if !contains(capturedPrompt, "Users cannot log in") {
			t.Error("Expected prompt to include issue description")
		}
		if !contains(capturedPrompt, "auth.go") {
			t.Error("Expected prompt to include changed files")
		}
	})
}

// contains checks if a string contains a substring
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		(len(s) > 0 && len(substr) > 0 && containsHelper(s, substr)))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestBuildPrompt(t *testing.T) {
	mockProvider := &mockAIProvider{}
	generator := NewMessageGenerator(mockProvider)

	req := CommitMessageRequest{
		IssueID:          "vc-100",
		IssueTitle:       "Add feature X",
		IssueDescription: "Implement new feature X for better UX",
		ChangedFiles:     []string{"feature.go", "feature_test.go"},
		Diff:             "diff --git a/feature.go b/feature.go\n+new code",
	}

	prompt := generator.buildPrompt(req)

	// Verify prompt structure
	tests := []struct {
		name     string
		expected string
	}{
		{"ContainsIssueID", "vc-100"},
		{"ContainsTitle", "Add feature X"},
		{"ContainsDescription", "Implement new feature X"},
		{"ContainsChangedFiles", "feature.go"},
		{"ContainsDiff", "diff --git"},
		{"ContainsInstructions", "conventional commits"},
		{"ContainsJSONFormat", "```json"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !contains(prompt, tt.expected) {
				t.Errorf("Expected prompt to contain %q", tt.expected)
			}
		})
	}
}

func TestDiffTruncation(t *testing.T) {
	mockProvider := &mockAIProvider{
		responseText: `{
			"subject": "test: large diff",
			"body": "Testing diff truncation",
			"reasoning": "Verifying large diffs are truncated"
		}`,
	}

	generator := NewMessageGenerator(mockProvider)

	// Create a diff larger than 10000 characters
	largeDiff := ""
	for i := 0; i < 15000; i++ {
		largeDiff += "x"
	}

	req := CommitMessageRequest{
		IssueID:    "vc-test",
		IssueTitle: "Large diff test",
		Diff:       largeDiff,
	}

	prompt := generator.buildPrompt(req)

	// Verify the prompt doesn't contain the full diff
	if contains(prompt, largeDiff) {
		t.Error("Expected large diff to be truncated")
	}

	// Verify truncation message is present
	if !contains(prompt, "truncated") {
		t.Error("Expected truncation message in prompt")
	}
}
