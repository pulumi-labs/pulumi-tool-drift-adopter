package driftadopt

import (
	"encoding/json"
	"fmt"
	"strings"
)

// PreviewParser parses Pulumi preview JSON output
type PreviewParser struct{}

// NewPreviewParser creates a new preview parser
func NewPreviewParser() *PreviewParser {
	return &PreviewParser{}
}

// pulumiPreview represents the structure of `pulumi preview --json` output
type pulumiPreview struct {
	Steps []pulumiStep `json:"steps"`
}

type pulumiStep struct {
	Op           string                 `json:"op"`
	URN          string                 `json:"urn"`
	Type         string                 `json:"type"`
	DetailedDiff map[string]diffDetail  `json:"detailedDiff"`
}

type diffDetail struct {
	Kind      string      `json:"kind"`       // "add", "delete", "update", "add-replace", "delete-replace", "update-replace"
	InputDiff bool        `json:"inputDiff"`  // true if this is an input property change
	LHS       interface{} `json:"lhs"`        // Left-hand side (old value)
	RHS       interface{} `json:"rhs"`        // Right-hand side (new value)
}

// ParseDiff parses `pulumi preview --json` output into structured diffs
func (p *PreviewParser) ParseDiff(jsonOutput string) ([]ResourceDiff, error) {
	// Parse JSON
	var preview pulumiPreview
	if err := json.Unmarshal([]byte(jsonOutput), &preview); err != nil {
		return nil, fmt.Errorf("unmarshal preview: %w", err)
	}

	var diffs []ResourceDiff

	// Convert each step to a ResourceDiff
	for _, step := range preview.Steps {
		diff := ResourceDiff{
			URN:          step.URN,
			Type:         step.Type,
			Name:         extractNameFromURN(step.URN),
			PropertyDiff: []PropChange{},
		}

		// Map operation to DiffType
		switch step.Op {
		case "create":
			// Skip creates - we're only interested in drift (updates/deletes/replaces)
			continue
		case "update":
			diff.DiffType = DiffTypeUpdate
		case "delete":
			diff.DiffType = DiffTypeDelete
		case "replace":
			diff.DiffType = DiffTypeReplace
		default:
			// Unknown operation, skip
			continue
		}

		// Parse detailed diffs into property changes
		for path, detail := range step.DetailedDiff {
			// Map kind to diffKind
			diffKind := strings.TrimSuffix(detail.Kind, "-replace") // remove -replace suffix if present

			change := PropChange{
				Path:     path,
				OldValue: detail.LHS,
				NewValue: detail.RHS,
				DiffKind: diffKind,
			}

			diff.PropertyDiff = append(diff.PropertyDiff, change)
		}

		diffs = append(diffs, diff)
	}

	return diffs, nil
}

// extractNameFromURN extracts the resource name from a URN
// URN format: urn:pulumi:stack::project::type::name
func extractNameFromURN(urn string) string {
	parts := strings.Split(urn, "::")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return ""
}
