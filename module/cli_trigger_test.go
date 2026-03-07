package module

import (
	"context"
	"testing"
)

// mockCLIEngine is a minimal WorkflowEngine for testing CLITrigger.
type mockCLIEngine struct {
	calls []cliTriggerCall
}

type cliTriggerCall struct {
	workflowType string
	action       string
	data         map[string]any
}

func (m *mockCLIEngine) TriggerWorkflow(_ context.Context, workflowType, action string, data map[string]any) error {
	m.calls = append(m.calls, cliTriggerCall{workflowType: workflowType, action: action, data: data})
	return nil
}

func TestCLITrigger_Name(t *testing.T) {
	tr := NewCLITrigger()
	if tr.Name() != CLITriggerName {
		t.Errorf("expected %q, got %q", CLITriggerName, tr.Name())
	}
}

func TestCLITrigger_StartStop(t *testing.T) {
	tr := NewCLITrigger()
	if err := tr.Start(context.Background()); err != nil {
		t.Errorf("Start should be no-op, got error: %v", err)
	}
	if err := tr.Stop(context.Background()); err != nil {
		t.Errorf("Stop should be no-op, got error: %v", err)
	}
}

func TestCLITrigger_Configure_AndDispatch(t *testing.T) {
	engine := &mockCLIEngine{}
	app := NewMockApplication()
	app.Services["workflowEngine"] = engine

	tr := NewCLITrigger()

	cfg := map[string]any{
		"command":      "validate",
		"workflowType": "pipeline:cmd-validate",
	}
	if err := tr.Configure(app, cfg); err != nil {
		t.Fatalf("Configure failed: %v", err)
	}

	if _, ok := app.Services[CLITriggerName]; !ok {
		t.Error("expected CLITrigger to register itself as service")
	}
	if !tr.HasCommand("validate") {
		t.Error("expected HasCommand(\"validate\") == true")
	}
	if err := tr.DispatchCommand(context.Background(), "validate", []string{"cfg.yaml"}); err != nil {
		t.Fatalf("DispatchCommand failed: %v", err)
	}
	if len(engine.calls) != 1 {
		t.Fatalf("expected 1 engine call, got %d", len(engine.calls))
	}
	c := engine.calls[0]
	if c.workflowType != "pipeline:cmd-validate" {
		t.Errorf("unexpected workflowType %q", c.workflowType)
	}
	if c.data["command"] != "validate" {
		t.Errorf("expected data.command=validate, got %v", c.data["command"])
	}
	if args, ok := c.data["args"].([]string); !ok || len(args) != 1 || args[0] != "cfg.yaml" {
		t.Errorf("unexpected data.args: %v", c.data["args"])
	}
}

func TestCLITrigger_Configure_MultipleCalls(t *testing.T) {
	engine := &mockCLIEngine{}
	app := NewMockApplication()
	app.Services["workflowEngine"] = engine
	tr := NewCLITrigger()

	for _, cmd := range []string{"validate", "inspect", "run"} {
		cfg := map[string]any{
			"command":      cmd,
			"workflowType": "pipeline:cmd-" + cmd,
		}
		if err := tr.Configure(app, cfg); err != nil {
			t.Fatalf("Configure(%s) failed: %v", cmd, err)
		}
	}
	for _, cmd := range []string{"validate", "inspect", "run"} {
		if !tr.HasCommand(cmd) {
			t.Errorf("expected HasCommand(%q) == true", cmd)
		}
	}
}

func TestCLITrigger_DispatchUnknownCommand(t *testing.T) {
	engine := &mockCLIEngine{}
	tr := NewCLITrigger()
	tr.engine = engine
	err := tr.DispatchCommand(context.Background(), "unknowncmd", nil)
	if err == nil {
		t.Fatal("expected error for unknown command")
	}
}

func TestCLITrigger_Configure_InvalidConfig(t *testing.T) {
	tr := NewCLITrigger()
	if err := tr.Configure(NewMockApplication(), 42); err == nil {
		t.Fatal("expected error for invalid config type")
	}
}

func TestCLITrigger_Configure_MissingCommand(t *testing.T) {
	tr := NewCLITrigger()
	err := tr.Configure(NewMockApplication(), map[string]any{
		"workflowType": "pipeline:foo",
	})
	if err == nil {
		t.Fatal("expected error for missing command")
	}
}

func TestCLITrigger_Configure_MissingWorkflowType(t *testing.T) {
	tr := NewCLITrigger()
	err := tr.Configure(NewMockApplication(), map[string]any{
		"command": "validate",
	})
	if err == nil {
		t.Fatal("expected error for missing workflowType")
	}
}

// TestCLITrigger_Configure_DuplicateCommandConflict verifies that registering
// two different workflow types for the same command returns an error.
func TestCLITrigger_Configure_DuplicateCommandConflict(t *testing.T) {
	engine := &mockCLIEngine{}
	app := NewMockApplication()
	app.Services["workflowEngine"] = engine
	tr := NewCLITrigger()

	if err := tr.Configure(app, map[string]any{
		"command":      "validate",
		"workflowType": "pipeline:cmd-validate",
	}); err != nil {
		t.Fatalf("first Configure failed: %v", err)
	}

	// Same command, different workflowType → must error.
	err := tr.Configure(app, map[string]any{
		"command":      "validate",
		"workflowType": "pipeline:cmd-other",
	})
	if err == nil {
		t.Fatal("expected error for conflicting command registration")
	}
}

// TestCLITrigger_Configure_IdempotentReregistration verifies that registering
// the same command→workflowType mapping twice is allowed (idempotent).
func TestCLITrigger_Configure_IdempotentReregistration(t *testing.T) {
	engine := &mockCLIEngine{}
	app := NewMockApplication()
	app.Services["workflowEngine"] = engine
	tr := NewCLITrigger()

	cfg := map[string]any{
		"command":      "validate",
		"workflowType": "pipeline:cmd-validate",
	}
	if err := tr.Configure(app, cfg); err != nil {
		t.Fatalf("first Configure failed: %v", err)
	}
	// Exact same mapping again — must not error.
	if err := tr.Configure(app, cfg); err != nil {
		t.Fatalf("idempotent Configure failed: %v", err)
	}
}
