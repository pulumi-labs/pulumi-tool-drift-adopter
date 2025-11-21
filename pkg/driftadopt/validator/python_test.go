//go:build unit

package validator_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt/validator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPythonValidator_ValidCode(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create valid Python code
	validCode := `import pulumi
from pulumi_aws import s3

bucket = s3.Bucket("my-bucket",
    tags={
        "Environment": "dev",
    }
)

pulumi.export("bucket_name", bucket.id)
`
	os.WriteFile(filepath.Join(tmpDir, "__main__.py"), []byte(validCode), 0644)

	pythonValidator := validator.NewPythonValidator()
	ctx := context.Background()

	// Act
	result, err := pythonValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.True(t, result.Success, "Expected validation to succeed")
	assert.Empty(t, result.Errors)
}

func TestPythonValidator_SyntaxError(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create invalid Python code (syntax error)
	invalidCode := `import pulumi

def my_function(
    # Missing closing parenthesis - syntax error
    pass

bucket = s3.Bucket("my-bucket")
`
	os.WriteFile(filepath.Join(tmpDir, "__main__.py"), []byte(invalidCode), 0644)

	pythonValidator := validator.NewPythonValidator()
	ctx := context.Background()

	// Act
	result, err := pythonValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.False(t, result.Success, "Expected validation to fail")
	assert.NotEmpty(t, result.Errors)

	// Check error contains file reference
	firstError := result.Errors[0]
	assert.Contains(t, firstError.File, "__main__.py")
	assert.Greater(t, firstError.Line, 0)
}

func TestPythonValidator_IndentationError(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create code with indentation error
	invalidCode := `import pulumi

def my_function():
pass  # Indentation error - should be indented
`
	os.WriteFile(filepath.Join(tmpDir, "index.py"), []byte(invalidCode), 0644)

	pythonValidator := validator.NewPythonValidator()
	ctx := context.Background()

	// Act
	result, err := pythonValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
}

func TestPythonValidator_NoPythonFiles(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// No Python files
	pythonValidator := validator.NewPythonValidator()
	ctx := context.Background()

	// Act
	result, err := pythonValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.True(t, result.Success, "Expected success with no Python files")
}

func TestPythonValidator_MultipleFiles(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create multiple Python files
	validCode := `print("hello")`
	os.WriteFile(filepath.Join(tmpDir, "file1.py"), []byte(validCode), 0644)
	os.WriteFile(filepath.Join(tmpDir, "file2.py"), []byte(validCode), 0644)

	pythonValidator := validator.NewPythonValidator()
	ctx := context.Background()

	// Act
	result, err := pythonValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.True(t, result.Success)
}
