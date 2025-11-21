//go:build unit

package driftadopt_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt/validator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplyOrchestrator_SuccessfulApplication(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	planFile := filepath.Join(tmpDir, "drift-plan.json")

	// Create a simple plan
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		TotalChunks: 1,
		Chunks: []driftadopt.DriftChunk{
			{
				ID:     "chunk-001",
				Order:  1,
				Status: driftadopt.ChunkPending,
				Resources: []driftadopt.ResourceDiff{
					{
						URN:  "urn:test",
						Type: "test:resource:Type",
						Name: "test-resource",
						PropertyDiff: []driftadopt.PropChange{
							{Path: "value", OldValue: "old", NewValue: "new", DiffKind: "update"},
						},
					},
				},
			},
		},
	}

	err := driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Create source file
	sourceFile := filepath.Join(tmpDir, "index.ts")
	os.WriteFile(sourceFile, []byte(`export const x = "old";`), 0644)
	plan.Chunks[0].Resources[0].SourceFile = sourceFile
	driftadopt.WritePlanFile(planFile, plan)

	// Create mock validator that always succeeds
	mockValidator := &MockValidator{Success: true}

	// Create mock preview runner that returns matching preview
	mockPreviewRunner := &MockPreviewRunner{
		Diffs: []driftadopt.ResourceDiff{
			{
				URN:  "urn:test",
				Type: "test:resource:Type",
				PropertyDiff: []driftadopt.PropChange{
					{Path: "value", OldValue: "old", NewValue: "new", DiffKind: "update"},
				},
			},
		},
	}

	orchestrator := driftadopt.NewApplyOrchestrator(tmpDir, mockValidator, mockPreviewRunner)

	// Prepare code changes
	changes := []driftadopt.FileChange{
		{FilePath: sourceFile, NewCode: `export const x = "new";`},
	}

	// Act
	result, err := orchestrator.ApplyDiff(context.Background(), planFile, "chunk-001", changes)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, driftadopt.ChunkCompleted, result.Status)
	assert.True(t, result.CompileSuccess)
	assert.True(t, result.DiffMatches)
	assert.NotEmpty(t, result.DiffID)

	// Verify plan was updated
	updatedPlan, _ := driftadopt.ReadPlanFile(planFile)
	assert.Equal(t, driftadopt.ChunkCompleted, updatedPlan.Chunks[0].Status)
}

func TestApplyOrchestrator_CompilationFailure(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	planFile := filepath.Join(tmpDir, "drift-plan.json")

	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		TotalChunks: 1,
		Chunks: []driftadopt.DriftChunk{
			{
				ID:        "chunk-001",
				Order:     1,
				Status:    driftadopt.ChunkPending,
				Resources: []driftadopt.ResourceDiff{{URN: "urn:test"}},
			},
		},
	}
	driftadopt.WritePlanFile(planFile, plan)

	sourceFile := filepath.Join(tmpDir, "index.ts")
	os.WriteFile(sourceFile, []byte(`const x = 1;`), 0644)

	// Mock validator that fails
	mockValidator := &MockValidator{
		Success: false,
		Errors: []validator.CompilationError{
			{File: sourceFile, Line: 1, Message: "Type error"},
		},
	}

	orchestrator := driftadopt.NewApplyOrchestrator(tmpDir, mockValidator, nil)

	changes := []driftadopt.FileChange{
		{FilePath: sourceFile, NewCode: `const x: number = "string";`},
	}

	// Act
	result, err := orchestrator.ApplyDiff(context.Background(), planFile, "chunk-001", changes)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, driftadopt.ChunkFailed, result.Status)
	assert.False(t, result.CompileSuccess)
	assert.Contains(t, result.ErrorMessage, "Compilation failed")
	assert.NotEmpty(t, result.Suggestions)

	// Verify chunk marked as failed in plan
	updatedPlan, _ := driftadopt.ReadPlanFile(planFile)
	assert.Equal(t, driftadopt.ChunkFailed, updatedPlan.Chunks[0].Status)
}

