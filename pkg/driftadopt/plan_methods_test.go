//go:build unit

package driftadopt_test

import (
	"testing"
	"time"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDriftPlan_GetStep(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalSteps:  3,
		Steps: []driftadopt.DriftStep{
			{ID: "step-001", Order: 0},
			{ID: "step-002", Order: 1},
			{ID: "step-003", Order: 2},
		},
	}

	// Act & Assert - Found
	step := plan.GetStep("step-002")
	require.NotNil(t, step)
	assert.Equal(t, "step-002", step.ID)
	assert.Equal(t, 1, step.Order)

	// Act & Assert - Not found
	step = plan.GetStep("step-999")
	assert.Nil(t, step)

	// Act & Assert - Empty plan
	emptyPlan := &driftadopt.DriftPlan{Steps: []driftadopt.DriftStep{}}
	step = emptyPlan.GetStep("step-001")
	assert.Nil(t, step)
}

func TestDriftPlan_GetNextPendingStep(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalSteps:  4,
		Steps: []driftadopt.DriftStep{
			{ID: "step-001", Order: 0, Status: driftadopt.StepCompleted},
			{ID: "step-002", Order: 1, Status: driftadopt.StepPending},
			{ID: "step-003", Order: 2, Status: driftadopt.StepPending},
			{ID: "step-004", Order: 3, Status: driftadopt.StepFailed},
		},
	}

	// Act
	step := plan.GetNextPendingStep()

	// Assert - Returns first pending step
	require.NotNil(t, step)
	assert.Equal(t, "step-002", step.ID)
	assert.Equal(t, driftadopt.StepPending, step.Status)
}

func TestDriftPlan_GetNextPendingStep_NoPending(t *testing.T) {
	// Arrange - All steps completed or failed
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalSteps:  2,
		Steps: []driftadopt.DriftStep{
			{ID: "step-001", Order: 0, Status: driftadopt.StepCompleted},
			{ID: "step-002", Order: 1, Status: driftadopt.StepFailed},
		},
	}

	// Act
	step := plan.GetNextPendingStep()

	// Assert - Returns nil when no pending steps
	assert.Nil(t, step)
}

func TestDriftPlan_GetNextPendingStep_EmptyPlan(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Steps: []driftadopt.DriftStep{},
	}

	// Act
	step := plan.GetNextPendingStep()

	// Assert
	assert.Nil(t, step)
}

func TestDriftPlan_CountByStatus(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalSteps:  6,
		Steps: []driftadopt.DriftStep{
			{ID: "step-001", Status: driftadopt.StepCompleted},
			{ID: "step-002", Status: driftadopt.StepCompleted},
			{ID: "step-003", Status: driftadopt.StepCompleted},
			{ID: "step-004", Status: driftadopt.StepPending},
			{ID: "step-005", Status: driftadopt.StepFailed},
			{ID: "step-006", Status: driftadopt.StepSkipped},
		},
	}

	// Act
	counts := plan.CountByStatus()

	// Assert
	assert.Equal(t, 3, counts[driftadopt.StepCompleted])
	assert.Equal(t, 1, counts[driftadopt.StepPending])
	assert.Equal(t, 1, counts[driftadopt.StepFailed])
	assert.Equal(t, 1, counts[driftadopt.StepSkipped])
	assert.Equal(t, 0, counts[driftadopt.StepInProgress]) // None in progress
}

func TestDriftPlan_CountByStatus_Empty(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Steps: []driftadopt.DriftStep{},
	}

	// Act
	counts := plan.CountByStatus()

	// Assert - All counts should be zero
	assert.Equal(t, 0, counts[driftadopt.StepCompleted])
	assert.Equal(t, 0, counts[driftadopt.StepPending])
	assert.Equal(t, 0, counts[driftadopt.StepFailed])
	assert.Equal(t, 0, counts[driftadopt.StepSkipped])
	assert.Equal(t, 0, counts[driftadopt.StepInProgress])
}

func TestDriftPlan_GetFailedSteps(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalSteps:  5,
		Steps: []driftadopt.DriftStep{
			{ID: "step-001", Status: driftadopt.StepCompleted},
			{ID: "step-002", Status: driftadopt.StepFailed, LastError: "compilation error"},
			{ID: "step-003", Status: driftadopt.StepPending},
			{ID: "step-004", Status: driftadopt.StepFailed, LastError: "diff mismatch"},
			{ID: "step-005", Status: driftadopt.StepSkipped},
		},
	}

	// Act
	failed := plan.GetFailedSteps()

	// Assert
	require.Len(t, failed, 2)
	assert.Equal(t, "step-002", failed[0].ID)
	assert.Equal(t, "compilation error", failed[0].LastError)
	assert.Equal(t, "step-004", failed[1].ID)
	assert.Equal(t, "diff mismatch", failed[1].LastError)
}

func TestDriftPlan_GetFailedSteps_None(t *testing.T) {
	// Arrange - No failed steps
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalSteps:  2,
		Steps: []driftadopt.DriftStep{
			{ID: "step-001", Status: driftadopt.StepCompleted},
			{ID: "step-002", Status: driftadopt.StepPending},
		},
	}

	// Act
	failed := plan.GetFailedSteps()

	// Assert - Empty slice, not nil
	assert.NotNil(t, failed)
	assert.Empty(t, failed)
}

func TestDriftPlan_GetFailedSteps_Empty(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Steps: []driftadopt.DriftStep{},
	}

	// Act
	failed := plan.GetFailedSteps()

	// Assert
	assert.NotNil(t, failed)
	assert.Empty(t, failed)
}

func TestDriftPlan_HelperMethods_Integration(t *testing.T) {
	// Test that all helper methods work together correctly
	plan := &driftadopt.DriftPlan{
		Stack:       "production",
		GeneratedAt: time.Now(),
		TotalSteps:  5,
		Steps: []driftadopt.DriftStep{
			{ID: "step-001", Order: 0, Status: driftadopt.StepCompleted},
			{ID: "step-002", Order: 1, Status: driftadopt.StepPending},
			{ID: "step-003", Order: 2, Status: driftadopt.StepPending},
			{ID: "step-004", Order: 3, Status: driftadopt.StepFailed, LastError: "error 1"},
			{ID: "step-005", Order: 4, Status: driftadopt.StepSkipped},
		},
	}

	// Test GetStep
	step := plan.GetStep("step-003")
	require.NotNil(t, step)
	assert.Equal(t, driftadopt.StepPending, step.Status)

	// Test GetNextPendingStep
	nextStep := plan.GetNextPendingStep()
	require.NotNil(t, nextStep)
	assert.Equal(t, "step-002", nextStep.ID)

	// Test CountByStatus
	counts := plan.CountByStatus()
	assert.Equal(t, 1, counts[driftadopt.StepCompleted])
	assert.Equal(t, 2, counts[driftadopt.StepPending])
	assert.Equal(t, 1, counts[driftadopt.StepFailed])
	assert.Equal(t, 1, counts[driftadopt.StepSkipped])

	// Test GetFailedSteps
	failed := plan.GetFailedSteps()
	require.Len(t, failed, 1)
	assert.Equal(t, "step-004", failed[0].ID)
}
