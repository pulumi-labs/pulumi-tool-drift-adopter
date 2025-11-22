package driftadopt

import "time"

// DriftPlan represents a complete drift adoption plan for a stack
type DriftPlan struct {
	// Stack is the Pulumi stack name
	Stack string `json:"stack"`

	// GeneratedAt is when this plan was created
	GeneratedAt time.Time `json:"generatedAt"`

	// TotalSteps is the number of steps in this plan
	TotalSteps int `json:"totalSteps"`

	// Steps are the drift adoption steps, ordered by dependencies
	Steps []DriftStep `json:"steps"`
}

// DriftStep represents a group of related drift changes to be adopted together
type DriftStep struct {
	// ID is the unique identifier for this step (e.g., "step-001")
	ID string `json:"id"`

	// Order is the processing order (0 = process first)
	Order int `json:"order"`

	// Resources are the resources with drift in this step
	Resources []ResourceDiff `json:"resources"`

	// Status is the current status of this step
	Status StepStatus `json:"status"`

	// Dependencies are the step IDs that depend on this step
	Dependencies []string `json:"dependencies,omitempty"`

	// Attempt is the number of adoption attempts for this step
	Attempt int `json:"attempt"`

	// LastError is the error message from the last failed attempt
	LastError string `json:"lastError,omitempty"`
}

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

// StepStatus represents the status of a drift step
type StepStatus string

const (
	// StepPending indicates the step hasn't been processed yet
	StepPending StepStatus = "pending"

	// StepInProgress indicates the step is currently being processed
	StepInProgress StepStatus = "in_progress"

	// StepCompleted indicates the step was successfully adopted
	StepCompleted StepStatus = "completed"

	// StepFailed indicates the step adoption failed
	StepFailed StepStatus = "failed"

	// StepSkipped indicates the step was manually skipped
	StepSkipped StepStatus = "skipped"
)
