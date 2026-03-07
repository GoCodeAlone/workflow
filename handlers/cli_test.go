package handlers

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/CrisisTextLine/modular"
)

func TestCLIWorkflowHandler_CanHandle(t *testing.T) {
	h := NewCLIWorkflowHandler()
	if !h.CanHandle("cli") {
		t.Error("expected CanHandle(\"cli\") to return true")
	}
	if h.CanHandle("http") {
		t.Error("expected CanHandle(\"http\") to return false")
	}
	if h.CanHandle("") {
		t.Error("expected CanHandle(\"\") to return false")
	}
}

func TestCLIWorkflowHandler_ConfigureAndDispatch(t *testing.T) {
	h := NewCLIWorkflowHandler()

	var buf bytes.Buffer
	h.SetOutput(&buf)

	called := ""
	h.RegisterCommand("validate", func(args []string) error {
		called = "validate"
		return nil
	})
	h.RegisterCommand("inspect", func(args []string) error {
		called = "inspect"
		return nil
	})

	cfg := map[string]any{
		"name":        "wfctl",
		"version":     "1.0.0",
		"description": "Workflow CLI",
		"commands": []any{
			map[string]any{"name": "validate", "description": "Validate a config"},
			map[string]any{"name": "inspect", "description": "Inspect a config"},
		},
	}

	if err := h.ConfigureWorkflow(nil, cfg); err != nil {
		t.Fatalf("ConfigureWorkflow failed: %v", err)
	}

	if err := h.Dispatch([]string{"validate", "myfile.yaml"}); err != nil {
		t.Fatalf("Dispatch(validate) failed: %v", err)
	}
	if called != "validate" {
		t.Errorf("expected validate handler to be called, got %q", called)
	}

	if err := h.Dispatch([]string{"inspect"}); err != nil {
		t.Fatalf("Dispatch(inspect) failed: %v", err)
	}
	if called != "inspect" {
		t.Errorf("expected inspect handler to be called, got %q", called)
	}
}

