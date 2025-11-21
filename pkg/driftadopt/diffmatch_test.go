//go:build unit

package driftadopt_test

import (
	"testing"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/stretchr/testify/assert"
)

func TestDiffMatcher_ExactMatch(t *testing.T) {
	// Arrange
	expected := []driftadopt.ResourceDiff{
		{
			URN:      "urn:pulumi:dev::app::aws:s3/bucket:Bucket::my-bucket",
			Type:     "aws:s3/bucket:Bucket",
			Name:     "my-bucket",
			DiffType: driftadopt.DiffTypeUpdate,
			PropertyDiff: []driftadopt.PropChange{
				{
					Path:     "tags.Environment",
					OldValue: "dev",
					NewValue: "production",
					DiffKind: "update",
				},
			},
		},
	}

	actual := []driftadopt.ResourceDiff{
		{
			URN:      "urn:pulumi:dev::app::aws:s3/bucket:Bucket::my-bucket",
			Type:     "aws:s3/bucket:Bucket",
			Name:     "my-bucket",
			DiffType: driftadopt.DiffTypeUpdate,
			PropertyDiff: []driftadopt.PropChange{
				{
					Path:     "tags.Environment",
					OldValue: "dev",
					NewValue: "production",
					DiffKind: "update",
				},
			},
		},
	}

	matcher := driftadopt.NewDiffMatcher()

	// Act
	result := matcher.Matches(expected, actual)

	// Assert
	assert.True(t, result.Matches)
	assert.Empty(t, result.MissingChanges)
	assert.Empty(t, result.UnexpectedChanges)
	assert.Len(t, result.MatchedResources, 1)
}

func TestDiffMatcher_MissingChanges(t *testing.T) {
	// Arrange
	expected := []driftadopt.ResourceDiff{
		{
			URN:  "urn:pulumi:dev::app::aws:s3/bucket:Bucket::my-bucket",
			Type: "aws:s3/bucket:Bucket",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "tags.Environment", OldValue: "dev", NewValue: "production", DiffKind: "update"},
				{Path: "tags.Owner", OldValue: nil, NewValue: "team-a", DiffKind: "add"},
			},
		},
	}

	actual := []driftadopt.ResourceDiff{
		{
			URN:  "urn:pulumi:dev::app::aws:s3/bucket:Bucket::my-bucket",
			Type: "aws:s3/bucket:Bucket",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "tags.Environment", OldValue: "dev", NewValue: "production", DiffKind: "update"},
				// Missing tags.Owner
			},
		},
	}

	matcher := driftadopt.NewDiffMatcher()

	// Act
	result := matcher.Matches(expected, actual)

	// Assert
	assert.False(t, result.Matches)
	assert.Len(t, result.MissingChanges, 1)
	assert.Equal(t, "tags.Owner", result.MissingChanges[0].Path)
	assert.Empty(t, result.UnexpectedChanges)
}

func TestDiffMatcher_UnexpectedChanges(t *testing.T) {
	// Arrange
	expected := []driftadopt.ResourceDiff{
		{
			URN:  "urn:pulumi:dev::app::aws:s3/bucket:Bucket::my-bucket",
			Type: "aws:s3/bucket:Bucket",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "tags.Environment", OldValue: "dev", NewValue: "production", DiffKind: "update"},
			},
		},
	}

	actual := []driftadopt.ResourceDiff{
		{
			URN:  "urn:pulumi:dev::app::aws:s3/bucket:Bucket::my-bucket",
			Type: "aws:s3/bucket:Bucket",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "tags.Environment", OldValue: "dev", NewValue: "production", DiffKind: "update"},
				{Path: "versioning.enabled", OldValue: false, NewValue: true, DiffKind: "update"}, // Unexpected
			},
		},
	}

	matcher := driftadopt.NewDiffMatcher()

	// Act
	result := matcher.Matches(expected, actual)

	// Assert
	assert.False(t, result.Matches)
	assert.Empty(t, result.MissingChanges)
	assert.Len(t, result.UnexpectedChanges, 1)
	assert.Equal(t, "versioning.enabled", result.UnexpectedChanges[0].Path)
}

func TestDiffMatcher_FuzzyValueMatching_StringBool(t *testing.T) {
	// Arrange - string "true" vs bool true
	expected := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "test",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "enabled", OldValue: false, NewValue: true, DiffKind: "update"},
			},
		},
	}

	actual := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "test",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "enabled", OldValue: "false", NewValue: "true", DiffKind: "update"}, // String values
			},
		},
	}

	matcher := driftadopt.NewDiffMatcher()

	// Act
	result := matcher.Matches(expected, actual)

	// Assert
	assert.True(t, result.Matches, "Should match string 'true' with bool true")
}

