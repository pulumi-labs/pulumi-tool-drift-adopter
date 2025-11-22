//go:build integration

package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNextCommand_NoPlan(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "drift-adopt-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Set plan file to non-existent path
	planFile := filepath.Join(tmpDir, "drift-plan.json")

	// Execute next command
	rootCmd.SetArgs([]string{"--plan", planFile, "next"})
	err = rootCmd.Execute()

	// Should not error, but should guide user to create plan
	assert.NoError(t, err)
}

func TestNextCommand_WithPendingStep(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "drift-adopt-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a plan with a pending step
	plan := &driftadopt.DriftPlan{
		Stack:      "test-stack",
		TotalSteps: 1,
		Steps: []driftadopt.DriftStep{
			{
				ID:     "step-001",
				Order:  1,
				Status: driftadopt.StepPending,
				Resources: []driftadopt.ResourceDiff{
					{
						URN:  "urn:pulumi:test::test::test:index:Test::mytest",
						Type: "test:index:Test",
						Name: "mytest",
					},
				},
			},
		},
	}

	planFile := filepath.Join(tmpDir, "drift-plan.json")
	err = driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Execute next command
	rootCmd.SetArgs([]string{"--plan", planFile, "next"})
	err = rootCmd.Execute()

	// Should guide user to view step
	assert.NoError(t, err)
}

func TestStatusCommand_WithPlan(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "drift-adopt-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a plan with various statuses
	plan := &driftadopt.DriftPlan{
		Stack:      "test-stack",
		TotalSteps: 4,
		Steps: []driftadopt.DriftStep{
			{
				ID:     "step-001",
				Order:  1,
				Status: driftadopt.StepCompleted,
				Resources: []driftadopt.ResourceDiff{
					{URN: "urn:pulumi:test::test::test:index:Test::test1", Type: "test:index:Test", Name: "test1"},
				},
			},
			{
				ID:     "step-002",
				Order:  2,
				Status: driftadopt.StepPending,
				Resources: []driftadopt.ResourceDiff{
					{URN: "urn:pulumi:test::test::test:index:Test::test2", Type: "test:index:Test", Name: "test2"},
				},
			},
			{
				ID:        "step-003",
				Order:     3,
				Status:    driftadopt.StepFailed,
				LastError: "Compilation failed",
				Resources: []driftadopt.ResourceDiff{
					{URN: "urn:pulumi:test::test::test:index:Test::test3", Type: "test:index:Test", Name: "test3"},
				},
			},
			{
				ID:     "step-004",
				Order:  4,
				Status: driftadopt.StepSkipped,
				Resources: []driftadopt.ResourceDiff{
					{URN: "urn:pulumi:test::test::test:index:Test::test4", Type: "test:index:Test", Name: "test4"},
				},
			},
		},
	}

	planFile := filepath.Join(tmpDir, "drift-plan.json")
	err = driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Execute status command
	rootCmd.SetArgs([]string{"--plan", planFile, "--project", tmpDir, "status"})
	err = rootCmd.Execute()

	// Should display status
	assert.NoError(t, err)
}

func TestSkipCommand(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "drift-adopt-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a plan with a pending step
	plan := &driftadopt.DriftPlan{
		Stack:      "test-stack",
		TotalSteps: 1,
		Steps: []driftadopt.DriftStep{
			{
				ID:     "step-001",
				Order:  1,
				Status: driftadopt.StepPending,
				Resources: []driftadopt.ResourceDiff{
					{URN: "urn:pulumi:test::test::test:index:Test::mytest", Type: "test:index:Test", Name: "mytest"},
				},
			},
		},
	}

	planFile := filepath.Join(tmpDir, "drift-plan.json")
	err = driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Execute skip command
	rootCmd.SetArgs([]string{"--plan", planFile, "skip", "--step", "step-001", "--reason", "Too complex"})
	err = rootCmd.Execute()
	require.NoError(t, err)

	// Verify step was skipped
	updatedPlan, err := driftadopt.ReadPlanFile(planFile)
	require.NoError(t, err)

	step := updatedPlan.GetStep("step-001")
	assert.Equal(t, driftadopt.StepSkipped, step.Status)
	assert.Contains(t, step.LastError, "Too complex")
}

func TestRollbackCommand_InvalidDiff(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "drift-adopt-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Execute rollback command with non-existent diff
	rootCmd.SetArgs([]string{"--project", tmpDir, "rollback", "--diff", "diff-999"})
	err = rootCmd.Execute()

	// Should error because diff doesn't exist
	assert.Error(t, err)
}

func TestShowStepCommand_InvalidStep(t *testing.T) {
	// Create temp directory
	tmpDir, err := os.MkdirTemp("", "drift-adopt-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a plan
	plan := &driftadopt.DriftPlan{
		Stack:      "test-stack",
		TotalSteps: 0,
		Steps:      []driftadopt.DriftStep{},
	}

	planFile := filepath.Join(tmpDir, "drift-plan.json")
	err = driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Execute show-step command with non-existent step
	rootCmd.SetArgs([]string{"--plan", planFile, "--project", tmpDir, "show-step", "--step", "step-999"})
	err = rootCmd.Execute()

	// Should error because step doesn't exist
	assert.Error(t, err)
}
