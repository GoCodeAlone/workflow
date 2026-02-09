package ai

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/dynamic"
)

// validDynamicSource is a well-formed dynamic component for validation tests.
const validDynamicSource = `package component

import (
	"context"
	"fmt"
)

func Name() string {
	return "test-validator"
}

func Init(services map[string]interface{}) error {
	return nil
}

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	name, _ := params["name"].(string)
	if name == "" {
		return nil, fmt.Errorf("missing name")
	}
	return map[string]interface{}{"greeting": "hello " + name}, nil
}
`

func TestValidateSource_Valid(t *testing.T) {
	pool := dynamic.NewInterpreterPool()
	v := NewValidator(DefaultValidationConfig(), pool)

	result := v.ValidateSource(validDynamicSource)
	if !result.Valid {
		t.Errorf("expected valid source, got errors: %v", result.Errors)
	}
	if len(result.Errors) != 0 {
		t.Errorf("expected no errors, got: %v", result.Errors)
	}
}

func TestValidateSource_InvalidImports(t *testing.T) {
	pool := dynamic.NewInterpreterPool()
	v := NewValidator(DefaultValidationConfig(), pool)

	source := `package component

import "os/exec"

func Name() string { return "bad" }
func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	_ = exec.Command("echo")
	return nil, nil
}
`
	result := v.ValidateSource(source)
	if result.Valid {
		t.Error("expected invalid result for blocked import")
	}
	found := false
	for _, e := range result.Errors {
		if strings.Contains(e, "os/exec") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected error mentioning os/exec, got: %v", result.Errors)
	}
}

func TestValidateSource_MissingRequiredFunctions(t *testing.T) {
	pool := dynamic.NewInterpreterPool()
	v := NewValidator(DefaultValidationConfig(), pool)

	source := `package component

func Helper() string { return "helper" }
`
	result := v.ValidateSource(source)
	if result.Valid {
		t.Error("expected invalid result for missing functions")
	}

	hasNameErr := false
	hasExecuteErr := false
	for _, e := range result.Errors {
		if strings.Contains(e, "Name()") {
			hasNameErr = true
		}
		if strings.Contains(e, "Execute()") {
			hasExecuteErr = true
		}
	}
	if !hasNameErr {
		t.Error("expected error about missing Name() function")
	}
	if !hasExecuteErr {
		t.Error("expected error about missing Execute() function")
	}
}

func TestValidateSource_CompileError(t *testing.T) {
	pool := dynamic.NewInterpreterPool()
	v := NewValidator(DefaultValidationConfig(), pool)

	// Syntactically valid Go with imports parsed OK, but will fail Yaegi compile
	// due to undefined reference.
	source := `package component

import "context"

func Name() string { return "bad" }

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	x := undefinedFunction()
	_ = x
	return nil, nil
}
`
	result := v.ValidateSource(source)
	if result.Valid {
		t.Error("expected invalid result for code with undefined references")
	}
	hasCompileErr := false
	for _, e := range result.Errors {
		if strings.Contains(e, "compilation failed") {
			hasCompileErr = true
			break
		}
	}
	if !hasCompileErr {
		t.Errorf("expected compilation error, got: %v", result.Errors)
	}
}

func TestValidateSource_NilPool(t *testing.T) {
	cfg := DefaultValidationConfig()
	v := NewValidator(cfg, nil)

	// With nil pool, compile check is skipped but import and func checks still run
	result := v.ValidateSource(validDynamicSource)
	if !result.Valid {
		t.Errorf("expected valid result with nil pool, got errors: %v", result.Errors)
	}
}

func TestValidateSource_CompileCheckDisabled(t *testing.T) {
	pool := dynamic.NewInterpreterPool()
	cfg := ValidationConfig{
		MaxRetries:        3,
		CompileCheck:      false,
		RequiredFuncCheck: true,
	}
	v := NewValidator(cfg, pool)

	// Code that would fail compile but has correct structure
	source := `package component

import "context"

func Name() string { return "test" }

func Execute(ctx context.Context, params map[string]interface{}) (map[string]interface{}, error) {
	x := undefinedFunction()
	_ = x
	return nil, nil
}
`
	result := v.ValidateSource(source)
	// Should pass because compile check is disabled
	if !result.Valid {
		t.Errorf("expected valid when compile check disabled, got errors: %v", result.Errors)
	}
}

