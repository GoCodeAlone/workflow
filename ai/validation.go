package ai

import (
	"context"
	"fmt"
	"strings"

	"github.com/GoCodeAlone/workflow/dynamic"
)

// ValidationResult holds the outcome of source code validation.
type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// ValidationConfig configures the validation loop behavior.
type ValidationConfig struct {
	MaxRetries        int  `json:"maxRetries"`
	CompileCheck      bool `json:"compileCheck"`
	RequiredFuncCheck bool `json:"requiredFuncCheck"`
}

// DefaultValidationConfig returns sensible defaults.
func DefaultValidationConfig() ValidationConfig {
	return ValidationConfig{
		MaxRetries:        3,
		CompileCheck:      true,
		RequiredFuncCheck: true,
	}
}

// Validator validates and optionally fixes AI-generated component source code.
type Validator struct {
	config ValidationConfig
	pool   *dynamic.InterpreterPool
}

// NewValidator creates a Validator with the given config and interpreter pool.
func NewValidator(config ValidationConfig, pool *dynamic.InterpreterPool) *Validator {
	return &Validator{
		config: config,
		pool:   pool,
	}
}

// ValidateSource checks the given source code for correctness.
// It validates imports, optionally compiles with Yaegi, and checks for
// required exported functions.
func (v *Validator) ValidateSource(source string) *ValidationResult {
	result := &ValidationResult{Valid: true}

	// Step 1: Import validation via dynamic.ValidateSource
	if err := dynamic.ValidateSource(source); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
	}

	// Step 2: Compile check with Yaegi interpreter
	if v.config.CompileCheck && v.pool != nil {
		interp, err := v.pool.NewInterpreter()
		if err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("failed to create interpreter: %v", err))
		} else {
			if _, err := interp.Eval(source); err != nil {
				result.Valid = false
				result.Errors = append(result.Errors, fmt.Sprintf("compilation failed: %v", err))
			}
		}
	}

	// Step 3: Required function check
	if v.config.RequiredFuncCheck {
		if !strings.Contains(source, "func Name()") {
			result.Valid = false
			result.Errors = append(result.Errors, "missing required function: Name() string")
		}
		if !strings.Contains(source, "func Execute(") {
			result.Valid = false
			result.Errors = append(result.Errors, "missing required function: Execute()")
		}
	}

	return result
}

// ValidateAndFix runs a validation loop that attempts to fix invalid source
// code by asking the AI service to regenerate with error context.
// It returns the final source, validation result, and any error encountered.
func (v *Validator) ValidateAndFix(ctx context.Context, aiService *Service, spec ComponentSpec, source string) (string, *ValidationResult, error) {
	var result *ValidationResult

	for attempt := 0; attempt <= v.config.MaxRetries; attempt++ {
		result = v.ValidateSource(source)
		if result.Valid {
			return source, result, nil
		}

		// If this is the last attempt, don't try to fix
		if attempt == v.config.MaxRetries {
			break
		}

		// Build a fix prompt and regenerate
		fixPrompt := v.BuildFixPrompt(spec, source, result.Errors)
		fixSpec := ComponentSpec{
			Name:        spec.Name,
			Type:        spec.Type,
			Description: fixPrompt,
			Interface:   spec.Interface,
		}

		regenerated, err := aiService.GenerateComponent(ctx, fixSpec)
		if err != nil {
			return source, result, fmt.Errorf("regeneration attempt %d failed: %w", attempt+1, err)
		}
		source = regenerated
	}

	return source, result, nil
}

// BuildFixPrompt formats a prompt that includes the original spec, the failing
// source code, and the list of errors so the AI can produce a corrected version.
func (v *Validator) BuildFixPrompt(spec ComponentSpec, source string, errors []string) string {
	var b strings.Builder
	b.WriteString("Fix the following dynamic component source code.\n\n")
	fmt.Fprintf(&b, "Component Name: %s\n", spec.Name)   //nolint:gosec // G705: building internal prompt string, not HTML
	fmt.Fprintf(&b, "Component Type: %s\n", spec.Type)   //nolint:gosec // G705: building internal prompt string, not HTML
	fmt.Fprintf(&b, "Description: %s\n\n", spec.Description) //nolint:gosec // G705: building internal prompt string, not HTML
	b.WriteString("Source code that failed validation:\n```go\n")
	b.WriteString(source)
	b.WriteString("\n```\n\n")
	b.WriteString("Errors found:\n")
	for _, e := range errors {
		fmt.Fprintf(&b, "- %s\n", e)
	}
	b.WriteString("\nPlease fix all errors and return corrected Go source code.\n")
	b.WriteString("The code must use 'package component', only standard library imports,\n")
	b.WriteString("and include Name() string and Execute(context.Context, map[string]interface{}) functions.\n")
	b.WriteString("Return only the Go source code, no explanation.")
	return b.String()
}