func TestCLIWorkflowHandler_UnknownCommand(t *testing.T) {
	h := NewCLIWorkflowHandler()
	var buf bytes.Buffer
	h.SetOutput(&buf)

	if err := h.ConfigureWorkflow(nil, map[string]any{
		"name":     "wfctl",
		"commands": []any{},
	}); err != nil {
		t.Fatalf("ConfigureWorkflow failed: %v", err)
	}

	err := h.Dispatch([]string{"noexist"})
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
	if !containsStr(err.Error(), "unknown command") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCLIWorkflowHandler_NoCommand(t *testing.T) {
	h := NewCLIWorkflowHandler()
	var buf bytes.Buffer
	h.SetOutput(&buf)

	if err := h.ConfigureWorkflow(nil, nil); err != nil {
		t.Fatalf("ConfigureWorkflow failed: %v", err)
	}

	err := h.Dispatch([]string{})
	if err == nil {
		t.Fatal("expected error when no command given")
	}
}

func TestCLIWorkflowHandler_HelpFlag(t *testing.T) {
	h := NewCLIWorkflowHandler()
	var buf bytes.Buffer
	h.SetOutput(&buf)

	cfg := map[string]any{
		"name":    "wfctl",
		"version": "1.2.3",
		"commands": []any{
			map[string]any{"name": "validate", "description": "Validate config"},
		},
	}
	if err := h.ConfigureWorkflow(nil, cfg); err != nil {
		t.Fatalf("ConfigureWorkflow failed: %v", err)
	}

	for _, flag := range []string{"-h", "--help", "help"} {
		buf.Reset()
		if err := h.Dispatch([]string{flag}); err != nil {
			t.Errorf("Dispatch(%q) returned error: %v", flag, err)
		}
		if !containsStr(buf.String(), "wfctl") {
			t.Errorf("usage output missing app name for flag %q: %s", flag, buf.String())
		}
	}
}

func TestCLIWorkflowHandler_VersionFlag(t *testing.T) {
	h := NewCLIWorkflowHandler()
	var buf bytes.Buffer
	h.SetOutput(&buf)

	if err := h.ConfigureWorkflow(nil, map[string]any{
		"name":    "wfctl",
		"version": "2.0.0",
	}); err != nil {
		t.Fatalf("ConfigureWorkflow failed: %v", err)
	}

	for _, flag := range []string{"-v", "--version", "version"} {
		buf.Reset()
		if err := h.Dispatch([]string{flag}); err != nil {
			t.Errorf("Dispatch(%q) returned error: %v", flag, err)
		}
		if !containsStr(buf.String(), "2.0.0") {
			t.Errorf("version output missing version for flag %q: %s", flag, buf.String())
		}
	}
}

func TestCLIWorkflowHandler_HandlerKey(t *testing.T) {
	h := NewCLIWorkflowHandler()
	var buf bytes.Buffer
	h.SetOutput(&buf)

	// Register runner under the explicit handler key, not the command name.
	called := false
	h.RegisterCommand("my-runner", func(args []string) error {
		called = true
		return nil
	})

	if err := h.ConfigureWorkflow(nil, map[string]any{
		"name": "app",
		"commands": []any{
			map[string]any{"name": "do-thing", "handler": "my-runner"},
		},
	}); err != nil {
		t.Fatalf("ConfigureWorkflow failed: %v", err)
	}

	if err := h.Dispatch([]string{"do-thing"}); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if !called {
		t.Error("expected my-runner to be called")
	}
}

func TestCLIWorkflowHandler_CommandReturnsError(t *testing.T) {
	h := NewCLIWorkflowHandler()
	var buf bytes.Buffer
	h.SetOutput(&buf)

	sentinel := errors.New("command failed")
	h.RegisterCommand("fail", func(args []string) error { return sentinel })

	if err := h.ConfigureWorkflow(nil, map[string]any{
		"name": "app",
		"commands": []any{
			map[string]any{"name": "fail"},
		},
	}); err != nil {
		t.Fatalf("ConfigureWorkflow failed: %v", err)
	}

	if err := h.Dispatch([]string{"fail"}); !errors.Is(err, sentinel) {
		t.Errorf("expected sentinel error, got: %v", err)
	}
}

func TestCLIWorkflowHandler_ExecuteWorkflow(t *testing.T) {
	h := NewCLIWorkflowHandler()
	var buf bytes.Buffer
	h.SetOutput(&buf)

	called := false
	h.RegisterCommand("ping", func(args []string) error {
		called = true
		return nil
	})

	if err := h.ConfigureWorkflow(nil, map[string]any{
		"name": "app",
		"commands": []any{
			map[string]any{"name": "ping"},
		},
	}); err != nil {
		t.Fatalf("ConfigureWorkflow failed: %v", err)
	}

	result, err := h.ExecuteWorkflow(context.Background(), "cli", "ping", map[string]any{
		"args": []string{},
	})
	if err != nil {
		t.Fatalf("ExecuteWorkflow failed: %v", err)
	}
	if !called {
		t.Error("expected ping runner to be called")
	}
	if result["success"] != true {
		t.Errorf("expected success=true, got %v", result["success"])
	}
}

func TestCLIWorkflowHandler_InvalidConfig(t *testing.T) {
	h := NewCLIWorkflowHandler()
	err := h.ConfigureWorkflow(nil, 42) // invalid type
	if err == nil {
		t.Fatal("expected error for invalid config type")
	}
}

// TestCLIWorkflowHandler_PipelineDispatch verifies that CLIWorkflowHandler falls
// back to CLIPipelineDispatcher when no direct Go runner is registered.
func TestCLIWorkflowHandler_PipelineDispatch(t *testing.T) {
	h := NewCLIWorkflowHandler()
	var buf bytes.Buffer
	h.SetOutput(&buf)

	app := modular.NewStdApplication(nil, nil)

	// Configure the handler with a command that has no direct runner.
	if err := h.ConfigureWorkflow(app, map[string]any{
		"name": "app",
		"commands": []any{
			map[string]any{"name": "deploy", "description": "Deploy something"},
		},
	}); err != nil {
		t.Fatalf("ConfigureWorkflow failed: %v", err)
	}

	// Register a CLIPipelineDispatcher service in the app.
	dispatched := ""
	dispatcher := &mockCLIPipelineDispatcher{
		dispatch: func(ctx context.Context, cmd string, args []string) error {
			dispatched = cmd
			return nil
		},
	}
	if err := app.RegisterService("cliTrigger", dispatcher); err != nil {
		t.Fatalf("RegisterService failed: %v", err)
	}

	if err := h.Dispatch([]string{"deploy", "--env", "prod"}); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}
	if dispatched != "deploy" {
		t.Errorf("expected deploy to be dispatched, got %q", dispatched)
	}
}

// TestCLIWorkflowHandler_RegisterServiceOnConfigure verifies the handler
// registers itself as CLIWorkflowHandlerServiceName in ConfigureWorkflow.
func TestCLIWorkflowHandler_RegisterServiceOnConfigure(t *testing.T) {
	h := NewCLIWorkflowHandler()
	app := modular.NewStdApplication(nil, nil)

	if err := h.ConfigureWorkflow(app, map[string]any{"name": "app"}); err != nil {
		t.Fatalf("ConfigureWorkflow failed: %v", err)
	}
	var registered *CLIWorkflowHandler
	if err := app.GetService(CLIWorkflowHandlerServiceName, &registered); err != nil {
		t.Errorf("expected handler registered as service, got error: %v", err)
	}
	if registered != h {
		t.Error("expected registered service to be the handler itself")
	}
}

// mockCLIPipelineDispatcher is a test double for CLIPipelineDispatcher.
type mockCLIPipelineDispatcher struct {
	dispatch func(ctx context.Context, cmd string, args []string) error
}

func (m *mockCLIPipelineDispatcher) DispatchCommand(ctx context.Context, cmd string, args []string) error {
	return m.dispatch(ctx, cmd, args)
}

// containsStr is a simple substring helper.
func containsStr(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (s == sub || len(s) > 0 &&
		func() bool {
			for i := 0; i <= len(s)-len(sub); i++ {
				if s[i:i+len(sub)] == sub {
					return true
				}
			}
			return false
		}()))
}
