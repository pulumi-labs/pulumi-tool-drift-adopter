//go:build e2e

package e2e

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDriftSimple tests the simple drift scenario end-to-end
func TestDriftSimple(t *testing.T) {
	fixtureDir := "../testdata/drift-simple"

	// Load preview output
	previewData, err := os.ReadFile(filepath.Join(fixtureDir, "preview.json"))
	require.NoError(t, err)

	var preview []driftadopt.ResourceDiff
	err = json.Unmarshal(previewData, &preview)
	require.NoError(t, err)

	// Verify preview has drift
	require.Len(t, preview, 1, "Should have 1 resource with drift")
	assert.Equal(t, "my-bucket", preview[0].Name)
	assert.Equal(t, driftadopt.DiffTypeUpdate, preview[0].DiffType)
	assert.Len(t, preview[0].PropertyDiff, 2, "Should have 2 property changes")

	// Create drift plan
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalSteps:  1,
		Steps: []driftadopt.DriftStep{
			{
				ID:           "step-001",
				Order:        0,
				Resources:    preview,
				Status:       driftadopt.StepPending,
				Dependencies: []string{},
			},
		},
	}

	// Verify plan structure
	assert.Equal(t, 1, plan.TotalSteps)
	assert.Equal(t, driftadopt.StepPending, plan.Steps[0].Status)

	// Verify next step is available
	nextStep := plan.GetNextPendingStep()
	require.NotNil(t, nextStep)
	assert.Equal(t, "step-001", nextStep.ID)
}

// TestDriftDependencies tests multi-resource drift with dependencies
func TestDriftDependencies(t *testing.T) {
	fixtureDir := "../testdata/drift-dependencies"

	// Load preview output
	previewData, err := os.ReadFile(filepath.Join(fixtureDir, "preview.json"))
	require.NoError(t, err)

	var preview []driftadopt.ResourceDiff
	err = json.Unmarshal(previewData, &preview)
	require.NoError(t, err)

	// Verify all resources have drift
	require.Len(t, preview, 3, "Should have 3 resources with drift")

	// Load state to build dependency graph
	stateData, err := os.ReadFile(filepath.Join(fixtureDir, "state.json"))
	require.NoError(t, err)

	// Build dependency graph
	graph, err := driftadopt.BuildGraphFromState(stateData)
	require.NoError(t, err)

	// Get topological order
	nodes, err := graph.TopologicalSort()
	require.NoError(t, err)

	// Verify VPC comes before Subnet and SecurityGroup
	vpcIndex := -1
	subnetIndex := -1
	sgIndex := -1

	for i, node := range nodes {
		if contains(node.URN, "vpc:Vpc") {
			vpcIndex = i
		} else if contains(node.URN, "subnet:Subnet") {
			subnetIndex = i
		} else if contains(node.URN, "securityGroup:SecurityGroup") {
			sgIndex = i
		}
	}

	assert.True(t, vpcIndex < subnetIndex, "VPC should come before Subnet")
	assert.True(t, vpcIndex < sgIndex, "VPC should come before SecurityGroup")

	// Create steps based on dependency levels
	steps := createStepsFromDependencies(preview, graph)
	assert.Len(t, steps, 2, "Should have 2 steps")

	// Verify first step has VPC
	assert.Len(t, steps[0].Resources, 1, "First step should have 1 resource")
	assert.Equal(t, "main-vpc", steps[0].Resources[0].Name)

	// Verify second step has Subnet and SecurityGroup
	assert.Len(t, steps[1].Resources, 2, "Second step should have 2 resources")
	assert.Equal(t, []string{"step-001"}, steps[1].Dependencies)
}

