//go:build unit

package driftadopt_test

import (
	"testing"
	"time"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDriftPlan_GetChunk(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalChunks: 3,
		Chunks: []driftadopt.DriftChunk{
			{ID: "chunk-001", Order: 0},
			{ID: "chunk-002", Order: 1},
			{ID: "chunk-003", Order: 2},
		},
	}

	// Act & Assert - Found
	chunk := plan.GetChunk("chunk-002")
	require.NotNil(t, chunk)
	assert.Equal(t, "chunk-002", chunk.ID)
	assert.Equal(t, 1, chunk.Order)

	// Act & Assert - Not found
	chunk = plan.GetChunk("chunk-999")
	assert.Nil(t, chunk)

	// Act & Assert - Empty plan
	emptyPlan := &driftadopt.DriftPlan{Chunks: []driftadopt.DriftChunk{}}
	chunk = emptyPlan.GetChunk("chunk-001")
	assert.Nil(t, chunk)
}

func TestDriftPlan_GetNextPendingChunk(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalChunks: 4,
		Chunks: []driftadopt.DriftChunk{
			{ID: "chunk-001", Order: 0, Status: driftadopt.ChunkCompleted},
			{ID: "chunk-002", Order: 1, Status: driftadopt.ChunkPending},
			{ID: "chunk-003", Order: 2, Status: driftadopt.ChunkPending},
			{ID: "chunk-004", Order: 3, Status: driftadopt.ChunkFailed},
		},
	}

	// Act
	chunk := plan.GetNextPendingChunk()

	// Assert - Returns first pending chunk
	require.NotNil(t, chunk)
	assert.Equal(t, "chunk-002", chunk.ID)
	assert.Equal(t, driftadopt.ChunkPending, chunk.Status)
}

func TestDriftPlan_GetNextPendingChunk_NoPending(t *testing.T) {
	// Arrange - All chunks completed or failed
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalChunks: 2,
		Chunks: []driftadopt.DriftChunk{
			{ID: "chunk-001", Order: 0, Status: driftadopt.ChunkCompleted},
			{ID: "chunk-002", Order: 1, Status: driftadopt.ChunkFailed},
		},
	}

	// Act
	chunk := plan.GetNextPendingChunk()

	// Assert - Returns nil when no pending chunks
	assert.Nil(t, chunk)
}

func TestDriftPlan_GetNextPendingChunk_EmptyPlan(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Chunks: []driftadopt.DriftChunk{},
	}

	// Act
	chunk := plan.GetNextPendingChunk()

	// Assert
	assert.Nil(t, chunk)
}

func TestDriftPlan_CountByStatus(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalChunks: 6,
		Chunks: []driftadopt.DriftChunk{
			{ID: "chunk-001", Status: driftadopt.ChunkCompleted},
			{ID: "chunk-002", Status: driftadopt.ChunkCompleted},
			{ID: "chunk-003", Status: driftadopt.ChunkCompleted},
			{ID: "chunk-004", Status: driftadopt.ChunkPending},
			{ID: "chunk-005", Status: driftadopt.ChunkFailed},
			{ID: "chunk-006", Status: driftadopt.ChunkSkipped},
		},
	}

	// Act
	counts := plan.CountByStatus()

	// Assert
	assert.Equal(t, 3, counts[driftadopt.ChunkCompleted])
	assert.Equal(t, 1, counts[driftadopt.ChunkPending])
	assert.Equal(t, 1, counts[driftadopt.ChunkFailed])
	assert.Equal(t, 1, counts[driftadopt.ChunkSkipped])
	assert.Equal(t, 0, counts[driftadopt.ChunkInProgress]) // None in progress
}

func TestDriftPlan_CountByStatus_Empty(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Chunks: []driftadopt.DriftChunk{},
	}

	// Act
	counts := plan.CountByStatus()

	// Assert - All counts should be zero
	assert.Equal(t, 0, counts[driftadopt.ChunkCompleted])
	assert.Equal(t, 0, counts[driftadopt.ChunkPending])
	assert.Equal(t, 0, counts[driftadopt.ChunkFailed])
	assert.Equal(t, 0, counts[driftadopt.ChunkSkipped])
	assert.Equal(t, 0, counts[driftadopt.ChunkInProgress])
}

