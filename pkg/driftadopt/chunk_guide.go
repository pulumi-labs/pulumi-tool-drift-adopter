package driftadopt

import (
	"fmt"
	"os"
)

// ChunkGuide provides guidance to agents about chunks
type ChunkGuide struct {
	projectDir string
}

// ChunkInfo contains information about a chunk for agent consumption
type ChunkInfo struct {
	ChunkID         string
	Resources       []ResourceDiff
	CurrentCode     map[string]string // filepath -> code
	ExpectedChanges []string          // Human-readable descriptions
	Dependencies    []string
	Status          ChunkStatus
}

// NewChunkGuide creates a new chunk guide
func NewChunkGuide(projectDir string) *ChunkGuide {
	return &ChunkGuide{projectDir: projectDir}
}

// ShowChunk provides detailed information about a chunk
func (g *ChunkGuide) ShowChunk(plan *DriftPlan, chunkID string) (*ChunkInfo, error) {
	chunk := plan.GetChunk(chunkID)
	if chunk == nil {
		return nil, fmt.Errorf("chunk not found: %s", chunkID)
	}

	// Read current code for affected files
	currentCode := make(map[string]string)
	for _, res := range chunk.Resources {
		if res.SourceFile != "" {
			content, err := os.ReadFile(res.SourceFile)
			if err != nil {
				return nil, fmt.Errorf("read source file %s: %w", res.SourceFile, err)
			}
			currentCode[res.SourceFile] = string(content)
		}
	}

	// Format expected changes as human-readable descriptions
	var expectedChanges []string
	for _, res := range chunk.Resources {
		for _, prop := range res.PropertyDiff {
			expectedChanges = append(expectedChanges, g.FormatPropertyChange(prop))
		}
	}

	return &ChunkInfo{
		ChunkID:         chunk.ID,
		Resources:       chunk.Resources,
		CurrentCode:     currentCode,
		ExpectedChanges: expectedChanges,
		Dependencies:    chunk.Dependencies,
		Status:          chunk.Status,
	}, nil
}

// FormatPropertyChange formats a property change as a human-readable string
func (g *ChunkGuide) FormatPropertyChange(prop PropChange) string {
	switch prop.DiffKind {
	case "add":
		return fmt.Sprintf("Add %s = %v", prop.Path, prop.NewValue)
	case "delete":
		return fmt.Sprintf("Delete %s (was: %v)", prop.Path, prop.OldValue)
	case "update":
		return fmt.Sprintf("Update %s: %v => %v", prop.Path, prop.OldValue, prop.NewValue)
	default:
		return fmt.Sprintf("%s %s: %v => %v", prop.DiffKind, prop.Path, prop.OldValue, prop.NewValue)
	}
}
