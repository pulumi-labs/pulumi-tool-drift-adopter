package testutil

import "fmt"

// MockLLMClient is a mock implementation of LLMClient for testing
type MockLLMClient struct {
	// Response is the pre-configured response to return
	Response string

	// Error is the error to return (if non-nil)
	Error error

	// CallCount tracks how many times Generate was called
	CallCount int

	// LastPrompt stores the last prompt passed to Generate
	LastPrompt string

	// Responses is a list of responses to return in order (overrides Response)
	Responses []string

	// ResponseIndex tracks which response to return next
	ResponseIndex int
}

// Generate implements the LLMClient interface
func (m *MockLLMClient) Generate(prompt string) (string, error) {
	m.CallCount++
	m.LastPrompt = prompt

	if m.Error != nil {
		return "", m.Error
	}

	// Use Responses list if provided
	if len(m.Responses) > 0 {
		if m.ResponseIndex >= len(m.Responses) {
			return "", fmt.Errorf("no more mock responses available (called %d times)", m.CallCount)
		}
		response := m.Responses[m.ResponseIndex]
		m.ResponseIndex++
		return response, nil
	}

	return m.Response, nil
}

// Reset resets the mock state
func (m *MockLLMClient) Reset() {
	m.CallCount = 0
	m.LastPrompt = ""
	m.ResponseIndex = 0
}