// TestDriftDeletion tests resource deletion scenario
func TestDriftDeletion(t *testing.T) {
	fixtureDir := "../testdata/drift-deletion"

	// Load preview output
	previewData, err := os.ReadFile(filepath.Join(fixtureDir, "preview.json"))
	require.NoError(t, err)

	var preview []driftadopt.ResourceDiff
	err = json.Unmarshal(previewData, &preview)
	require.NoError(t, err)

	// Verify deletion drift
	require.Len(t, preview, 1, "Should have 1 resource with drift")
	assert.Equal(t, "deleted-bucket", preview[0].Name)
	assert.Equal(t, driftadopt.DiffTypeDelete, preview[0].DiffType)
	assert.Empty(t, preview[0].PropertyDiff, "Delete should have no property changes")

	// Verify plan can be created for deletion
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalSteps:  1,
		Steps: []driftadopt.DriftStep{
			{
				ID:           "step-001",
				Order:        0,
				Resources:    preview,
				Status:       driftadopt.StepPending,
				Dependencies: []string{},
			},
		},
	}

	assert.Equal(t, 1, plan.TotalSteps)
	assert.Equal(t, driftadopt.DiffTypeDelete, plan.Steps[0].Resources[0].DiffType)
}

// TestDriftReplacement tests resource replacement scenario
func TestDriftReplacement(t *testing.T) {
	fixtureDir := "../testdata/drift-replacement"

	// Load preview output
	previewData, err := os.ReadFile(filepath.Join(fixtureDir, "preview.json"))
	require.NoError(t, err)

	var preview []driftadopt.ResourceDiff
	err = json.Unmarshal(previewData, &preview)
	require.NoError(t, err)

	// Verify replacement drift
	require.Len(t, preview, 1, "Should have 1 resource with drift")
	assert.Equal(t, "my-bucket", preview[0].Name)
	assert.Equal(t, driftadopt.DiffTypeReplace, preview[0].DiffType)
	assert.Len(t, preview[0].PropertyDiff, 1, "Should have 1 property change")
	assert.Equal(t, "bucket", preview[0].PropertyDiff[0].Path)
}

// TestStepGuide tests the step guide functionality
func TestStepGuide(t *testing.T) {
	fixtureDir := "../testdata/drift-simple"

	// Load expected plan
	planData, err := os.ReadFile(filepath.Join(fixtureDir, "expected-plan.json"))
	require.NoError(t, err)

	var plan driftadopt.DriftPlan
	err = json.Unmarshal(planData, &plan)
	require.NoError(t, err)

	// Update SourceFile to absolute path for testing
	absFixtureDir, _ := filepath.Abs(fixtureDir)
	for i := range plan.Steps[0].Resources {
		if plan.Steps[0].Resources[i].SourceFile != "" {
			plan.Steps[0].Resources[i].SourceFile = filepath.Join(absFixtureDir, plan.Steps[0].Resources[i].SourceFile)
		}
	}

	// Create step guide
	guide := driftadopt.NewStepGuide(absFixtureDir)

	// Get step info
	info, err := guide.ShowStep(&plan, "step-001")
	require.NoError(t, err)

	// Verify step info
	assert.Equal(t, "step-001", info.StepID)
	assert.Equal(t, driftadopt.StepPending, info.Status)
	assert.Len(t, info.Resources, 1)
	assert.Equal(t, "my-bucket", info.Resources[0].Name)

	// Verify expected changes are formatted
	assert.NotEmpty(t, info.ExpectedChanges)

	// Verify current code was read
	assert.NotEmpty(t, info.CurrentCode)
}

// TestSkipFunctionality tests skipping a step
func TestSkipFunctionality(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "e2e-skip-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a plan
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalSteps:  2,
		Steps: []driftadopt.DriftStep{
			{
				ID:     "step-001",
				Order:  0,
				Status: driftadopt.StepPending,
				Resources: []driftadopt.ResourceDiff{
					{URN: "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket1", Name: "bucket1", Type: "aws:s3/bucket:Bucket"},
				},
			},
			{
				ID:     "step-002",
				Order:  1,
				Status: driftadopt.StepPending,
				Resources: []driftadopt.ResourceDiff{
					{URN: "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket2", Name: "bucket2", Type: "aws:s3/bucket:Bucket"},
				},
				Dependencies: []string{"step-001"},
			},
		},
	}

	// Write plan
	planFile := filepath.Join(tmpDir, "drift-plan.json")
	err = driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Skip first step
	plan.Steps[0].Status = driftadopt.StepSkipped
	err = driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Read plan back
	loadedPlan, err := driftadopt.ReadPlanFile(planFile)
	require.NoError(t, err)

	// Verify skip persisted
	assert.Equal(t, driftadopt.StepSkipped, loadedPlan.Steps[0].Status)

	// Next pending step should be step-002
	nextStep := loadedPlan.GetNextPendingStep()
	require.NotNil(t, nextStep)
	assert.Equal(t, "step-002", nextStep.ID)
}

