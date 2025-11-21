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

func TestGoValidator_ValidCode(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module example.com/test

go 1.21
`
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)

	// Create valid Go code
	validCode := `package main

import "fmt"

func main() {
	fmt.Println("Hello, World!")
}
`
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(validCode), 0644)

	goValidator := validator.NewGoValidator()
	ctx := context.Background()

	// Act
	result, err := goValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.True(t, result.Success, "Expected validation to succeed")
	assert.Empty(t, result.Errors)
}

func TestGoValidator_CompileError(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module example.com/test

go 1.21
`
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)

	// Create invalid Go code (type error)
	invalidCode := `package main

func main() {
	var x int = "not an int"  // Type error
	println(x)
}
`
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(invalidCode), 0644)

	goValidator := validator.NewGoValidator()
	ctx := context.Background()

	// Act
	result, err := goValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.False(t, result.Success, "Expected validation to fail")
	assert.NotEmpty(t, result.Errors)

	// Check error
	firstError := result.Errors[0]
	assert.Contains(t, firstError.File, "main.go")
	assert.Equal(t, 4, firstError.Line)
}

func TestGoValidator_UndefinedVariable(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create go.mod
	goMod := `module example.com/test

go 1.21
`
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)

	// Create code with undefined variable
	invalidCode := `package main

func main() {
	println(undefinedVar)  // Error: undefined
}
`
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(invalidCode), 0644)

	goValidator := validator.NewGoValidator()
	ctx := context.Background()

	// Act
	result, err := goValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
}

func TestGoValidator_NoGoMod(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// No go.mod file
	validCode := `package main

func main() {}
`
	os.WriteFile(filepath.Join(tmpDir, "main.go"), []byte(validCode), 0644)

	goValidator := validator.NewGoValidator()
	ctx := context.Background()

	// Act
	_, err := goValidator.Validate(ctx, tmpDir)

	// Assert
	assert.Error(t, err, "Expected error when go.mod not found")
	assert.Contains(t, err.Error(), "go.mod")
}

func TestGoValidator_NoGoFiles(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create go.mod but no Go files
	goMod := `module example.com/test

go 1.21
`
	os.WriteFile(filepath.Join(tmpDir, "go.mod"), []byte(goMod), 0644)

	goValidator := validator.NewGoValidator()
	ctx := context.Background()

	// Act
	result, err := goValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.True(t, result.Success, "Expected success with no Go files")
}
