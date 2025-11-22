//go:build unit

package driftadopt_test

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDriftPlan_Serialization(t *testing.T) {
	// Arrange
	now := time.Now().UTC().Truncate(time.Second)
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: now,
		TotalSteps: 2,
		Steps: []driftadopt.DriftStep{
			{ID: "step-001", Order: 0, Status: driftadopt.StepPending},
			{ID: "step-002", Order: 1, Status: driftadopt.StepPending},
		},
	}

	// Act - Marshal
	data, err := json.Marshal(plan)
	require.NoError(t, err)

	// Act - Unmarshal
	var unmarshaled driftadopt.DriftPlan
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	// Assert
	assert.Equal(t, plan.Stack, unmarshaled.Stack)
	assert.Equal(t, plan.TotalSteps, unmarshaled.TotalSteps)
	assert.Equal(t, plan.GeneratedAt.Unix(), unmarshaled.GeneratedAt.Unix())
	assert.Len(t, unmarshaled.Steps, 2)
	assert.Equal(t, "step-001", unmarshaled.Steps[0].ID)
	assert.Equal(t, "step-002", unmarshaled.Steps[1].ID)
}

func TestDriftStep_Ordering(t *testing.T) {
	// Arrange - Steps out of order
	steps := []driftadopt.DriftStep{
		{ID: "step-003", Order: 2},
		{ID: "step-001", Order: 0},
		{ID: "step-002", Order: 1},
	}

	// Act - Sort by order
	sort.Slice(steps, func(i, j int) bool {
		return steps[i].Order < steps[j].Order
	})

	// Assert - Correct order
	assert.Equal(t, "step-001", steps[0].ID)
	assert.Equal(t, "step-002", steps[1].ID)
	assert.Equal(t, "step-003", steps[2].ID)
	assert.Equal(t, 0, steps[0].Order)
	assert.Equal(t, 1, steps[1].Order)
	assert.Equal(t, 2, steps[2].Order)
}

func TestResourceDiff_PropertyPaths(t *testing.T) {
	// Arrange - Resource with nested property change
	diff := driftadopt.ResourceDiff{
		URN:  "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
		Type: "aws:s3/bucket:Bucket",
		Name: "my-bucket",
		PropertyDiff: []driftadopt.PropChange{
			{
				Path:     "tags.Environment",
				OldValue: "dev",
				NewValue: "production",
				DiffKind: "update",
			},
		},
	}

	// Act - Parse path
	parts := strings.Split(diff.PropertyDiff[0].Path, ".")

	// Assert
	assert.Equal(t, []string{"tags", "Environment"}, parts)
	assert.Equal(t, "dev", diff.PropertyDiff[0].OldValue)
	assert.Equal(t, "production", diff.PropertyDiff[0].NewValue)
	assert.Equal(t, "update", diff.PropertyDiff[0].DiffKind)
}

func TestPropChange_Types(t *testing.T) {
	tests := []struct {
		name     string
		change   driftadopt.PropChange
		wantType string
	}{
		{
			name: "string",
			change: driftadopt.PropChange{
				Path:     "name",
				OldValue: "old",
				NewValue: "new",
				DiffKind: "update",
			},
			wantType: "string",
		},
		{
			name: "number",
			change: driftadopt.PropChange{
				Path:     "count",
				OldValue: float64(1), // JSON unmarshals numbers as float64
				NewValue: float64(2),
				DiffKind: "update",
			},
			wantType: "float64",
		},
		{
			name: "bool",
			change: driftadopt.PropChange{
				Path:     "enabled",
				OldValue: false,
				NewValue: true,
				DiffKind: "update",
			},
			wantType: "bool",
		},
		{
			name: "float",
			change: driftadopt.PropChange{
				Path:     "price",
				OldValue: 9.99,
				NewValue: 19.99,
				DiffKind: "update",
			},
			wantType: "float64",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Act - Serialize and deserialize
			data, err := json.Marshal(tt.change)
			require.NoError(t, err)

			var unmarshaled driftadopt.PropChange
			err = json.Unmarshal(data, &unmarshaled)
			require.NoError(t, err)

			// Assert - Values preserved
			assert.Equal(t, tt.change.Path, unmarshaled.Path)
			assert.Equal(t, tt.change.OldValue, unmarshaled.OldValue)
			assert.Equal(t, tt.change.NewValue, unmarshaled.NewValue)
			assert.Equal(t, tt.change.DiffKind, unmarshaled.DiffKind)
		})
	}
}

