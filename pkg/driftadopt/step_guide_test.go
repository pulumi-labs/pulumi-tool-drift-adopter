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

func TestStepGuide_ShowStep(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "index.ts")
	code := `export const bucket = new aws.s3.Bucket("my-bucket");`
	err := os.WriteFile(filePath, []byte(code), 0644)
	require.NoError(t, err)

	plan := &driftadopt.DriftPlan{
		Steps: []driftadopt.DriftStep{
			{
				ID:    "step-001",
				Order: 1,
				Resources: []driftadopt.ResourceDiff{
					{
						URN:        "urn:pulumi:dev::app::aws:s3/bucket:Bucket::my-bucket",
						Type:       "aws:s3/bucket:Bucket",
						Name:       "my-bucket",
						DiffType:   driftadopt.DiffTypeUpdate,
						SourceFile: filePath,
						PropertyDiff: []driftadopt.PropChange{
							{
								Path:     "tags.Environment",
								OldValue: nil,
								NewValue: "production",
								DiffKind: "add",
							},
						},
					},
				},
				Status: driftadopt.StepPending,
			},
		},
	}

	guide := driftadopt.NewStepGuide(tmpDir)

	// Act
	info, err := guide.ShowStep(plan, "step-001")
	require.NoError(t, err)

	// Assert
	assert.Equal(t, "step-001", info.StepID)
	assert.Len(t, info.Resources, 1)
	assert.Contains(t, info.CurrentCode[filePath], "my-bucket")
	assert.Len(t, info.ExpectedChanges, 1)
	assert.Contains(t, info.ExpectedChanges[0], "tags.Environment")
	assert.Contains(t, info.ExpectedChanges[0], "production")
}

func TestStepGuide_StepNotFound(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	plan := &driftadopt.DriftPlan{
		Steps: []driftadopt.DriftStep{},
	}

	guide := driftadopt.NewStepGuide(tmpDir)

	// Act
	_, err := guide.ShowStep(plan, "nonexistent")

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "step not found")
}

func TestStepGuide_MultipleResources(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	file1 := filepath.Join(tmpDir, "bucket.ts")
	file2 := filepath.Join(tmpDir, "object.ts")
	os.WriteFile(file1, []byte("bucket code"), 0644)
	os.WriteFile(file2, []byte("object code"), 0644)

	plan := &driftadopt.DriftPlan{
		Steps: []driftadopt.DriftStep{
			{
				ID:    "step-001",
				Order: 1,
				Resources: []driftadopt.ResourceDiff{
					{
						URN:        "urn:1",
						Name:       "bucket",
						SourceFile: file1,
						PropertyDiff: []driftadopt.PropChange{
							{Path: "versioning.enabled", OldValue: false, NewValue: true, DiffKind: "update"},
						},
					},
					{
						URN:        "urn:2",
						Name:       "object",
						SourceFile: file2,
						PropertyDiff: []driftadopt.PropChange{
							{Path: "content", OldValue: "old", NewValue: "new", DiffKind: "update"},
						},
					},
				},
			},
		},
	}

	guide := driftadopt.NewStepGuide(tmpDir)

	// Act
	info, err := guide.ShowStep(plan, "step-001")
	require.NoError(t, err)

	// Assert
	assert.Len(t, info.Resources, 2)
	assert.Len(t, info.CurrentCode, 2)
	assert.Len(t, info.ExpectedChanges, 2)
}

func TestStepGuide_FormatPropertyChange_Add(t *testing.T) {
	// Arrange
	guide := driftadopt.NewStepGuide("")
	propChange := driftadopt.PropChange{
		Path:     "tags.Owner",
		NewValue: "team-a",
		DiffKind: "add",
	}

	// Act
	description := guide.FormatPropertyChange(propChange)

	// Assert
	assert.Contains(t, description, "Add")
	assert.Contains(t, description, "tags.Owner")
	assert.Contains(t, description, "team-a")
}

func TestStepGuide_FormatPropertyChange_Delete(t *testing.T) {
	// Arrange
	guide := driftadopt.NewStepGuide("")
	propChange := driftadopt.PropChange{
		Path:     "tags.Environment",
		OldValue: "dev",
		DiffKind: "delete",
	}

	// Act
	description := guide.FormatPropertyChange(propChange)

	// Assert
	assert.Contains(t, description, "Delete")
	assert.Contains(t, description, "tags.Environment")
	assert.Contains(t, description, "dev")
}

func TestStepGuide_FormatPropertyChange_Update(t *testing.T) {
	// Arrange
	guide := driftadopt.NewStepGuide("")
	propChange := driftadopt.PropChange{
		Path:     "tags.Owner",
		OldValue: "team-a",
		NewValue: "team-b",
		DiffKind: "update",
	}

	// Act
	description := guide.FormatPropertyChange(propChange)

	// Assert
	assert.Contains(t, description, "Update")
	assert.Contains(t, description, "tags.Owner")
	assert.Contains(t, description, "team-a")
	assert.Contains(t, description, "team-b")
}

func TestStepGuide_WithDependencies(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	plan := &driftadopt.DriftPlan{
		Steps: []driftadopt.DriftStep{
			{
				ID:           "step-002",
				Order:        2,
				Resources:    []driftadopt.ResourceDiff{},
				Dependencies: []string{"step-001"},
			},
		},
	}

	guide := driftadopt.NewStepGuide(tmpDir)

	// Act
	info, err := guide.ShowStep(plan, "step-002")
	require.NoError(t, err)

	// Assert
	assert.Len(t, info.Dependencies, 1)
	assert.Contains(t, info.Dependencies, "step-001")
}

func TestStepGuide_SourceFileNotFound(t *testing.T) {
	// Arrange
	tmpDir := t.TempDir()
	plan := &driftadopt.DriftPlan{
		Steps: []driftadopt.DriftStep{
			{
				ID:    "step-001",
				Order: 1,
				Resources: []driftadopt.ResourceDiff{
					{
						URN:        "urn:1",
						SourceFile: "/nonexistent/file.ts",
					},
				},
			},
		},
	}

	guide := driftadopt.NewStepGuide(tmpDir)

	// Act
	_, err := guide.ShowStep(plan, "step-001")

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "read source file")
}
