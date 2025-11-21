package validator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// GoValidator validates Go code
type GoValidator struct{}

// NewGoValidator creates a new Go validator
func NewGoValidator() *GoValidator {
	return &GoValidator{}
}

// Validate validates Go code using go build
func (v *GoValidator) Validate(ctx context.Context, projectPath string) (*ValidationResult, error) {
	// Check for go.mod
	goModPath := filepath.Join(projectPath, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		return nil, fmt.Errorf("go.mod not found in %s", projectPath)
	}

	// Find go executable
	goPath, err := exec.LookPath("go")
	if err != nil {
		return nil, fmt.Errorf("go not found in PATH")
	}

	// Run go build
	cmd := exec.CommandContext(ctx, goPath, "build", "./...")
	cmd.Dir = projectPath

	output, err := cmd.CombinedOutput()

	// go build returns non-zero on errors
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("go build execution failed: %w", err)
	}

	// Parse errors
	errors := v.parseErrors(string(output), projectPath)

	return &ValidationResult{
		Success: len(errors) == 0,
		Errors:  errors,
	}, nil
}

// parseErrors parses Go compiler error output
// Format: "file.go:line:col: message"
func (v *GoValidator) parseErrors(output, projectPath string) []CompilationError {
	var errors []CompilationError

	// Regex to match Go error format
	// Example: main.go:4:13: cannot use "not an int" (untyped string constant) as int value in variable declaration
	errorRegex := regexp.MustCompile(`(.+?):(\d+):(\d+):\s+(.+)`)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		matches := errorRegex.FindStringSubmatch(line)
		if len(matches) >= 5 {
			file := matches[1]
			lineNum, _ := strconv.Atoi(matches[2])
			col, _ := strconv.Atoi(matches[3])
			message := matches[4]

			// Make file path absolute if relative
			if !filepath.IsAbs(file) {
				file = filepath.Join(projectPath, file)
			}

			errors = append(errors, CompilationError{
				File:    file,
				Line:    lineNum,
				Column:  col,
				Message: message,
			})
		}
	}

	return errors
}
