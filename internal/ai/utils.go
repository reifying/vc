package ai

import (
	"context"
	"fmt"
	"os"
	"time"
	"unicode/utf8"

	"github.com/steveyegge/vc/internal/types"
)

// logAIUsage logs AI API usage metrics to the issue's event stream
func (s *Supervisor) logAIUsage(ctx context.Context, issueID, activity string, inputTokens, outputTokens int64, duration time.Duration) error {
	// Check if issue exists before trying to add comment
	// This prevents FOREIGN KEY constraint failures in tests where issues aren't in the database
	issue, err := s.store.GetIssue(ctx, issueID)
	if err != nil || issue == nil {
		// Issue doesn't exist - silently skip logging
		// This is common in tests where we pass test issues directly to AI functions
		// Note: GetIssue returns (nil, nil) when issue not found, so check both err and issue
		return nil
	}

	comment := fmt.Sprintf("AI Usage (%s): input=%d tokens, output=%d tokens, duration=%v",
		activity, inputTokens, outputTokens, duration)
	return s.store.AddComment(ctx, issueID, "ai-supervisor", comment)
}

// CallAI makes a generic AI API call with the given prompt
// This provides a generic interface for other components (like watchdog) to use AI
// without duplicating retry logic and circuit breaker code
func (s *Supervisor) CallAI(ctx context.Context, prompt string, operation string, model string, maxTokens int) (string, error) {
	startTime := time.Now()

	// Use default maxTokens if not specified
	if maxTokens == 0 {
		maxTokens = 4096
	}

	// Call AI provider with retry logic
	var result *InvokeResult
	err := s.retryWithBackoff(ctx, operation, func(attemptCtx context.Context) error {
		res, providerErr := s.provider.Invoke(attemptCtx, InvokeParams{
			Operation: operation,
			Prompt:    prompt,
			MaxTokens: maxTokens,
			Model:     model, // Optional model override
		})
		result = res
		return providerErr
	})
	if err != nil {
		return "", fmt.Errorf("AI call failed: %w", err)
	}

	// Log the call
	duration := time.Since(startTime)
	fmt.Printf("AI %s call: input=%d tokens, output=%d tokens, duration=%v\n",
		operation, result.InputTokens, result.OutputTokens, duration)

	return result.Text, nil
}

// SummarizeAgentOutput uses AI to create an intelligent summary of agent output
// instead of using a simple "last N lines" heuristic.
//
// This method:
// - Sends the full output to AI with context about the issue
// - AI extracts: what was done, key decisions, important warnings
// - Returns a concise summary suitable for comments/notifications
// - Handles various output formats (test results, build logs, etc.)
func (s *Supervisor) SummarizeAgentOutput(ctx context.Context, issue *types.Issue, fullOutput string, maxLength int) (string, error) {
	startTime := time.Now()

	// Handle empty output
	if len(fullOutput) == 0 {
		return "Agent completed with no output", nil
	}

	// For very short output, just return it directly
	if len(fullOutput) <= maxLength {
		return fullOutput, nil
	}

	// Build the summarization prompt
	prompt := s.buildSummarizationPrompt(issue, fullOutput, maxLength)

	// Call AI provider with retry logic
	var result *InvokeResult
	err := s.retryWithBackoff(ctx, "summarization", func(attemptCtx context.Context) error {
		res, providerErr := s.provider.Invoke(attemptCtx, InvokeParams{
			Operation: "summarization",
			Prompt:    prompt,
			MaxTokens: 4096,
		})
		result = res
		return providerErr
	})
	if err != nil {
		// Don't fall back to heuristics - return the error (ZFC compliance)
		return "", fmt.Errorf("AI summarization failed: %w", err)
	}
	summaryText := result.Text
	inputTokens := result.InputTokens
	outputTokens := result.OutputTokens

	// Log the summarization
	duration := time.Since(startTime)
	fmt.Printf("AI Summarization: input=%d chars, output=%d chars, duration=%v\n",
		len(fullOutput), len(summaryText), duration)

	// Log AI usage to events
	if err := s.logAIUsage(ctx, issue.ID, "summarization", int64(inputTokens), int64(outputTokens), duration); err != nil {
		fmt.Fprintf(os.Stderr, "warning: failed to log AI usage: %v\n", err)
	}

	return summaryText, nil
}

// buildSummarizationPrompt builds the prompt for summarizing agent output
func (s *Supervisor) buildSummarizationPrompt(issue *types.Issue, fullOutput string, maxLength int) string {
	// Intelligently sample the output if it's very large
	// Send beginning + end for context, with indication of truncation
	outputToAnalyze := fullOutput
	wasTruncated := false

	// If output is enormous (>50k chars), sample it intelligently
	const maxPromptOutput = 50000
	if len(fullOutput) > maxPromptOutput {
		// Take first 20k and last 30k for context
		outputToAnalyze = fullOutput[:20000] + "\n\n... [truncated middle section] ...\n\n" + fullOutput[len(fullOutput)-30000:]
		wasTruncated = true
	}

	truncationNote := ""
	if wasTruncated {
		truncationNote = "\n\nNote: The full output was very large and has been sampled. Focus on extracting the most important information from what's provided."
	}

	return fmt.Sprintf(`You are summarizing the output from a coding agent that just worked on an issue. Extract the key information into a concise summary.

Issue Context:
Issue ID: %s
Title: %s
Description: %s

Agent Output (may be truncated):
%s%s

Please provide a concise summary (max %d characters) that captures:
1. What was actually done/accomplished
2. Key decisions or changes made
3. Important warnings, errors, or issues encountered
4. Test results (if any)
5. Next steps mentioned (if any)

Format the summary as plain text, suitable for adding as a comment. Be specific about concrete actions taken, not just "the agent worked on X". Include actual file names, test names, command outputs, etc.

Focus on information that would be useful to someone reviewing this work later. Skip boilerplate or irrelevant output.`,
		issue.ID, issue.Title, issue.Description,
		outputToAnalyze,
		truncationNote,
		maxLength)
}

// join concatenates a slice of strings with a separator
func join(strs []string, sep string) string {
	if len(strs) == 0 {
		return ""
	}
	result := strs[0]
	for i := 1; i < len(strs); i++ {
		result += sep + strs[i]
	}
	return result
}

// truncateString truncates a string to maxLen characters
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[len(s)-maxLen:]
}

// safeTruncateString truncates a string to maxLen bytes while preserving UTF-8 encoding
// If truncation would split a multi-byte UTF-8 sequence, it backs off to a valid boundary
func safeTruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}

	// Truncate at maxLen initially
	truncated := s[:maxLen]

	// Walk backwards to find a valid UTF-8 boundary
	// We only need to check up to 4 bytes back (max UTF-8 sequence length)
	for i := 0; i < 4 && len(truncated) > 0; i++ {
		// Check if we have valid UTF-8
		if utf8.ValidString(truncated) {
			return truncated
		}
		// Remove last byte and try again
		truncated = truncated[:len(truncated)-1]
	}

	// If we still don't have valid UTF-8 after 4 bytes, something is very wrong
	// Return empty string rather than corrupted data
	return ""
}
