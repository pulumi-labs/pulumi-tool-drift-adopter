package driftadopt

// ResourceDiff represents drift for a single resource
type ResourceDiff struct {
	// URN is the Pulumi URN of the resource
	URN string `json:"urn"`

	// Type is the resource type (e.g., "aws:s3/bucket:Bucket")
	Type string `json:"type"`

	// Name is the logical name of the resource
	Name string `json:"name"`

	// DiffType indicates the type of drift
	DiffType DiffType `json:"diffType"`

	// PropertyDiff are the specific property changes
	PropertyDiff []PropChange `json:"propertyDiff"`

	// SourceFile is the file where this resource is defined
	SourceFile string `json:"sourceFile,omitempty"`

	// SourceLine is the line number where this resource is defined
	SourceLine int `json:"sourceLine,omitempty"`
}

// PropChange represents a single property change
type PropChange struct {
	// Path is the property path (e.g., "tags.Environment")
	Path string `json:"path"`

	// OldValue is the current value in IaC
	OldValue interface{} `json:"oldValue"`

	// NewValue is the actual value in the cloud
	NewValue interface{} `json:"newValue"`

	// DiffKind is the kind of change ("add", "delete", "update")
	DiffKind string `json:"diffKind"`
}

// DiffType indicates the type of drift for a resource
type DiffType string

const (
	// DiffTypeUpdate indicates property changes without replacement
	DiffTypeUpdate DiffType = "update"

	// DiffTypeDelete indicates the resource was deleted in the cloud
	DiffTypeDelete DiffType = "delete"

	// DiffTypeReplace indicates the resource needs replacement
	DiffTypeReplace DiffType = "replace"
)
