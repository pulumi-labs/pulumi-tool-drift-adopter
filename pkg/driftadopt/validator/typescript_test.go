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

func TestTypeScriptValidator_ValidCode(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create package.json
	packageJSON := `{
  "name": "test-project",
  "version": "1.0.0",
  "devDependencies": {
    "typescript": "^5.0.0"
  }
}`
	os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644)

	// Create tsconfig.json
	tsconfigJSON := `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "commonjs",
    "strict": true
  }
}`
	os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfigJSON), 0644)

	// Create valid TypeScript code
	validCode := `export const greeting: string = "Hello, World!";
console.log(greeting);`
	os.WriteFile(filepath.Join(tmpDir, "index.ts"), []byte(validCode), 0644)

	tsValidator := validator.NewTypeScriptValidator()
	ctx := context.Background()

	// Act
	result, err := tsValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.True(t, result.Success, "Expected validation to succeed")
	assert.Empty(t, result.Errors)
}

func TestTypeScriptValidator_InvalidCode(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create package.json
	packageJSON := `{
  "name": "test-project",
  "version": "1.0.0",
  "devDependencies": {
    "typescript": "^5.0.0"
  }
}`
	os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644)

	// Create tsconfig.json
	tsconfigJSON := `{
  "compilerOptions": {
    "target": "ES2020",
    "module": "commonjs",
    "strict": true
  }
}`
	os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfigJSON), 0644)

	// Create invalid TypeScript code (type error)
	invalidCode := `const x: number = "not a number";  // Type error
const y = x + 1;`
	os.WriteFile(filepath.Join(tmpDir, "index.ts"), []byte(invalidCode), 0644)

	tsValidator := validator.NewTypeScriptValidator()
	ctx := context.Background()

	// Act
	result, err := tsValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.False(t, result.Success, "Expected validation to fail")
	assert.NotEmpty(t, result.Errors, "Expected compilation errors")

	// Check first error
	firstError := result.Errors[0]
	assert.Contains(t, firstError.File, "index.ts")
	assert.Equal(t, 1, firstError.Line)
	assert.Contains(t, firstError.Message, "Type")
}

func TestTypeScriptValidator_MultipleErrors(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create package.json
	packageJSON := `{"name": "test", "devDependencies": {"typescript": "^5.0.0"}}`
	os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644)

	// Create tsconfig.json
	tsconfigJSON := `{"compilerOptions": {"target": "ES2020", "strict": true}}`
	os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfigJSON), 0644)

	// Create code with multiple errors
	invalidCode := `const x: number = "string";  // Error 1
const y: string = 123;         // Error 2
const z: boolean = null;       // Error 3`
	os.WriteFile(filepath.Join(tmpDir, "index.ts"), []byte(invalidCode), 0644)

	tsValidator := validator.NewTypeScriptValidator()
	ctx := context.Background()

	// Act
	result, err := tsValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.False(t, result.Success)
	assert.GreaterOrEqual(t, len(result.Errors), 2, "Expected at least 2 errors")
}

func TestTypeScriptValidator_NoTypeScriptFiles(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// Create package.json
	packageJSON := `{"name": "test", "devDependencies": {"typescript": "^5.0.0"}}`
	os.WriteFile(filepath.Join(tmpDir, "package.json"), []byte(packageJSON), 0644)

	// Create tsconfig.json
	tsconfigJSON := `{"compilerOptions": {"target": "ES2020"}}`
	os.WriteFile(filepath.Join(tmpDir, "tsconfig.json"), []byte(tsconfigJSON), 0644)

	// No .ts files

	tsValidator := validator.NewTypeScriptValidator()
	ctx := context.Background()

	// Act
	result, err := tsValidator.Validate(ctx, tmpDir)

	// Assert
	require.NoError(t, err)
	assert.True(t, result.Success, "Expected validation to succeed with no files")
}

func TestTypeScriptValidator_NoTsconfigJSON(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()

	// No tsconfig.json

	validCode := `export const x = 1;`
	os.WriteFile(filepath.Join(tmpDir, "index.ts"), []byte(validCode), 0644)

	tsValidator := validator.NewTypeScriptValidator()
	ctx := context.Background()

	// Act
	_, err := tsValidator.Validate(ctx, tmpDir)

	// Assert
	assert.Error(t, err, "Expected error when tsconfig.json not found")
	assert.Contains(t, err.Error(), "tsconfig.json")
}