func TestDiffType_Constants(t *testing.T) {
	// Test that DiffType constants are defined correctly
	assert.Equal(t, driftadopt.DiffType("update"), driftadopt.DiffTypeUpdate)
	assert.Equal(t, driftadopt.DiffType("delete"), driftadopt.DiffTypeDelete)
	assert.Equal(t, driftadopt.DiffType("replace"), driftadopt.DiffTypeReplace)
}

func TestStepStatus_Constants(t *testing.T) {
	// Test that StepStatus constants are defined correctly
	assert.Equal(t, driftadopt.StepStatus("pending"), driftadopt.StepPending)
	assert.Equal(t, driftadopt.StepStatus("in_progress"), driftadopt.StepInProgress)
	assert.Equal(t, driftadopt.StepStatus("completed"), driftadopt.StepCompleted)
	assert.Equal(t, driftadopt.StepStatus("failed"), driftadopt.StepFailed)
	assert.Equal(t, driftadopt.StepStatus("skipped"), driftadopt.StepSkipped)
}

func TestDriftStep_CompleteStructure(t *testing.T) {
	// Test a complete step with all fields
	step := driftadopt.DriftStep{
		ID:    "step-001",
		Order: 0,
		Resources: []driftadopt.ResourceDiff{
			{
				URN:      "urn:pulumi:dev::test::aws:s3/bucket:Bucket::my-bucket",
				Type:     "aws:s3/bucket:Bucket",
				Name:     "my-bucket",
				DiffType: driftadopt.DiffTypeUpdate,
				PropertyDiff: []driftadopt.PropChange{
					{
						Path:     "tags.Environment",
						OldValue: "dev",
						NewValue: "production",
						DiffKind: "update",
					},
				},
				SourceFile: "index.ts",
				SourceLine: 42,
			},
		},
		Status:       driftadopt.StepPending,
		Dependencies: []string{"step-000"},
		Attempt:      0,
		LastError:    "",
	}

	// Act - Serialize and deserialize
	data, err := json.Marshal(step)
	require.NoError(t, err)

	var unmarshaled driftadopt.DriftStep
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	// Assert - All fields preserved
	assert.Equal(t, step.ID, unmarshaled.ID)
	assert.Equal(t, step.Order, unmarshaled.Order)
	assert.Len(t, unmarshaled.Resources, 1)
	assert.Equal(t, step.Resources[0].URN, unmarshaled.Resources[0].URN)
	assert.Equal(t, step.Resources[0].Type, unmarshaled.Resources[0].Type)
	assert.Equal(t, step.Resources[0].DiffType, unmarshaled.Resources[0].DiffType)
	assert.Equal(t, step.Status, unmarshaled.Status)
	assert.Equal(t, step.Dependencies, unmarshaled.Dependencies)
	assert.Equal(t, step.Attempt, unmarshaled.Attempt)
}

func TestDriftPlan_EmptySteps(t *testing.T) {
	// Test plan with no steps
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalSteps: 0,
		Steps:      []driftadopt.DriftStep{},
	}

	// Act
	data, err := json.Marshal(plan)
	require.NoError(t, err)

	var unmarshaled driftadopt.DriftPlan
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	// Assert
	assert.Equal(t, 0, unmarshaled.TotalSteps)
	assert.Empty(t, unmarshaled.Steps)
}
