package ai

import (
	"context"
	"fmt"
)

// MockAIProvider is a test provider that returns predefined responses
type MockAIProvider struct {
	// ResponseText is the text to return from Invoke
	ResponseText string
	// InputTokens is the input token count to return
	InputTokens int
	// OutputTokens is the output token count to return
	OutputTokens int
	// Error is the error to return (if not nil, Invoke will fail)
	Error error
	// CallCount tracks how many times Invoke was called
	CallCount int
	// LastParams stores the last InvokeParams passed to Invoke
	LastParams *InvokeParams
}

// NewMockAIProvider creates a new mock provider with default success response
func NewMockAIProvider() *MockAIProvider {
	return &MockAIProvider{
		ResponseText: `{"success": true}`,
		InputTokens:  100,
		OutputTokens: 50,
	}
}

// Invoke implements the AIProvider interface for testing
func (m *MockAIProvider) Invoke(ctx context.Context, params InvokeParams) (*InvokeResult, error) {
	m.CallCount++
	m.LastParams = &params

	if m.Error != nil {
		return nil, m.Error
	}

	return &InvokeResult{
		Text:         m.ResponseText,
		InputTokens:  m.InputTokens,
		OutputTokens: m.OutputTokens,
	}, nil
}

// Reset resets the mock provider state
func (m *MockAIProvider) Reset() {
	m.CallCount = 0
	m.LastParams = nil
}

// WithError sets the error to return and returns the provider for chaining
func (m *MockAIProvider) WithError(err error) *MockAIProvider {
	m.Error = err
	return m
}

// WithResponse sets the response text and returns the provider for chaining
func (m *MockAIProvider) WithResponse(text string) *MockAIProvider {
	m.ResponseText = text
	return m
}

// NewErrorProvider creates a provider that always returns the given error
func NewErrorProvider(err error) *MockAIProvider {
	return &MockAIProvider{
		Error: err,
	}
}

// NewRetriableErrorProvider creates a provider that returns a retriable error
func NewRetriableErrorProvider() *MockAIProvider {
	return NewErrorProvider(fmt.Errorf("503 service unavailable"))
}
