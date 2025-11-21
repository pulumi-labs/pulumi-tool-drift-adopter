package driftadopt

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// DiffRecorder records code changes for rollback capability
type DiffRecorder struct {
	diffsDir string
}

// DiffRecord represents a recorded code change
type DiffRecord struct {
	ID        string            `json:"id"`        // "001", "002", etc.
	ChunkID   string            `json:"chunkID"`   // Associated chunk
	Timestamp time.Time         `json:"timestamp"`
	Files     map[string]string `json:"files"`  // filepath -> original content
	Applied   bool              `json:"applied"` // Currently applied?
}

// NewDiffRecorder creates a new diff recorder
func NewDiffRecorder(diffsDir string) *DiffRecorder {
	os.MkdirAll(diffsDir, 0755)
	return &DiffRecorder{diffsDir: diffsDir}
}

// NextID generates the next sequential diff ID
func (r *DiffRecorder) NextID() string {
	diffs, _ := r.ListDiffs()
	return fmt.Sprintf("%03d", len(diffs)+1)
}

// RecordDiff records a diff to disk
func (r *DiffRecorder) RecordDiff(diff *DiffRecord) error {
	data, err := json.MarshalIndent(diff, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal diff: %w", err)
	}

	filePath := filepath.Join(r.diffsDir, fmt.Sprintf("%s.json", diff.ID))
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("write diff file: %w", err)
	}

	return nil
}

// GetDiff retrieves a diff by ID
func (r *DiffRecorder) GetDiff(diffID string) (*DiffRecord, error) {
	filePath := filepath.Join(r.diffsDir, fmt.Sprintf("%s.json", diffID))

	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("read diff file: %w", err)
	}

	var diff DiffRecord
	if err := json.Unmarshal(data, &diff); err != nil {
		return nil, fmt.Errorf("unmarshal diff: %w", err)
	}

	return &diff, nil
}

// ListDiffs returns all recorded diffs in order
func (r *DiffRecorder) ListDiffs() ([]*DiffRecord, error) {
	entries, err := os.ReadDir(r.diffsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read diffs directory: %w", err)
	}

	var diffs []*DiffRecord
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".json") {
			diffID := strings.TrimSuffix(entry.Name(), ".json")
			diff, err := r.GetDiff(diffID)
			if err != nil {
				return nil, err
			}
			diffs = append(diffs, diff)
		}
	}

	// Sort by ID
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].ID < diffs[j].ID
	})

	return diffs, nil
}

// Rollback restores files to their original state and marks diff as unapplied
func (r *DiffRecorder) Rollback(diffID string) error {
	diff, err := r.GetDiff(diffID)
	if err != nil {
		return fmt.Errorf("get diff: %w", err)
	}

	if !diff.Applied {
		return fmt.Errorf("diff %s not currently applied", diffID)
	}

	// Restore original files
	for filePath, originalContent := range diff.Files {
		if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
			return fmt.Errorf("restore file %s: %w", filePath, err)
		}
	}

	// Mark as unapplied
	diff.Applied = false
	return r.RecordDiff(diff)
}
