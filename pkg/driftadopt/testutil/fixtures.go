package testutil

import (
	"os"
	"path/filepath"
	"testing"
)

// LoadFixture loads a test fixture file from the testdata directory
func LoadFixture(t *testing.T, path string) []byte {
	t.Helper()

	// Try relative to test file first
	data, err := os.ReadFile(path)
	if err == nil {
		return data
	}

	// Try relative to project root
	rootPath := filepath.Join("..", "..", "..", path)
	data, err = os.ReadFile(rootPath)
	if err != nil {
		t.Fatalf("failed to load fixture %s: %v", path, err)
	}

	return data
}

// CreateTempDir creates a temporary directory for testing
func CreateTempDir(t *testing.T) string {
	t.Helper()

	dir, err := os.MkdirTemp("", "drift-adopt-test-*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}

	t.Cleanup(func() {
		os.RemoveAll(dir)
	})

	return dir
}

// WriteFile writes content to a file, creating parent directories if needed
func WriteFile(t *testing.T, path string, content []byte) {
	t.Helper()

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatalf("failed to create directory %s: %v", dir, err)
	}

	if err := os.WriteFile(path, content, 0644); err != nil {
		t.Fatalf("failed to write file %s: %v", path, err)
	}
}
