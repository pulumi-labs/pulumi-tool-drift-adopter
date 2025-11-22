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

func TestDiffRecorder_RecordAndRetrieve(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	recorder := driftadopt.NewDiffRecorder(tmpDir)

	diff := &driftadopt.DiffRecord{
		ID:        "001",
		StepID:   "step-001",
		Timestamp: time.Now(),
		Files: map[string]string{
			"/path/to/file.ts": "original content",
		},
		Applied: true,
	}

	// Act
	err := recorder.RecordDiff(diff)
	require.NoError(t, err)

	// Assert
	retrieved, err := recorder.GetDiff("001")
	require.NoError(t, err)
	assert.Equal(t, "001", retrieved.ID)
	assert.Equal(t, "step-001", retrieved.StepID)
	assert.True(t, retrieved.Applied)
	assert.Equal(t, "original content", retrieved.Files["/path/to/file.ts"])
}

func TestDiffRecorder_ListDiffs(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	recorder := driftadopt.NewDiffRecorder(tmpDir)

	recorder.RecordDiff(&driftadopt.DiffRecord{ID: "001", StepID: "step-001", Applied: true})
	recorder.RecordDiff(&driftadopt.DiffRecord{ID: "002", StepID: "step-002", Applied: false})

	// Act
	diffs, err := recorder.ListDiffs()
	require.NoError(t, err)

	// Assert
	assert.Len(t, diffs, 2)
	assert.Equal(t, "001", diffs[0].ID)
	assert.Equal(t, "002", diffs[1].ID)
}

func TestDiffRecorder_Rollback(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.ts")
	err := os.WriteFile(filePath, []byte("new content"), 0644)
	require.NoError(t, err)

	recorder := driftadopt.NewDiffRecorder(filepath.Join(tmpDir, "diffs"))
	diff := &driftadopt.DiffRecord{
		ID:      "001",
		StepID: "step-001",
		Files: map[string]string{
			filePath: "original content",
		},
		Applied: true,
	}
	recorder.RecordDiff(diff)

	// Act
	err = recorder.Rollback("001")
	require.NoError(t, err)

	// Assert - file restored
	content, _ := os.ReadFile(filePath)
	assert.Equal(t, "original content", string(content))

	// Assert - diff marked as unapplied
	retrieved, _ := recorder.GetDiff("001")
	assert.False(t, retrieved.Applied)
}

func TestDiffRecorder_NextID(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	recorder := driftadopt.NewDiffRecorder(tmpDir)

	// Act
	id1 := recorder.NextID()
	recorder.RecordDiff(&driftadopt.DiffRecord{ID: id1, StepID: "c1"})
	id2 := recorder.NextID()

	// Assert
	assert.Equal(t, "001", id1)
	assert.Equal(t, "002", id2)
}

func TestDiffRecorder_GetDiff_NotFound(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	recorder := driftadopt.NewDiffRecorder(tmpDir)

	// Act
	_, err := recorder.GetDiff("999")

	// Assert
	assert.Error(t, err)
}

func TestDiffRecorder_Rollback_NotApplied(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	recorder := driftadopt.NewDiffRecorder(tmpDir)

	diff := &driftadopt.DiffRecord{
		ID:      "001",
		StepID: "step-001",
		Files:   map[string]string{},
		Applied: false,
	}
	recorder.RecordDiff(diff)

	// Act
	err := recorder.Rollback("001")

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not currently applied")
}

func TestDiffRecorder_EmptyDirectory(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	recorder := driftadopt.NewDiffRecorder(tmpDir)

	// Act
	diffs, err := recorder.ListDiffs()

	// Assert
	require.NoError(t, err)
	assert.Empty(t, diffs)
}

func TestDiffRecorder_UpdateExistingDiff(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	recorder := driftadopt.NewDiffRecorder(tmpDir)

	diff := &driftadopt.DiffRecord{
		ID:      "001",
		StepID: "step-001",
		Applied: true,
	}
	recorder.RecordDiff(diff)

	// Act - update the same diff
	diff.Applied = false
	err := recorder.RecordDiff(diff)
	require.NoError(t, err)

	// Assert - should be updated
	retrieved, _ := recorder.GetDiff("001")
	assert.False(t, retrieved.Applied)
}
