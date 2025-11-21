package testutil

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// AssertFileExists checks that a file exists at the given path
func AssertFileExists(t *testing.T, path string) bool {
	t.Helper()

	// Using os.Stat through the standard testing approach
	// This will be implemented when we have the types package
	return assert.FileExists(t, path)
}

// AssertFileNotExists checks that a file does not exist at the given path
func AssertFileNotExists(t *testing.T, path string) bool {
	t.Helper()

	return assert.NoFileExists(t, path)
}

// AssertJSONEqual compares two JSON strings, ignoring whitespace differences
func AssertJSONEqual(t *testing.T, expected, actual string, msgAndArgs ...interface{}) bool {
	t.Helper()

	return assert.JSONEq(t, expected, actual, msgAndArgs...)
}

// AssertContainsAll checks that a string contains all the given substrings
func AssertContainsAll(t *testing.T, s string, substrings []string, msgAndArgs ...interface{}) bool {
	t.Helper()

	for _, substr := range substrings {
		if !assert.Contains(t, s, substr, msgAndArgs...) {
			return false
		}
	}
	return true
}

// AssertErrorContains checks that an error contains a specific substring
func AssertErrorContains(t *testing.T, err error, substring string, msgAndArgs ...interface{}) bool {
	t.Helper()

	if !assert.Error(t, err, msgAndArgs...) {
		return false
	}

	return assert.Contains(t, err.Error(), substring, msgAndArgs...)
}
