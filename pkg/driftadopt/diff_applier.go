package driftadopt

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DiffApplier applies agent-submitted code changes
type DiffApplier struct {
	projectDir string
	recorder   *DiffRecorder
}

// FileChange represents a code change to a single file
type FileChange struct {
	FilePath string
	NewCode  string
}

// NewDiffApplier creates a new diff applier
func NewDiffApplier(projectDir string) *DiffApplier {
	return &DiffApplier{
		projectDir: projectDir,
		recorder:   NewDiffRecorder(filepath.Join(projectDir, "code-diffs")),
	}
}

// GetRecorder returns the diff recorder for testing
func (d *DiffApplier) GetRecorder() *DiffRecorder {
	return d.recorder
}

// ApplyChanges applies code changes and records them for rollback
func (d *DiffApplier) ApplyChanges(stepID string, changes []FileChange) (string, error) {
	// 1. Record original state
	originalFiles := make(map[string]string)
	for _, change := range changes {
		content, err := os.ReadFile(change.FilePath)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", change.FilePath, err)
		}
		originalFiles[change.FilePath] = string(content)
	}

	// 2. Generate diff ID
	diffID := d.recorder.NextID()

	// 3. Record diff
	diffRecord := &DiffRecord{
		ID:        diffID,
		StepID:   stepID,
		Timestamp: time.Now(),
		Files:     originalFiles,
		Applied:   true,
	}

	if err := d.recorder.RecordDiff(diffRecord); err != nil {
		return "", fmt.Errorf("record diff: %w", err)
	}

	// 4. Apply changes
	for _, change := range changes {
		if err := os.WriteFile(change.FilePath, []byte(change.NewCode), 0644); err != nil {
			// Rollback on error
			d.recorder.Rollback(diffID)
			return "", fmt.Errorf("write file %s: %w", change.FilePath, err)
		}
	}

	return diffID, nil
}
