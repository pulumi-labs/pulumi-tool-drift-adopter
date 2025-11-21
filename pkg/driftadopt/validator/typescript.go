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

// TypeScriptValidator validates TypeScript code
type TypeScriptValidator struct{}

// NewTypeScriptValidator creates a new TypeScript validator
func NewTypeScriptValidator() *TypeScriptValidator {
	return &TypeScriptValidator{}
}

// Validate validates TypeScript code using tsc --noEmit
func (v *TypeScriptValidator) Validate(ctx context.Context, projectPath string) (*ValidationResult, error) {
	// Check for tsconfig.json
	tsconfigPath := filepath.Join(projectPath, "tsconfig.json")
	if _, err := os.Stat(tsconfigPath); err != nil {
		return nil, fmt.Errorf("tsconfig.json not found in %s", projectPath)
	}

	// Find tsc executable
	tscPath, err := v.findTSC(projectPath)
	if err != nil {
		return nil, fmt.Errorf("find tsc: %w", err)
	}

	// Run tsc --noEmit
	cmd := exec.CommandContext(ctx, tscPath, "--noEmit")
	cmd.Dir = projectPath

	output, err := cmd.CombinedOutput()

	// tsc returns non-zero exit code on errors, which is expected
	if err != nil && len(output) == 0 {
		return nil, fmt.Errorf("tsc execution failed: %w", err)
	}

	// Parse output
	errors := v.parseErrors(string(output), projectPath)

	return &ValidationResult{
		Success: len(errors) == 0,
		Errors:  errors,
	}, nil
}

// findTSC finds the TypeScript compiler
func (v *TypeScriptValidator) findTSC(projectPath string) (string, error) {
	// Try local node_modules first
	localTSC := filepath.Join(projectPath, "node_modules", ".bin", "tsc")
	if _, err := os.Stat(localTSC); err == nil {
		return localTSC, nil
	}

	// Try global tsc
	tscPath, err := exec.LookPath("tsc")
	if err != nil {
		return "", fmt.Errorf("tsc not found (install TypeScript: npm install --save-dev typescript)")
	}

	return tscPath, nil
}

// parseErrors parses tsc error output
// Format: "file.ts(line,col): error TS1234: message"
func (v *TypeScriptValidator) parseErrors(output, projectPath string) []CompilationError {
	var errors []CompilationError

	// Regex to match tsc error format
	// Example: index.ts(1,7): error TS2322: Type 'string' is not assignable to type 'number'.
	errorRegex := regexp.MustCompile(`(.+?)\((\d+),(\d+)\):\s+error\s+TS\d+:\s+(.+)`)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		matches := errorRegex.FindStringSubmatch(line)
		if len(matches) >= 5 {
			file := matches[1]
			line, _ := strconv.Atoi(matches[2])
			col, _ := strconv.Atoi(matches[3])
			message := matches[4]

			// Make file path absolute if relative
			if !filepath.IsAbs(file) {
				file = filepath.Join(projectPath, file)
			}

			errors = append(errors, CompilationError{
				File:    file,
				Line:    line,
				Column:  col,
				Message: message,
			})
		}
	}

	return errors
}