func TestValidateAndFix_AlreadyValid(t *testing.T) {
	pool := dynamic.NewInterpreterPool()
	v := NewValidator(DefaultValidationConfig(), pool)

	svc := NewService()
	mock := &MockGenerator{}
	svc.RegisterGenerator(ProviderAnthropic, mock)

	spec := ComponentSpec{
		Name:        "test",
		Type:        "test.component",
		Description: "A test component",
	}

	source, result, err := v.ValidateAndFix(context.Background(), svc, spec, validDynamicSource)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected valid result, got errors: %v", result.Errors)
	}
	if source != validDynamicSource {
		t.Error("expected source to be unchanged")
	}
}

func TestValidateAndFix_FixedByRetry(t *testing.T) {
	pool := dynamic.NewInterpreterPool()
	v := NewValidator(DefaultValidationConfig(), pool)

	callCount := 0
	mock := &MockGenerator{
		GenerateComponentFn: func(ctx context.Context, spec ComponentSpec) (string, error) {
			callCount++
			// On first call, return valid source
			return validDynamicSource, nil
		},
	}

	svc := NewService()
	svc.RegisterGenerator(ProviderAnthropic, mock)

	spec := ComponentSpec{
		Name:        "test",
		Type:        "test.component",
		Description: "A test component",
	}

	// Start with invalid source (missing required functions)
	invalidSource := `package component

func Helper() string { return "helper" }
`

	source, result, err := v.ValidateAndFix(context.Background(), svc, spec, invalidSource)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Valid {
		t.Errorf("expected valid result after fix, got errors: %v", result.Errors)
	}
	if source != validDynamicSource {
		t.Error("expected source to be the fixed version")
	}
	if callCount != 1 {
		t.Errorf("expected 1 regeneration call, got %d", callCount)
	}
}

func TestValidateAndFix_ExhaustedRetries(t *testing.T) {
	pool := dynamic.NewInterpreterPool()
	cfg := ValidationConfig{
		MaxRetries:        2,
		CompileCheck:      false,
		RequiredFuncCheck: true,
	}
	v := NewValidator(cfg, pool)

	// Always return invalid source
	mock := &MockGenerator{
		GenerateComponentFn: func(ctx context.Context, spec ComponentSpec) (string, error) {
			return `package component

func Helper() string { return "still-bad" }
`, nil
		},
	}

	svc := NewService()
	svc.RegisterGenerator(ProviderAnthropic, mock)

	spec := ComponentSpec{
		Name:        "test",
		Type:        "test.component",
		Description: "A test component",
	}

	invalidSource := `package component

func Helper() string { return "bad" }
`

	_, result, err := v.ValidateAndFix(context.Background(), svc, spec, invalidSource)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Valid {
		t.Error("expected invalid result after exhausting retries")
	}
}

func TestValidateAndFix_GenerationError(t *testing.T) {
	pool := dynamic.NewInterpreterPool()
	v := NewValidator(DefaultValidationConfig(), pool)

	mock := &MockGenerator{
		GenerateComponentFn: func(ctx context.Context, spec ComponentSpec) (string, error) {
			return "", fmt.Errorf("API error")
		},
	}

	svc := NewService()
	svc.RegisterGenerator(ProviderAnthropic, mock)

	spec := ComponentSpec{
		Name:        "test",
		Type:        "test.component",
		Description: "A test component",
	}

	invalidSource := `package component

func Helper() string { return "bad" }
`

	_, _, err := v.ValidateAndFix(context.Background(), svc, spec, invalidSource)
	if err == nil {
		t.Error("expected error when AI generation fails")
	}
	if !strings.Contains(err.Error(), "regeneration attempt") {
		t.Errorf("expected regeneration error, got: %v", err)
	}
}