func TestDiffMatcher_FuzzyValueMatching_NumberString(t *testing.T) {
	// Arrange - number vs string representation
	expected := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "test",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "count", OldValue: 1, NewValue: 10, DiffKind: "update"},
			},
		},
	}

	actual := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "test",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "count", OldValue: "1", NewValue: "10", DiffKind: "update"}, // String values
			},
		},
	}

	matcher := driftadopt.NewDiffMatcher()

	// Act
	result := matcher.Matches(expected, actual)

	// Assert
	assert.True(t, result.Matches, "Should match number 10 with string '10'")
}

func TestDiffMatcher_MultipleResources(t *testing.T) {
	// Arrange
	expected := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "type1",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "prop1", OldValue: "a", NewValue: "b", DiffKind: "update"},
			},
		},
		{
			URN:  "urn:2",
			Type: "type2",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "prop2", OldValue: "x", NewValue: "y", DiffKind: "update"},
			},
		},
	}

	actual := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "type1",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "prop1", OldValue: "a", NewValue: "b", DiffKind: "update"},
			},
		},
		{
			URN:  "urn:2",
			Type: "type2",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "prop2", OldValue: "x", NewValue: "y", DiffKind: "update"},
			},
		},
	}

	matcher := driftadopt.NewDiffMatcher()

	// Act
	result := matcher.Matches(expected, actual)

	// Assert
	assert.True(t, result.Matches)
	assert.Len(t, result.MatchedResources, 2)
}

func TestDiffMatcher_ResourceNotFound(t *testing.T) {
	// Arrange
	expected := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "type1",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "prop1", OldValue: "a", NewValue: "b", DiffKind: "update"},
			},
		},
	}

	actual := []driftadopt.ResourceDiff{
		{
			URN:  "urn:2", // Different URN
			Type: "type1",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "prop1", OldValue: "a", NewValue: "b", DiffKind: "update"},
			},
		},
	}

	matcher := driftadopt.NewDiffMatcher()

	// Act
	result := matcher.Matches(expected, actual)

	// Assert
	assert.False(t, result.Matches)
	assert.Len(t, result.MissingChanges, 1) // All changes from urn:1 are missing
}

func TestDiffMatcher_EmptyDiffs(t *testing.T) {
	// Arrange
	expected := []driftadopt.ResourceDiff{}
	actual := []driftadopt.ResourceDiff{}

	matcher := driftadopt.NewDiffMatcher()

	// Act
	result := matcher.Matches(expected, actual)

	// Assert
	assert.True(t, result.Matches, "Empty diffs should match")
}

func TestDiffMatcher_NestedPropertyPaths(t *testing.T) {
	// Arrange
	expected := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "test",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "config.nested.deep.value", OldValue: 1, NewValue: 2, DiffKind: "update"},
			},
		},
	}

	actual := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "test",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "config.nested.deep.value", OldValue: 1, NewValue: 2, DiffKind: "update"},
			},
		},
	}

	matcher := driftadopt.NewDiffMatcher()

	// Act
	result := matcher.Matches(expected, actual)

	// Assert
	assert.True(t, result.Matches)
}

func TestDiffMatcher_NullValues(t *testing.T) {
	// Arrange
	expected := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "test",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "value", OldValue: nil, NewValue: "something", DiffKind: "add"},
			},
		},
	}

	actual := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "test",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "value", OldValue: nil, NewValue: "something", DiffKind: "add"},
			},
		},
	}

	matcher := driftadopt.NewDiffMatcher()

	// Act
	result := matcher.Matches(expected, actual)

	// Assert
	assert.True(t, result.Matches)
}

func TestDiffMatcher_DifferentDiffKinds(t *testing.T) {
	// Arrange - expected "add" but got "update"
	expected := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "test",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "value", OldValue: nil, NewValue: "something", DiffKind: "add"},
			},
		},
	}

	actual := []driftadopt.ResourceDiff{
		{
			URN:  "urn:1",
			Type: "test",
			PropertyDiff: []driftadopt.PropChange{
				{Path: "value", OldValue: "old", NewValue: "something", DiffKind: "update"},
			},
		},
	}

	matcher := driftadopt.NewDiffMatcher()

	// Act
	result := matcher.Matches(expected, actual)

	// Assert
	assert.False(t, result.Matches, "Different diff kinds should not match")
}
