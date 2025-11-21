//go:build unit

package driftadopt_test

import (
	"testing"

	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt"
	"github.com/pulumi/pulumi-drift-adoption-tool/pkg/driftadopt/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPreviewParser_ParseDiff_Update(t *testing.T) {
	// Arrange
	previewJSON := testutil.LoadFixture(t, "testdata/previews/update-preview.json")
	parser := driftadopt.NewPreviewParser()

	// Act
	diffs, err := parser.ParseDiff(string(previewJSON))
	require.NoError(t, err)

	// Assert
	require.Len(t, diffs, 1)

	diff := diffs[0]
	assert.Equal(t, "urn:pulumi:dev::myapp::aws:s3/bucket:Bucket::my-bucket", diff.URN)
	assert.Equal(t, "aws:s3/bucket:Bucket", diff.Type)
	assert.Equal(t, "my-bucket", diff.Name)
	assert.Equal(t, driftadopt.DiffTypeUpdate, diff.DiffType)

	// Check property diff
	require.Len(t, diff.PropertyDiff, 1)
	prop := diff.PropertyDiff[0]
	assert.Equal(t, "tags.Environment", prop.Path)
	assert.Equal(t, "dev", prop.OldValue)
	assert.Equal(t, "production", prop.NewValue)
	assert.Equal(t, "update", prop.DiffKind)
}

func TestPreviewParser_ParseDiff_Delete(t *testing.T) {
	// Arrange
	previewJSON := testutil.LoadFixture(t, "testdata/previews/delete-preview.json")
	parser := driftadopt.NewPreviewParser()

	// Act
	diffs, err := parser.ParseDiff(string(previewJSON))
	require.NoError(t, err)

	// Assert
	require.Len(t, diffs, 1)

	diff := diffs[0]
	assert.Equal(t, "urn:pulumi:dev::myapp::aws:s3/bucket:Bucket::old-bucket", diff.URN)
	assert.Equal(t, "aws:s3/bucket:Bucket", diff.Type)
	assert.Equal(t, driftadopt.DiffTypeDelete, diff.DiffType)
}

func TestPreviewParser_ParseDiff_Replace(t *testing.T) {
	// Arrange
	previewJSON := testutil.LoadFixture(t, "testdata/previews/replace-preview.json")
	parser := driftadopt.NewPreviewParser()

	// Act
	diffs, err := parser.ParseDiff(string(previewJSON))
	require.NoError(t, err)

	// Assert
	require.Len(t, diffs, 1)

	diff := diffs[0]
	assert.Equal(t, "urn:pulumi:dev::myapp::aws:ec2/instance:Instance::my-instance", diff.URN)
	assert.Equal(t, "aws:ec2/instance:Instance", diff.Type)
	assert.Equal(t, driftadopt.DiffTypeReplace, diff.DiffType)

	// Check property diff
	require.Len(t, diff.PropertyDiff, 1)
	prop := diff.PropertyDiff[0]
	assert.Equal(t, "instanceType", prop.Path)
	assert.Equal(t, "t2.micro", prop.OldValue)
	assert.Equal(t, "t2.small", prop.NewValue)
}

func TestPreviewParser_ParseDiff_MultipleChanges(t *testing.T) {
	// Arrange
	previewJSON := testutil.LoadFixture(t, "testdata/previews/multiple-changes.json")
	parser := driftadopt.NewPreviewParser()

	// Act
	diffs, err := parser.ParseDiff(string(previewJSON))
	require.NoError(t, err)

	// Assert
	require.Len(t, diffs, 2)

	// First resource - bucket with multiple property changes
	bucket := diffs[0]
	assert.Equal(t, "urn:pulumi:dev::myapp::aws:s3/bucket:Bucket::my-bucket", bucket.URN)
	assert.Equal(t, driftadopt.DiffTypeUpdate, bucket.DiffType)
	assert.Len(t, bucket.PropertyDiff, 3) // tags.Owner (add), tags.Environment (update), versioning.enabled (update)

	// Check property changes
	var ownerProp, envProp, versioningProp *driftadopt.PropChange
	for i := range bucket.PropertyDiff {
		prop := &bucket.PropertyDiff[i]
		switch prop.Path {
		case "tags.Owner":
			ownerProp = prop
		case "tags.Environment":
			envProp = prop
		case "versioning.enabled":
			versioningProp = prop
		}
	}

	require.NotNil(t, ownerProp)
	assert.Equal(t, "add", ownerProp.DiffKind)
	assert.Equal(t, "alice", ownerProp.NewValue)

	require.NotNil(t, envProp)
	assert.Equal(t, "update", envProp.DiffKind)
	assert.Equal(t, "dev", envProp.OldValue)
	assert.Equal(t, "production", envProp.NewValue)

	require.NotNil(t, versioningProp)
	assert.Equal(t, "update", versioningProp.DiffKind)
	assert.Equal(t, false, versioningProp.OldValue)
	assert.Equal(t, true, versioningProp.NewValue)

	// Second resource - object
	object := diffs[1]
	assert.Equal(t, "urn:pulumi:dev::myapp::aws:s3/bucketObject:BucketObject::my-object", object.URN)
	assert.Len(t, object.PropertyDiff, 1)
}

func TestPreviewParser_EmptyJSON(t *testing.T) {
	// Arrange
	parser := driftadopt.NewPreviewParser()

	// Act
	diffs, err := parser.ParseDiff("{}")
	require.NoError(t, err)

	// Assert
	assert.Empty(t, diffs)
}

func TestPreviewParser_NoSteps(t *testing.T) {
	// Arrange
	previewJSON := `{"steps": []}`
	parser := driftadopt.NewPreviewParser()

	// Act
	diffs, err := parser.ParseDiff(previewJSON)
	require.NoError(t, err)

	// Assert
	assert.Empty(t, diffs)
}

func TestPreviewParser_InvalidJSON(t *testing.T) {
	// Arrange
	parser := driftadopt.NewPreviewParser()

	// Act
	_, err := parser.ParseDiff("not json")

	// Assert
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unmarshal")
}
