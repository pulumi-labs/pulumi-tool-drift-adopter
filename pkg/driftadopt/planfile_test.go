//go:build unit

package driftadopt_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPlanFile_ReadWrite(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	planPath := filepath.Join(tempDir, "drift-plan.json")

	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		TotalChunks: 1,
		Chunks: []driftadopt.DriftChunk{
			{ID: "chunk-001", Status: driftadopt.ChunkPending, Order: 0},
		},
	}

	// Act - Write
	err := driftadopt.WritePlanFile(planPath, plan)
	require.NoError(t, err)

	// Act - Read
	loaded, err := driftadopt.ReadPlanFile(planPath)
	require.NoError(t, err)

	// Assert
	assert.Equal(t, plan.Stack, loaded.Stack)
	assert.Equal(t, plan.TotalChunks, loaded.TotalChunks)
	assert.Equal(t, plan.GeneratedAt.Unix(), loaded.GeneratedAt.Unix())
	assert.Len(t, loaded.Chunks, 1)
	assert.Equal(t, "chunk-001", loaded.Chunks[0].ID)
	assert.Equal(t, driftadopt.ChunkPending, loaded.Chunks[0].Status)
}

func TestPlanFile_UpdateChunkStatus(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	planPath := filepath.Join(tempDir, "drift-plan.json")

	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		TotalChunks: 2,
		Chunks: []driftadopt.DriftChunk{
			{ID: "chunk-001", Status: driftadopt.ChunkPending, Order: 0},
			{ID: "chunk-002", Status: driftadopt.ChunkPending, Order: 1},
		},
	}

	// Initial write
	err := driftadopt.WritePlanFile(planPath, plan)
	require.NoError(t, err)

	// Act - Update status
	plan.Chunks[0].Status = driftadopt.ChunkCompleted
	plan.Chunks[0].Attempt = 1
	err = driftadopt.WritePlanFile(planPath, plan)
	require.NoError(t, err)

	// Reload and verify
	loaded, err := driftadopt.ReadPlanFile(planPath)
	require.NoError(t, err)

	// Assert
	assert.Equal(t, driftadopt.ChunkCompleted, loaded.Chunks[0].Status)
	assert.Equal(t, 1, loaded.Chunks[0].Attempt)
	assert.Equal(t, driftadopt.ChunkPending, loaded.Chunks[1].Status)
}

