package module

import (
	"context"
	"errors"
	"testing"
)

func TestCLIInvokeStep_Basic(t *testing.T) {
	registry := NewCLICommandRegistry()
	called := ""
	registry.Register("validate", func(args []string) error {
		called = "validate"
		return nil
	})

	app := NewMockApplication()
	app.Services[CLICommandRegistryServiceName] = registry

	factory := NewCLIInvokeStepFactory()
	step, err := factory("invoke", map[string]any{"command": "validate"}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"args": []string{"cfg.yaml"}}, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if called != "validate" {
		t.Error("expected validate runner to be called")
	}
	if result.Output["success"] != true {
		t.Errorf("expected success=true, got %v", result.Output["success"])
	}
	if result.Output["command"] != "validate" {
		t.Errorf("expected command=validate, got %v", result.Output["command"])
	}
}

func TestCLIInvokeStep_CommandError(t *testing.T) {
	sentinel := errors.New("validate failed")
	registry := NewCLICommandRegistry()
	registry.Register("validate", func(args []string) error { return sentinel })

	app := NewMockApplication()
	app.Services[CLICommandRegistryServiceName] = registry

	factory := NewCLIInvokeStepFactory()
	step, err := factory("invoke", map[string]any{"command": "validate"}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	_, err = step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

func TestCLIInvokeStep_UnknownCommand(t *testing.T) {
	registry := NewCLICommandRegistry()
	app := NewMockApplication()
	app.Services[CLICommandRegistryServiceName] = registry

	factory := NewCLIInvokeStepFactory()
	step, err := factory("invoke", map[string]any{"command": "nonexistent"}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	_, err = step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestCLIInvokeStep_NoRegistry(t *testing.T) {
	app := NewMockApplication() // registry not registered

	factory := NewCLIInvokeStepFactory()
	step, err := factory("invoke", map[string]any{"command": "validate"}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	_, err = step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err == nil {
		t.Fatal("expected error when registry not found")
	}
}

func TestCLIInvokeStep_RegistryFallbackScan(t *testing.T) {
	registry := NewCLICommandRegistry()
	called := false
	registry.Register("validate", func(args []string) error {
		called = true
		return nil
	})

	app := NewMockApplication()
	// Register under a non-standard name to test fallback scan.
	app.Services["customRegistryKey"] = registry

	factory := NewCLIInvokeStepFactory()
	step, err := factory("invoke", map[string]any{"command": "validate"}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	_, err = step.Execute(context.Background(), NewPipelineContext(nil, nil))
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if !called {
		t.Error("expected validate to be called via fallback scan")
	}
}

func TestCLIInvokeStep_MissingCommand(t *testing.T) {
	factory := NewCLIInvokeStepFactory()
	_, err := factory("invoke", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error for missing command config")
	}
}

func TestCLIInvokeStep_ArgsPassthrough(t *testing.T) {
	registry := NewCLICommandRegistry()
	var receivedArgs []string
	registry.Register("deploy", func(args []string) error {
		receivedArgs = args
		return nil
	})

	app := NewMockApplication()
	app.Services[CLICommandRegistryServiceName] = registry

	factory := NewCLIInvokeStepFactory()
	step, err := factory("invoke", map[string]any{"command": "deploy"}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"args": []string{"--env", "prod", "myapp"}}, nil)
	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(receivedArgs) != 3 || receivedArgs[0] != "--env" {
		t.Errorf("unexpected args: %v", receivedArgs)
	}
}

// TestCLIInvokeStep_ArgsSliceAny verifies that args arriving as []any
// (e.g. after JSON/YAML round-trip) are correctly coerced to []string.
func TestCLIInvokeStep_ArgsSliceAny(t *testing.T) {
	registry := NewCLICommandRegistry()
	var receivedArgs []string
	registry.Register("deploy", func(args []string) error {
		receivedArgs = args
		return nil
	})

	app := NewMockApplication()
	app.Services[CLICommandRegistryServiceName] = registry

	factory := NewCLIInvokeStepFactory()
	step, err := factory("invoke", map[string]any{"command": "deploy"}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	// Simulate JSON-decoded args ([]any instead of []string).
	pc := NewPipelineContext(map[string]any{"args": []any{"--env", "prod"}}, nil)
	if _, err := step.Execute(context.Background(), pc); err != nil {
		t.Fatalf("Execute with []any args failed: %v", err)
	}
	if len(receivedArgs) != 2 || receivedArgs[0] != "--env" || receivedArgs[1] != "prod" {
		t.Errorf("unexpected receivedArgs: %v", receivedArgs)
	}
}

// TestCLIInvokeStep_ArgsSliceAnyNonString verifies that []any with a non-string
// element returns a clear error.
func TestCLIInvokeStep_ArgsSliceAnyNonString(t *testing.T) {
	registry := NewCLICommandRegistry()
	registry.Register("deploy", func(args []string) error { return nil })

	app := NewMockApplication()
	app.Services[CLICommandRegistryServiceName] = registry

	factory := NewCLIInvokeStepFactory()
	step, err := factory("invoke", map[string]any{"command": "deploy"}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	pc := NewPipelineContext(map[string]any{"args": []any{"ok", 42}}, nil)
	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error for non-string element in args")
	}
}
