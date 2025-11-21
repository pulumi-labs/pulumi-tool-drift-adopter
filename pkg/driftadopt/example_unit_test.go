//go:build unit

package driftadopt_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Example unit test - tests pure logic without external dependencies
// Run with: go test -tags=unit ./...
func TestExample_Unit(t *testing.T) {
	// Unit tests should be fast and test isolated logic
	result := 2 + 2
	assert.Equal(t, 4, result)
}
