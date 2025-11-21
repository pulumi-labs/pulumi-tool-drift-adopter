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
		TotalChunks: 1,
		Chunks: []driftadopt.DriftChunk{
			{
				ID:           "chunk-001",
				Order:        0,
				Resources:    preview,
				Status:       driftadopt.ChunkPending,
				Dependencies: []string{},
			},
		},
	}

	// Verify plan structure
	assert.Equal(t, 1, plan.TotalChunks)
	assert.Equal(t, driftadopt.ChunkPending, plan.Chunks[0].Status)

	// Verify next chunk is available
	nextChunk := plan.GetNextPendingChunk()
	require.NotNil(t, nextChunk)
	assert.Equal(t, "chunk-001", nextChunk.ID)
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

	// Create chunks based on dependency levels
	chunks := createChunksFromDependencies(preview, graph)
	assert.Len(t, chunks, 2, "Should have 2 chunks")

	// Verify first chunk has VPC
	assert.Len(t, chunks[0].Resources, 1, "First chunk should have 1 resource")
	assert.Equal(t, "main-vpc", chunks[0].Resources[0].Name)

	// Verify second chunk has Subnet and SecurityGroup
	assert.Len(t, chunks[1].Resources, 2, "Second chunk should have 2 resources")
	assert.Equal(t, []string{"chunk-001"}, chunks[1].Dependencies)
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
		TotalChunks: 1,
		Chunks: []driftadopt.DriftChunk{
			{
				ID:           "chunk-001",
				Order:        0,
				Resources:    preview,
				Status:       driftadopt.ChunkPending,
				Dependencies: []string{},
			},
		},
	}

	assert.Equal(t, 1, plan.TotalChunks)
	assert.Equal(t, driftadopt.DiffTypeDelete, plan.Chunks[0].Resources[0].DiffType)
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

// TestChunkGuide tests the chunk guide functionality
func TestChunkGuide(t *testing.T) {
	fixtureDir := "../testdata/drift-simple"

	// Load expected plan
	planData, err := os.ReadFile(filepath.Join(fixtureDir, "expected-plan.json"))
	require.NoError(t, err)

	var plan driftadopt.DriftPlan
	err = json.Unmarshal(planData, &plan)
	require.NoError(t, err)

	// Update SourceFile to absolute path for testing
	absFixtureDir, _ := filepath.Abs(fixtureDir)
	for i := range plan.Chunks[0].Resources {
		if plan.Chunks[0].Resources[i].SourceFile != "" {
			plan.Chunks[0].Resources[i].SourceFile = filepath.Join(absFixtureDir, plan.Chunks[0].Resources[i].SourceFile)
		}
	}

	// Create chunk guide
	guide := driftadopt.NewChunkGuide(absFixtureDir)

	// Get chunk info
	info, err := guide.ShowChunk(&plan, "chunk-001")
	require.NoError(t, err)

	// Verify chunk info
	assert.Equal(t, "chunk-001", info.ChunkID)
	assert.Equal(t, driftadopt.ChunkPending, info.Status)
	assert.Len(t, info.Resources, 1)
	assert.Equal(t, "my-bucket", info.Resources[0].Name)

	// Verify expected changes are formatted
	assert.NotEmpty(t, info.ExpectedChanges)

	// Verify current code was read
	assert.NotEmpty(t, info.CurrentCode)
}

// TestSkipFunctionality tests skipping a chunk
func TestSkipFunctionality(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "e2e-skip-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a plan
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalChunks: 2,
		Chunks: []driftadopt.DriftChunk{
			{
				ID:     "chunk-001",
				Order:  0,
				Status: driftadopt.ChunkPending,
				Resources: []driftadopt.ResourceDiff{
					{URN: "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket1", Name: "bucket1", Type: "aws:s3/bucket:Bucket"},
				},
			},
			{
				ID:     "chunk-002",
				Order:  1,
				Status: driftadopt.ChunkPending,
				Resources: []driftadopt.ResourceDiff{
					{URN: "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket2", Name: "bucket2", Type: "aws:s3/bucket:Bucket"},
				},
				Dependencies: []string{"chunk-001"},
			},
		},
	}

	// Write plan
	planFile := filepath.Join(tmpDir, "drift-plan.json")
	err = driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Skip first chunk
	plan.Chunks[0].Status = driftadopt.ChunkSkipped
	err = driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Read plan back
	loadedPlan, err := driftadopt.ReadPlanFile(planFile)
	require.NoError(t, err)

	// Verify skip persisted
	assert.Equal(t, driftadopt.ChunkSkipped, loadedPlan.Chunks[0].Status)

	// Next pending chunk should be chunk-002
	nextChunk := loadedPlan.GetNextPendingChunk()
	require.NotNil(t, nextChunk)
	assert.Equal(t, "chunk-002", nextChunk.ID)
}

// TestFailureRecovery tests handling of failed chunks
func TestFailureRecovery(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "e2e-failure-*")
	require.NoError(t, err)
	defer os.RemoveAll(tmpDir)

	// Create a plan with a failed chunk
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalChunks: 1,
		Chunks: []driftadopt.DriftChunk{
			{
				ID:        "chunk-001",
				Order:     0,
				Status:    driftadopt.ChunkFailed,
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

	// Get failed chunks
	failedChunks := plan.GetFailedChunks()
	assert.Len(t, failedChunks, 1)
	assert.Equal(t, "chunk-001", failedChunks[0].ID)
	assert.Equal(t, "Compilation failed", failedChunks[0].LastError)

	// Retry chunk
	plan.Chunks[0].Status = driftadopt.ChunkPending
	plan.Chunks[0].Attempt++
	plan.Chunks[0].LastError = ""

	err = driftadopt.WritePlanFile(planFile, plan)
	require.NoError(t, err)

	// Verify retry was recorded
	loadedPlan, err := driftadopt.ReadPlanFile(planFile)
	require.NoError(t, err)
	assert.Equal(t, driftadopt.ChunkPending, loadedPlan.Chunks[0].Status)
	assert.Equal(t, 2, loadedPlan.Chunks[0].Attempt)
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

func createChunksFromDependencies(resources []driftadopt.ResourceDiff, graph *driftadopt.Graph) []driftadopt.DriftChunk {
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

	// Create chunks
	var chunks []driftadopt.DriftChunk
	for i := 0; i <= maxDepth; i++ {
		if len(levels[i]) == 0 {
			continue
		}

		chunkID := "chunk-" + padInt(len(chunks)+1, 3)
		chunk := driftadopt.DriftChunk{
			ID:        chunkID,
			Order:     len(chunks),
			Resources: levels[i],
			Status:    driftadopt.ChunkPending,
		}

		if len(chunks) > 0 {
			chunk.Dependencies = []string{"chunk-" + padInt(len(chunks), 3)}
		}

		chunks = append(chunks, chunk)
	}

	return chunks
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
