//go:build unit

package driftadopt_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiffApplier_ApplyChanges(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "index.ts")
	originalCode := `export const bucket = new aws.s3.Bucket("my-bucket", {
    tags: { Environment: "dev" }
});`
	err := os.WriteFile(filePath, []byte(originalCode), 0644)
	require.NoError(t, err)

	applier := driftadopt.NewDiffApplier(tmpDir)
	changes := []driftadopt.FileChange{
		{
			FilePath: filePath,
			NewCode: `export const bucket = new aws.s3.Bucket("my-bucket", {
    tags: { Environment: "production" }
});`,
		},
	}

	// Act
	diffID, err := applier.ApplyChanges("step-001", changes)
	require.NoError(t, err)

	// Assert
	assert.NotEmpty(t, diffID)
	newContent, _ := os.ReadFile(filePath)
	assert.Contains(t, string(newContent), "production")
	assert.NotContains(t, string(newContent), "dev")
}

func TestDiffApplier_RecordsOriginalState(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "index.ts")
	originalCode := "const x = 1;"
	err := os.WriteFile(filePath, []byte(originalCode), 0644)
	require.NoError(t, err)

	applier := driftadopt.NewDiffApplier(tmpDir)
	changes := []driftadopt.FileChange{{FilePath: filePath, NewCode: "const x = 2;"}}

	// Act
	diffID, err := applier.ApplyChanges("step-001", changes)
	require.NoError(t, err)

	// Assert - check that original is recorded
	recorder := applier.GetRecorder()
	diff, err := recorder.GetDiff(diffID)
	require.NoError(t, err)
	assert.Equal(t, "step-001", diff.StepID)
	assert.Equal(t, originalCode, diff.Files[filePath])
	assert.True(t, diff.Applied)
}

func TestDiffApplier_MultipleFiles(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "file1.ts")
	file2 := filepath.Join(tmpDir, "file2.ts")
	err := os.WriteFile(file1, []byte("// file 1"), 0644)
	require.NoError(t, err)
	err = os.WriteFile(file2, []byte("// file 2"), 0644)
	require.NoError(t, err)

	applier := driftadopt.NewDiffApplier(tmpDir)
	changes := []driftadopt.FileChange{
		{FilePath: file1, NewCode: "// file 1 updated"},
		{FilePath: file2, NewCode: "// file 2 updated"},
	}

	// Act
	diffID, err := applier.ApplyChanges("step-001", changes)
	require.NoError(t, err)

	// Assert - both files updated
	content1, _ := os.ReadFile(file1)
	content2, _ := os.ReadFile(file2)
	assert.Contains(t, string(content1), "updated")
	assert.Contains(t, string(content2), "updated")

	// Assert - both originals recorded
	recorder := applier.GetRecorder()
	diff, _ := recorder.GetDiff(diffID)
	assert.Len(t, diff.Files, 2)
}

func TestDiffApplier_FileNotFound(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	applier := driftadopt.NewDiffApplier(tmpDir)
	changes := []driftadopt.FileChange{
		{FilePath: "/nonexistent/file.ts", NewCode: "some code"},
	}

	// Act
	_, err := applier.ApplyChanges("step-001", changes)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read file")
}

func TestDiffApplier_EmptyChanges(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	applier := driftadopt.NewDiffApplier(tmpDir)

	// Act
	diffID, err := applier.ApplyChanges("step-001", []driftadopt.FileChange{})

	// Assert
	require.NoError(t, err)
	assert.NotEmpty(t, diffID)
}

func TestDiffApplier_SequentialDiffIDs(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.ts")
	os.WriteFile(filePath, []byte("v1"), 0644)

	applier := driftadopt.NewDiffApplier(tmpDir)

	// Act
	diffID1, err1 := applier.ApplyChanges("step-001", []driftadopt.FileChange{
		{FilePath: filePath, NewCode: "v2"},
	})
	require.NoError(t, err1)

	os.WriteFile(filePath, []byte("v2"), 0644)
	diffID2, err2 := applier.ApplyChanges("step-002", []driftadopt.FileChange{
		{FilePath: filePath, NewCode: "v3"},
	})
	require.NoError(t, err2)

	// Assert - IDs should be sequential
	assert.Equal(t, "001", diffID1)
	assert.Equal(t, "002", diffID2)
}
