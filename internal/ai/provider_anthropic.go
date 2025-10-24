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