func TestApplyOrchestrator_DiffMismatch(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	planFile := filepath.Join(tmpDir, "drift-plan.json")

	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		TotalChunks: 1,
		Chunks: []driftadopt.DriftChunk{
			{
				ID:     "chunk-001",
				Order:  1,
				Status: driftadopt.ChunkPending,
				Resources: []driftadopt.ResourceDiff{
					{
						URN: "urn:test",
						PropertyDiff: []driftadopt.PropChange{
							{Path: "value", OldValue: "a", NewValue: "b", DiffKind: "update"},
						},
					},
				},
			},
		},
	}
	driftadopt.WritePlanFile(planFile, plan)

	sourceFile := filepath.Join(tmpDir, "index.ts")
	os.WriteFile(sourceFile, []byte(`export const x = 1;`), 0644)

	mockValidator := &MockValidator{Success: true}

	// Preview returns different changes than expected
	mockPreviewRunner := &MockPreviewRunner{
		Diffs: []driftadopt.ResourceDiff{
			{
				URN: "urn:test",
				PropertyDiff: []driftadopt.PropChange{
					{Path: "otherValue", OldValue: "x", NewValue: "y", DiffKind: "update"}, // Wrong property
				},
			},
		},
	}

	orchestrator := driftadopt.NewApplyOrchestrator(tmpDir, mockValidator, mockPreviewRunner)

	changes := []driftadopt.FileChange{
		{FilePath: sourceFile, NewCode: `export const x = 2;`},
	}

	// Act
	result, err := orchestrator.ApplyDiff(context.Background(), planFile, "chunk-001", changes)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, driftadopt.ChunkFailed, result.Status)
	assert.True(t, result.CompileSuccess)
	assert.False(t, result.DiffMatches)
	assert.Contains(t, result.ErrorMessage, "Preview mismatch")

	// Verify chunk marked as failed
	updatedPlan, _ := driftadopt.ReadPlanFile(planFile)
	assert.Equal(t, driftadopt.ChunkFailed, updatedPlan.Chunks[0].Status)
}

func TestApplyOrchestrator_AutomaticRollback(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	planFile := filepath.Join(tmpDir, "drift-plan.json")

	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		TotalChunks: 1,
		Chunks: []driftadopt.DriftChunk{
			{ID: "chunk-001", Order: 1, Status: driftadopt.ChunkPending, Resources: []driftadopt.ResourceDiff{{URN: "urn:test"}}},
		},
	}
	driftadopt.WritePlanFile(planFile, plan)

	sourceFile := filepath.Join(tmpDir, "index.ts")
	originalContent := `export const x = "original";`
	os.WriteFile(sourceFile, []byte(originalContent), 0644)

	mockValidator := &MockValidator{Success: false, Errors: []validator.CompilationError{{File: sourceFile, Line: 1, Message: "Error"}}}

	orchestrator := driftadopt.NewApplyOrchestrator(tmpDir, mockValidator, nil)

	changes := []driftadopt.FileChange{
		{FilePath: sourceFile, NewCode: `export const x = "broken";`},
	}

	// Act
	result, err := orchestrator.ApplyDiff(context.Background(), planFile, "chunk-001", changes)

	// Assert
	require.NoError(t, err)
	assert.Equal(t, driftadopt.ChunkFailed, result.Status)

	// Verify file was rolled back to original content
	content, _ := os.ReadFile(sourceFile)
	assert.Equal(t, originalContent, string(content), "File should be rolled back on compilation failure")
}

func TestApplyOrchestrator_ChunkNotFound(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	planFile := filepath.Join(tmpDir, "drift-plan.json")

	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		TotalChunks: 1,
		Chunks:      []driftadopt.DriftChunk{{ID: "chunk-001", Order: 1, Resources: []driftadopt.ResourceDiff{}}},
	}
	driftadopt.WritePlanFile(planFile, plan)

	orchestrator := driftadopt.NewApplyOrchestrator(tmpDir, &MockValidator{Success: true}, nil)

	// Act
	_, err := orchestrator.ApplyDiff(context.Background(), planFile, "nonexistent", []driftadopt.FileChange{})

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "chunk not found")
}

// MockValidator for testing
type MockValidator struct {
	Success bool
	Errors  []validator.CompilationError
}

func (m *MockValidator) Validate(ctx context.Context, projectPath string) (*validator.ValidationResult, error) {
	return &validator.ValidationResult{
		Success: m.Success,
		Errors:  m.Errors,
	}, nil
}

// MockPreviewRunner for testing
type MockPreviewRunner struct {
	Diffs []driftadopt.ResourceDiff
	Error error
}

func (m *MockPreviewRunner) RunPreview(ctx context.Context, projectPath string) ([]driftadopt.ResourceDiff, error) {
	if m.Error != nil {
		return nil, m.Error
	}
	return m.Diffs, nil
}