// TestFailureRecovery tests handling of failed steps
func TestFailureRecovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "e2e-failure-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a plan with a failed step
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalSteps:  1,
		Steps: []driftadopt.DriftStep{
			{
				ID:        "step-001",
				Order:     0,
				Status:    driftadopt.StepFailed,
				LastError: "Compilation failed",
				Attempt:   1,
				Resources: []driftadopt.ResourceDiff{
					{URN: "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket1", Name: "bucket1", Type: "aws:s3/bucket:Bucket"},
				},
			},
		},
	}

	// Write plan
	planFile := filepath.Join(tmpDir, "drift-plan.json")
	err = driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Get failed steps
	failedSteps := plan.GetFailedSteps()
	assert.Len(t, failedSteps, 1)
	assert.Equal(t, "step-001", failedSteps[0].ID)
	assert.Equal(t, "Compilation failed", failedSteps[0].LastError)

	// Retry step
	plan.Steps[0].Status = driftadopt.StepPending
	plan.Steps[0].Attempt++
	plan.Steps[0].LastError = ""

	err = driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Verify retry was recorded
	loadedPlan, err := driftadopt.ReadPlanFile(planFile)
	require.NoError(t, err)
	assert.Equal(t, driftadopt.StepPending, loadedPlan.Steps[0].Status)
	assert.Equal(t, 2, loadedPlan.Steps[0].Attempt)
}

// Helper functions

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[len(s)-len(substr):] == substr ||
		(len(s) > len(substr) && s[0:len(substr)] == substr) ||
		(len(s) > len(substr) && findSubstring(s, substr))
}

func findSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func createStepsFromDependencies(resources []driftadopt.ResourceDiff, graph *driftadopt.Graph) []driftadopt.DriftStep {
	// Calculate dependency depth for each resource
	depths := make(map[string]int)
	calculateDepths(graph, depths)

	// Group resources by depth
	levels := make(map[int][]driftadopt.ResourceDiff)
	maxDepth := 0
	for _, res := range resources {
		depth := depths[res.URN]
		levels[depth] = append(levels[depth], res)
		if depth > maxDepth {
			maxDepth = depth
		}
	}

	// Create steps
	var steps []driftadopt.DriftStep
	for i := 0; i <= maxDepth; i++ {
		if len(levels[i]) == 0 {
			continue
		}

		stepID := "step-" + padInt(len(steps)+1, 3)
		step := driftadopt.DriftStep{
			ID:        stepID,
			Order:     len(steps),
			Resources: levels[i],
			Status:    driftadopt.StepPending,
		}

		if len(steps) > 0 {
			step.Dependencies = []string{"step-" + padInt(len(steps), 3)}
		}

		steps = append(steps, step)
	}

	return steps
}

// calculateDepths calculates the maximum dependency depth for each node
func calculateDepths(graph *driftadopt.Graph, depths map[string]int) {
	// Initialize all to 0
	for urn := range graph.Nodes {
		depths[urn] = 0
	}

	// Iterate until no changes (simple approach)
	changed := true
	for changed {
		changed = false
		for urn, node := range graph.Nodes {
			for _, depURN := range node.Dependencies {
				if depths[urn] <= depths[depURN] {
					depths[urn] = depths[depURN] + 1
					changed = true
				}
			}
		}
	}
}

func padInt(n, width int) string {
	s := ""
	for i := 0; i < width; i++ {
		s = "0" + s
	}
	// Simple padding - just for test
	if n < 10 {
		return s[:width-1] + string(rune('0'+n))
	} else if n < 100 {
		return s[:width-2] + string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return s
}