func TestDriftPlan_GetFailedChunks(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalChunks: 5,
		Chunks: []driftadopt.DriftChunk{
			{ID: "chunk-001", Status: driftadopt.ChunkCompleted},
			{ID: "chunk-002", Status: driftadopt.ChunkFailed, LastError: "compilation error"},
			{ID: "chunk-003", Status: driftadopt.ChunkPending},
			{ID: "chunk-004", Status: driftadopt.ChunkFailed, LastError: "diff mismatch"},
			{ID: "chunk-005", Status: driftadopt.ChunkSkipped},
		},
	}

	// Act
	failed := plan.GetFailedChunks()

	// Assert
	require.Len(t, failed, 2)
	assert.Equal(t, "chunk-002", failed[0].ID)
	assert.Equal(t, "compilation error", failed[0].LastError)
	assert.Equal(t, "chunk-004", failed[1].ID)
	assert.Equal(t, "diff mismatch", failed[1].LastError)
}

func TestDriftPlan_GetFailedChunks_None(t *testing.T) {
	// Arrange - No failed chunks
	plan := &driftadopt.DriftPlan{
		Stack:       "dev",
		GeneratedAt: time.Now(),
		TotalChunks: 2,
		Chunks: []driftadopt.DriftChunk{
			{ID: "chunk-001", Status: driftadopt.ChunkCompleted},
			{ID: "chunk-002", Status: driftadopt.ChunkPending},
		},
	}

	// Act
	failed := plan.GetFailedChunks()

	// Assert - Empty slice, not nil
	assert.NotNil(t, failed)
	assert.Empty(t, failed)
}

func TestDriftPlan_GetFailedChunks_Empty(t *testing.T) {
	// Arrange
	plan := &driftadopt.DriftPlan{
		Chunks: []driftadopt.DriftChunk{},
	}

	// Act
	failed := plan.GetFailedChunks()

	// Assert
	assert.NotNil(t, failed)
	assert.Empty(t, failed)
}

func TestDriftPlan_HelperMethods_Integration(t *testing.T) {
	// Test that all helper methods work together correctly
	plan := &driftadopt.DriftPlan{
		Stack:       "production",
		GeneratedAt: time.Now(),
		TotalChunks: 5,
		Chunks: []driftadopt.DriftChunk{
			{ID: "chunk-001", Order: 0, Status: driftadopt.ChunkCompleted},
			{ID: "chunk-002", Order: 1, Status: driftadopt.ChunkPending},
			{ID: "chunk-003", Order: 2, Status: driftadopt.ChunkPending},
			{ID: "chunk-004", Order: 3, Status: driftadopt.ChunkFailed, LastError: "error 1"},
			{ID: "chunk-005", Order: 4, Status: driftadopt.ChunkSkipped},
		},
	}

	// Test GetChunk
	chunk := plan.GetChunk("chunk-003")
	require.NotNil(t, chunk)
	assert.Equal(t, driftadopt.ChunkPending, chunk.Status)

	// Test GetNextPendingChunk
	nextChunk := plan.GetNextPendingChunk()
	require.NotNil(t, nextChunk)
	assert.Equal(t, "chunk-002", nextChunk.ID)

	// Test CountByStatus
	counts := plan.CountByStatus()
	assert.Equal(t, 1, counts[driftadopt.ChunkCompleted])
	assert.Equal(t, 2, counts[driftadopt.ChunkPending])
	assert.Equal(t, 1, counts[driftadopt.ChunkFailed])
	assert.Equal(t, 1, counts[driftadopt.ChunkSkipped])

	// Test GetFailedChunks
	failed := plan.GetFailedChunks()
	require.Len(t, failed, 1)
	assert.Equal(t, "chunk-004", failed[0].ID)
}
