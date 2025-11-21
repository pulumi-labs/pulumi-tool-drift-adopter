package driftadopt

// GetChunk returns the chunk with the given ID, or nil if not found
func (p *DriftPlan) GetChunk(id string) *DriftChunk {
	for i := range p.Chunks {
		if p.Chunks[i].ID == id {
			return &p.Chunks[i]
		}
	}
	return nil
}

// GetNextPendingChunk returns the first chunk with status "pending",
// or nil if no pending chunks exist
func (p *DriftPlan) GetNextPendingChunk() *DriftChunk {
	for i := range p.Chunks {
		if p.Chunks[i].Status == ChunkPending {
			return &p.Chunks[i]
		}
	}
	return nil
}

// CountByStatus returns a map of chunk status to count
func (p *DriftPlan) CountByStatus() map[ChunkStatus]int {
	counts := make(map[ChunkStatus]int)

	for _, chunk := range p.Chunks {
		counts[chunk.Status]++
	}

	return counts
}

// GetFailedChunks returns all chunks with status "failed"
func (p *DriftPlan) GetFailedChunks() []*DriftChunk {
	var failed []*DriftChunk

	for i := range p.Chunks {
		if p.Chunks[i].Status == ChunkFailed {
			failed = append(failed, &p.Chunks[i])
		}
	}

	// Return empty slice instead of nil for consistency
	if failed == nil {
		return []*DriftChunk{}
	}

	return failed
}