func TestPlanFile_InvalidJSON(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	planPath := filepath.Join(tempDir, "invalid.json")

	// Write invalid JSON
	err := os.WriteFile(planPath, []byte("not json content"), 0644)
	require.NoError(t, err)

	// Act
	_, err = driftadopt.ReadPlanFile(planPath)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestPlanFile_NotExists(t *testing.T) {
	// Arrange
	nonexistentPath := "/nonexistent/plan.json"

	// Act
	_, err := driftadopt.ReadPlanFile(nonexistentPath)

	// Assert
	assert.Error(t, err)
	assert.True(t, os.IsNotExist(err), "expected file not found error")
}

func TestPlanFile_ComplexPlan(t *testing.T) {
	// Arrange - Complex plan with multiple chunks and dependencies
	tempDir := t.TempDir()
	planPath := filepath.Join(tempDir, "drift-plan.json")

	plan := &driftadopt.DriftPlan{
		Stack:       "production",
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		TotalChunks: 3,
		Chunks: []driftadopt.DriftChunk{
			{
				ID:    "chunk-001",
				Order: 0,
				Resources: []driftadopt.ResourceDiff{
					{
						URN:      "urn:pulumi:prod::app::aws:s3/bucket:Bucket::data",
						Type:     "aws:s3/bucket:Bucket",
						Name:     "data",
						DiffType: driftadopt.DiffTypeUpdate,
						PropertyDiff: []driftadopt.PropChange{
							{
								Path:     "versioning.enabled",
								OldValue: false,
								NewValue: true,
								DiffKind: "update",
							},
						},
						SourceFile: "infrastructure/storage.ts",
						SourceLine: 15,
					},
				},
				Status:       driftadopt.ChunkCompleted,
				Dependencies: []string{},
				Attempt:      1,
				LastError:    "",
			},
			{
				ID:           "chunk-002",
				Order:        1,
				Resources:    []driftadopt.ResourceDiff{},
				Status:       driftadopt.ChunkFailed,
				Dependencies: []string{"chunk-003"},
				Attempt:      2,
				LastError:    "compilation failed",
			},
			{
				ID:           "chunk-003",
				Order:        2,
				Resources:    []driftadopt.ResourceDiff{},
				Status:       driftadopt.ChunkSkipped,
				Dependencies: []string{},
				Attempt:      0,
				LastError:    "skipped by user",
			},
		},
	}

	// Act - Write and read
	err := driftadopt.WritePlanFile(planPath, plan)
	require.NoError(t, err)

	loaded, err := driftadopt.ReadPlanFile(planPath)
	require.NoError(t, err)

	// Assert - All complex fields preserved
	assert.Equal(t, plan.Stack, loaded.Stack)
	assert.Equal(t, plan.TotalChunks, loaded.TotalChunks)
	assert.Len(t, loaded.Chunks, 3)

	// Check first chunk details
	assert.Equal(t, "chunk-001", loaded.Chunks[0].ID)
	assert.Equal(t, driftadopt.ChunkCompleted, loaded.Chunks[0].Status)
	assert.Len(t, loaded.Chunks[0].Resources, 1)
	assert.Equal(t, "data", loaded.Chunks[0].Resources[0].Name)
	assert.Equal(t, driftadopt.DiffTypeUpdate, loaded.Chunks[0].Resources[0].DiffType)
	assert.Len(t, loaded.Chunks[0].Resources[0].PropertyDiff, 1)

	// Check failed chunk
	assert.Equal(t, driftadopt.ChunkFailed, loaded.Chunks[1].Status)
	assert.Equal(t, 2, loaded.Chunks[1].Attempt)
	assert.Equal(t, "compilation failed", loaded.Chunks[1].LastError)
	assert.Contains(t, loaded.Chunks[1].Dependencies, "chunk-003")

	// Check skipped chunk
	assert.Equal(t, driftadopt.ChunkSkipped, loaded.Chunks[2].Status)
	assert.Equal(t, "skipped by user", loaded.Chunks[2].LastError)
}

func TestPlanFile_PrettyPrinted(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	planPath := filepath.Join(tempDir, "drift-plan.json")

	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		TotalChunks: 1,
		Chunks: []driftadopt.DriftChunk{
			{ID: "chunk-001", Status: driftadopt.ChunkPending, Order: 0},
		},
	}

	// Act
	err := driftadopt.WritePlanFile(planPath, plan)
	require.NoError(t, err)

	// Read raw content
	content, err := os.ReadFile(planPath)
	require.NoError(t, err)

	// Assert - Should be formatted with indentation
	contentStr := string(content)
	assert.Contains(t, contentStr, "\n")      // Has newlines
	assert.Contains(t, contentStr, "  ")      // Has indentation
	assert.Contains(t, contentStr, "\"stack\"") // Has proper field names
}

func TestPlanFile_WriteCreatesDirectory(t *testing.T) {
	// Arrange
	tempDir := t.TempDir()
	nestedPath := filepath.Join(tempDir, "subdir", "nested", "drift-plan.json")

	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now().UTC().Truncate(time.Second),
		TotalChunks: 0,
		Chunks:      []driftadopt.DriftChunk{},
	}

	// Act - Write to nested path (parent dirs don't exist)
	err := driftadopt.WritePlanFile(nestedPath, plan)

	// Assert - Should create directories and succeed
	require.NoError(t, err)

	// Verify file exists
	_, err = os.Stat(nestedPath)
	assert.NoError(t, err)
}
