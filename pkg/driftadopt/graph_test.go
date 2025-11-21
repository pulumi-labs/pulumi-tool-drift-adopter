//go:build unit

package driftadopt_test

import (
	"testing"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGraph_FromStateFile(t *testing.T) {
	// Arrange - Load fixture state file
	stateData := testutil.LoadFixture(t, "testdata/states/simple-state.json")

	// Act
	graph, err := driftadopt.BuildGraphFromState(stateData)
	require.NoError(t, err)

	// Assert - Should have 3 custom resources (excluding Stack)
	assert.Len(t, graph.Nodes, 3)

	// Verify bucket-a node (leaf - no dependencies)
	bucketURN := "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-a"
	node := graph.Nodes[bucketURN]
	require.NotNil(t, node)
	assert.Equal(t, bucketURN, node.URN)
	assert.Equal(t, "aws:s3/bucket:Bucket", node.Type)
	assert.Empty(t, node.Dependencies, "bucket-a should have no dependencies")
	assert.Len(t, node.Dependents, 2, "bucket-a should have 2 dependents")

	// Verify object-b node (depends on bucket-a)
	objectBURN := "urn:pulumi:dev::test::aws:s3/bucketObject:BucketObject::object-b"
	nodeB := graph.Nodes[objectBURN]
	require.NotNil(t, nodeB)
	assert.Equal(t, "aws:s3/bucketObject:BucketObject", nodeB.Type)
	assert.Len(t, nodeB.Dependencies, 1)
	assert.Contains(t, nodeB.Dependencies, bucketURN)
	assert.Empty(t, nodeB.Dependents, "object-b should have no dependents")
}

func TestGraph_TopologicalSort(t *testing.T) {
	// Arrange - Create DAG
	// bucket-a (no dependencies, leaf)
	// object-b depends on bucket-a
	// object-c depends on bucket-a
	stateData := testutil.LoadFixture(t, "testdata/states/simple-state.json")
	graph, err := driftadopt.BuildGraphFromState(stateData)
	require.NoError(t, err)

	// Act
	sorted, err := graph.TopologicalSort()
	require.NoError(t, err)

	// Assert - Leaves come first (resources with no dependencies)
	assert.Len(t, sorted, 3)

	// First node should be bucket-a (leaf)
	assert.Equal(t, "urn:pulumi:dev::test::aws:s3/bucket:Bucket::bucket-a", sorted[0].URN)
	assert.Empty(t, sorted[0].Dependencies)

	// Next two should be the objects (order doesn't matter between them)
	objectURNs := []string{sorted[1].URN, sorted[2].URN}
	assert.Contains(t, objectURNs, "urn:pulumi:dev::test::aws:s3/bucketObject:BucketObject::object-b")
	assert.Contains(t, objectURNs, "urn:pulumi:dev::test::aws:s3/bucketObject:BucketObject::object-c")
}

func TestGraph_CycleDetection(t *testing.T) {
	// Arrange - Create cycle: A -> C -> B -> A
	stateData := testutil.LoadFixture(t, "testdata/states/cycle-state.json")
	graph, err := driftadopt.BuildGraphFromState(stateData)
	require.NoError(t, err)

	// Act
	_, err = graph.TopologicalSort()

	// Assert - Should detect cycle
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cycle")
}

func TestGraph_MultipleLeaves(t *testing.T) {
	// Arrange - Create graph with multiple leaves
	// We'll build this manually for precise control
	graph := &driftadopt.Graph{
		Nodes: map[string]*driftadopt.Node{
			"urn:A": {
				URN:          "urn:A",
				Type:         "type:A",
				Dependencies: []string{},
				Dependents:   []string{"urn:C"},
			},
			"urn:B": {
				URN:          "urn:B",
				Type:         "type:B",
				Dependencies: []string{},
				Dependents:   []string{"urn:C"},
			},
			"urn:C": {
				URN:          "urn:C",
				Type:         "type:C",
				Dependencies: []string{"urn:A", "urn:B"},
				Dependents:   []string{},
			},
		},
		Edges: map[string][]string{
			"urn:A": {"urn:C"},
			"urn:B": {"urn:C"},
		},
	}

	// Act
	sorted, err := graph.TopologicalSort()
	require.NoError(t, err)

	// Assert - Both leaves come before C
	assert.Len(t, sorted, 3)

	// First two should be A and B (order doesn't matter)
	leafURNs := []string{sorted[0].URN, sorted[1].URN}
	assert.Contains(t, leafURNs, "urn:A")
	assert.Contains(t, leafURNs, "urn:B")

	// Last should be C
	assert.Equal(t, "urn:C", sorted[2].URN)
}

func TestGraph_EmptyState(t *testing.T) {
	// Arrange - Empty deployment
	stateJSON := []byte(`{
		"version": 3,
		"deployment": {
			"manifest": {},
			"resources": []
		}
	}`)

	// Act
	graph, err := driftadopt.BuildGraphFromState(stateJSON)
	require.NoError(t, err)

	// Assert
	assert.NotNil(t, graph)
	assert.Empty(t, graph.Nodes)
}

func TestGraph_SingleResource(t *testing.T) {
	// Arrange - Single resource with no dependencies
	stateJSON := []byte(`{
		"version": 3,
		"deployment": {
			"manifest": {},
			"resources": [{
				"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::solo",
				"custom": true,
				"type": "aws:s3/bucket:Bucket",
				"dependencies": []
			}]
		}
	}`)

	// Act
	graph, err := driftadopt.BuildGraphFromState(stateJSON)
	require.NoError(t, err)

	// Assert
	assert.Len(t, graph.Nodes, 1)

	sorted, err := graph.TopologicalSort()
	require.NoError(t, err)
	assert.Len(t, sorted, 1)
	assert.Equal(t, "urn:pulumi:dev::test::aws:s3/bucket:Bucket::solo", sorted[0].URN)
}

func TestGraph_InvalidJSON(t *testing.T) {
	// Arrange
	invalidJSON := []byte("not json")

	// Act
	_, err := driftadopt.BuildGraphFromState(invalidJSON)

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}

func TestGraph_LinearChain(t *testing.T) {
	// Arrange - Linear dependency chain: A -> B -> C
	stateJSON := []byte(`{
		"version": 3,
		"deployment": {
			"manifest": {},
			"resources": [
				{
					"urn": "urn:A",
					"custom": true,
					"type": "type:A",
					"dependencies": []
				},
				{
					"urn": "urn:B",
					"custom": true,
					"type": "type:B",
					"dependencies": ["urn:A"]
				},
				{
					"urn": "urn:C",
					"custom": true,
					"type": "type:C",
					"dependencies": ["urn:B"]
				}
			]
		}
	}`)

	// Act
	graph, err := driftadopt.BuildGraphFromState(stateJSON)
	require.NoError(t, err)

	sorted, err := graph.TopologicalSort()
	require.NoError(t, err)

	// Assert - Should be in order A, B, C
	assert.Len(t, sorted, 3)
	assert.Equal(t, "urn:A", sorted[0].URN)
	assert.Equal(t, "urn:B", sorted[1].URN)
	assert.Equal(t, "urn:C", sorted[2].URN)
}

func TestGraph_StackResourcesFiltered(t *testing.T) {
	// Arrange - State with Stack resource that should be filtered
	stateJSON := []byte(`{
		"version": 3,
		"deployment": {
			"manifest": {},
			"resources": [
				{
					"urn": "urn:pulumi:dev::test::pulumi:pulumi:Stack::test-dev",
					"custom": false,
					"type": "pulumi:pulumi:Stack"
				},
				{
					"urn": "urn:pulumi:dev::test::aws:s3/bucket:Bucket::mybucket",
					"custom": true,
					"type": "aws:s3/bucket:Bucket",
					"dependencies": []
				}
			]
		}
	}`)

	// Act
	graph, err := driftadopt.BuildGraphFromState(stateJSON)
	require.NoError(t, err)

	// Assert - Stack resource should be filtered out
	assert.Len(t, graph.Nodes, 1)
	_, hasStack := graph.Nodes["urn:pulumi:dev::test::pulumi:pulumi:Stack::test-dev"]
	assert.False(t, hasStack, "Stack resource should be filtered")
}