func TestBuildFixPrompt(t *testing.T) {
	v := NewValidator(DefaultValidationConfig(), nil)

	spec := ComponentSpec{
		Name:        "my-component",
		Type:        "test.type",
		Description: "A test component that does things",
	}

	source := `package component

func Helper() string { return "bad" }
`
	errors := []string{
		"missing required function: Name() string",
		"missing required function: Execute()",
	}

	prompt := v.BuildFixPrompt(spec, source, errors)

	if !strings.Contains(prompt, "my-component") {
		t.Error("fix prompt missing component name")
	}
	if !strings.Contains(prompt, "test.type") {
		t.Error("fix prompt missing component type")
	}
	if !strings.Contains(prompt, "A test component that does things") {
		t.Error("fix prompt missing description")
	}
	if !strings.Contains(prompt, "func Helper()") {
		t.Error("fix prompt missing source code")
	}
	if !strings.Contains(prompt, "missing required function: Name() string") {
		t.Error("fix prompt missing error about Name()")
	}
	if !strings.Contains(prompt, "missing required function: Execute()") {
		t.Error("fix prompt missing error about Execute()")
	}
	if !strings.Contains(prompt, "package component") {
		t.Error("fix prompt missing instruction about package component")
	}
}

func TestDefaultValidationConfig(t *testing.T) {
	cfg := DefaultValidationConfig()

	if cfg.MaxRetries != 3 {
		t.Errorf("expected MaxRetries=3, got %d", cfg.MaxRetries)
	}
	if !cfg.CompileCheck {
		t.Error("expected CompileCheck=true")
	}
	if !cfg.RequiredFuncCheck {
		t.Error("expected RequiredFuncCheck=true")
	}
}

func TestNewValidator(t *testing.T) {
	pool := dynamic.NewInterpreterPool()
	cfg := ValidationConfig{MaxRetries: 5, CompileCheck: true, RequiredFuncCheck: false}
	v := NewValidator(cfg, pool)

	if v.config.MaxRetries != 5 {
		t.Errorf("expected MaxRetries=5, got %d", v.config.MaxRetries)
	}
	if !v.config.CompileCheck {
		t.Error("expected CompileCheck=true")
	}
	if v.config.RequiredFuncCheck {
		t.Error("expected RequiredFuncCheck=false")
	}
	if v.pool == nil {
		t.Error("expected non-nil pool")
	}
}

func TestContextEnrichedPrompt(t *testing.T) {
	spec := ComponentSpec{
		Name:        "data-processor",
		Type:        "data.processor",
		Description: "Processes incoming data streams",
	}

	modules := []string{"http.server", "messaging.broker", "cache.modular"}
	services := []string{"logger", "database", "eventbus"}

	prompt := ContextEnrichedPrompt(spec, modules, services)

	// Should contain base dynamic prompt content
	if !strings.Contains(prompt, "data-processor") {
		t.Error("prompt missing component name")
	}
	if !strings.Contains(prompt, "package component") {
		t.Error("prompt missing package component instruction")
	}

	// Should contain available modules
	if !strings.Contains(prompt, "Available Module Types") {
		t.Error("prompt missing module types section")
	}
	for _, m := range modules {
		if !strings.Contains(prompt, m) {
			t.Errorf("prompt missing module: %s", m)
		}
	}

	// Should contain available services
	if !strings.Contains(prompt, "Available Services") {
		t.Error("prompt missing services section")
	}
	for _, s := range services {
		if !strings.Contains(prompt, s) {
			t.Errorf("prompt missing service: %s", s)
		}
	}

	// Test with empty lists
	minimal := ContextEnrichedPrompt(spec, nil, nil)
	if strings.Contains(minimal, "Available Module Types") {
		t.Error("minimal prompt should not contain modules section")
	}
	if strings.Contains(minimal, "Available Services") {
		t.Error("minimal prompt should not contain services section")
	}
}
