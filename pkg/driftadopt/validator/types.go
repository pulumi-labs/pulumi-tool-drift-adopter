package validator

import (
	"context"
)

// Validator validates code compilation
type Validator interface {
	// Validate checks if code in the project compiles
	Validate(ctx context.Context, projectPath string) (*ValidationResult, error)
}

// ValidationResult contains the result of a validation
type ValidationResult struct {
	Success bool
	Errors  []CompilationError
}

// CompilationError represents a compilation error
type CompilationError struct {
	File    string
	Line    int
	Column  int
	Message string
}
