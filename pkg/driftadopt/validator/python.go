package validator

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// PythonValidator validates Python code
type PythonValidator struct{}

// NewPythonValidator creates a new Python validator
func NewPythonValidator() *PythonValidator {
	return &PythonValidator{}
}

// Validate validates Python code using py_compile and mypy
func (v *PythonValidator) Validate(ctx context.Context, projectPath string) (*ValidationResult, error) {
	// Find all Python files
	pythonFiles, err := filepath.Glob(filepath.Join(projectPath, "*.py"))
	if err != nil {
		return nil, fmt.Errorf("glob Python files: %w", err)
	}

	// No Python files is success
	if len(pythonFiles) == 0 {
		return &ValidationResult{Success: true}, nil
	}

	// Find python executable
	pythonPath, err := exec.LookPath("python3")
	if err != nil {
		pythonPath, err = exec.LookPath("python")
		if err != nil {
			return nil, fmt.Errorf("python not found in PATH")
		}
	}

	var allErrors []CompilationError

	// Step 1: Run py_compile for syntax checking
	for _, file := range pythonFiles {
		cmd := exec.CommandContext(ctx, pythonPath, "-m", "py_compile", file)
		output, err := cmd.CombinedOutput()

		// py_compile returns non-zero on errors
		if err != nil {
			errors := v.parseErrors(string(output), file)
			allErrors = append(allErrors, errors...)
		}
	}

	// Step 2: Run mypy for type checking (if available)
	if mypyPath, err := exec.LookPath("mypy"); err == nil {
		cmd := exec.CommandContext(ctx, mypyPath, projectPath)
		output, err := cmd.CombinedOutput()

		// mypy returns non-zero on errors
		if err != nil {
			errors := v.parseMypyErrors(string(output))
			allErrors = append(allErrors, errors...)
		}
	}

	return &ValidationResult{
		Success: len(allErrors) == 0,
		Errors:  allErrors,
	}, nil
}

// parseErrors parses Python error output
// Format examples:
//   File "file.py", line 123
//   SyntaxError: invalid syntax
func (v *PythonValidator) parseErrors(output, filePath string) []CompilationError {
	var errors []CompilationError

	// Regex to match Python error format
	// Example: File "file.py", line 4
	fileLineRegex := regexp.MustCompile(`File "(.+?)", line (\d+)`)
	matches := fileLineRegex.FindAllStringSubmatch(output, -1)

	lines := strings.Split(output, "\n")

	for i, match := range matches {
		if len(match) < 3 {
			continue
		}

		file := match[1]
		line, _ := strconv.Atoi(match[2])

		// Try to find error message on following lines
		message := "Syntax error"
		if i < len(lines)-1 {
			// Look for error type (SyntaxError, IndentationError, etc.)
			for j := 0; j < len(lines); j++ {
				if strings.Contains(lines[j], "Error:") {
					message = strings.TrimSpace(lines[j])
					break
				}
			}
		}

		errors = append(errors, CompilationError{
			File:    file,
			Line:    line,
			Column:  0, // Python errors don't always provide column
			Message: message,
		})
	}

	// If no structured errors found but there's output, create a generic error
	if len(errors) == 0 && output != "" {
		errors = append(errors, CompilationError{
			File:    filePath,
			Line:    1,
			Column:  0,
			Message: strings.TrimSpace(output),
		})
	}

	return errors
}

// parseMypyErrors parses mypy error output
// Format: "file.py:line: error: message"
func (v *PythonValidator) parseMypyErrors(output string) []CompilationError {
	var errors []CompilationError

	// Regex to match mypy error format
	// Example: main.py:4: error: Incompatible types in assignment
	errorRegex := regexp.MustCompile(`(.+?):(\d+):\s+error:\s+(.+)`)

	lines := strings.Split(output, "\n")
	for _, line := range lines {
		matches := errorRegex.FindStringSubmatch(line)
		if len(matches) >= 4 {
			file := matches[1]
			lineNum, _ := strconv.Atoi(matches[2])
			message := matches[3]

			errors = append(errors, CompilationError{
				File:    file,
				Line:    lineNum,
				Column:  0,
				Message: message,
			})
		}
	}

	return errors
}
