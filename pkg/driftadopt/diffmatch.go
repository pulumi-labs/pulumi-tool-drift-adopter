package driftadopt

import (
	"fmt"
	"strconv"
)

// DiffMatcher compares expected vs actual preview diffs
type DiffMatcher struct{}

// MatchResult contains the result of diff matching
type MatchResult struct {
	Matches           bool
	MissingChanges    []PropChange // Expected but not in actual
	UnexpectedChanges []PropChange // In actual but not expected
	MatchedResources  []string     // URNs that matched
}

// NewDiffMatcher creates a new diff matcher
func NewDiffMatcher() *DiffMatcher {
	return &DiffMatcher{}
}

// Matches compares expected diffs with actual diffs from preview
func (m *DiffMatcher) Matches(expected, actual []ResourceDiff) *MatchResult {
	result := &MatchResult{
		Matches:           true,
		MissingChanges:    []PropChange{},
		UnexpectedChanges: []PropChange{},
		MatchedResources:  []string{},
	}

	// Build a map of actual resources by URN for quick lookup
	actualByURN := make(map[string]*ResourceDiff)
	for i := range actual {
		actualByURN[actual[i].URN] = &actual[i]
	}

	// Check each expected resource
	for _, expectedRes := range expected {
		actualRes, found := actualByURN[expectedRes.URN]
		if !found {
			// Resource not found in actual - all changes are missing
			result.Matches = false
			for _, prop := range expectedRes.PropertyDiff {
				result.MissingChanges = append(result.MissingChanges, prop)
			}
			continue
		}

		// Compare property diffs
		missing, unexpected := m.comparePropertyDiffs(expectedRes.PropertyDiff, actualRes.PropertyDiff)

		if len(missing) > 0 || len(unexpected) > 0 {
			result.Matches = false
			result.MissingChanges = append(result.MissingChanges, missing...)
			result.UnexpectedChanges = append(result.UnexpectedChanges, unexpected...)
		} else {
			result.MatchedResources = append(result.MatchedResources, expectedRes.URN)
		}
	}

	return result
}

// comparePropertyDiffs compares expected and actual property changes
func (m *DiffMatcher) comparePropertyDiffs(expected, actual []PropChange) (missing, unexpected []PropChange) {
	// Build a map of actual changes by path
	actualByPath := make(map[string]*PropChange)
	for i := range actual {
		actualByPath[actual[i].Path] = &actual[i]
	}

	// Check for missing changes
	for _, expectedProp := range expected {
		actualProp, found := actualByPath[expectedProp.Path]
		if !found {
			missing = append(missing, expectedProp)
			continue
		}

		// Check if the change matches
		if !m.propertyMatches(expectedProp, *actualProp) {
			missing = append(missing, expectedProp)
		}

		// Mark as checked
		delete(actualByPath, expectedProp.Path)
	}

	// Remaining actual changes are unexpected
	for _, actualProp := range actualByPath {
		unexpected = append(unexpected, *actualProp)
	}

	return missing, unexpected
}

// propertyMatches checks if two property changes match
func (m *DiffMatcher) propertyMatches(expected, actual PropChange) bool {
	// Path must match exactly
	if expected.Path != actual.Path {
		return false
	}

	// DiffKind must match
	if expected.DiffKind != actual.DiffKind {
		return false
	}

	// Values must match (with fuzzy matching)
	if !m.valuesEqual(expected.OldValue, actual.OldValue) {
		return false
	}

	if !m.valuesEqual(expected.NewValue, actual.NewValue) {
		return false
	}

	return true
}

// valuesEqual compares two values with fuzzy matching
func (m *DiffMatcher) valuesEqual(a, b interface{}) bool {
	// Direct equality
	if a == b {
		return true
	}

	// Both nil
	if a == nil && b == nil {
		return true
	}

	// One nil, one not
	if (a == nil) != (b == nil) {
		return false
	}

	// Try to convert both to strings and compare
	aStr := fmt.Sprintf("%v", a)
	bStr := fmt.Sprintf("%v", b)

	if aStr == bStr {
		return true
	}

	// Try bool conversion
	if aBool, aOk := toBool(a); aOk {
		if bBool, bOk := toBool(b); bOk {
			return aBool == bBool
		}
	}

	// Try numeric conversion
	if aNum, aOk := toFloat(a); aOk {
		if bNum, bOk := toFloat(b); bOk {
			return aNum == bNum
		}
	}

	return false
}

// toBool attempts to convert a value to bool
func toBool(v interface{}) (bool, bool) {
	switch val := v.(type) {
	case bool:
		return val, true
	case string:
		if val == "true" {
			return true, true
		}
		if val == "false" {
			return false, true
		}
	}
	return false, false
}

// toFloat attempts to convert a value to float64
func toFloat(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	case string:
		if f, err := strconv.ParseFloat(val, 64); err == nil {
			return f, true
		}
	}
	return 0, false
}
