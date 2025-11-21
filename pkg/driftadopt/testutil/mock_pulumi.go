package testutil

import "context"

// MockPulumiClient is a mock implementation for Pulumi operations
type MockPulumiClient struct {
	// PreviewOutput is the pre-configured preview output
	PreviewOutput string

	// PreviewError is the error to return from preview
	PreviewError error

	// RefreshOutput is the pre-configured refresh output
	RefreshOutput string

	// RefreshError is the error to return from refresh
	RefreshError error

	// StateJSON is the mocked state file content
	StateJSON []byte

	// PreviewCallCount tracks how many times Preview was called
	PreviewCallCount int

	// RefreshCallCount tracks how many times Refresh was called
	RefreshCallCount int
}

// Preview simulates running pulumi preview
func (m *MockPulumiClient) Preview(ctx context.Context, stack string) (string, error) {
	m.PreviewCallCount++

	if m.PreviewError != nil {
		return "", m.PreviewError
	}

	return m.PreviewOutput, nil
}

// Refresh simulates running pulumi refresh
func (m *MockPulumiClient) Refresh(ctx context.Context, stack string) (string, error) {
	m.RefreshCallCount++

	if m.RefreshError != nil {
		return "", m.RefreshError
	}

	return m.RefreshOutput, nil
}

// GetState returns the mocked state
func (m *MockPulumiClient) GetState() []byte {
	return m.StateJSON
}

// Reset resets the mock state
func (m *MockPulumiClient) Reset() {
	m.PreviewCallCount = 0
	m.RefreshCallCount = 0
}
