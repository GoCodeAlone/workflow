package module

import (
	"context"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// CLITriggerName is the canonical name / service key for the CLI trigger.
const CLITriggerName = "trigger.cli"

// CLITrigger maps CLI command names to workflow pipelines. It implements the
// interfaces.Trigger contract so it can be registered with the engine via
// engine.RegisterTrigger() or DefaultTriggers().
//
// In a workflow config file, CLI commands are wired to pipelines via the "cli"
// pipeline trigger type:
//
//	pipelines:
//	  cmd-validate:
//	    trigger:
//	      type: cli
//	      config:
//	        command: validate
//	    steps:
//	      - name: run
//	        type: step.cli_invoke
//	        config:
//	          command: validate
//
// The engine's wrapPipelineTriggerConfig helper enriches the flat config with a
// "workflowType" key before calling Configure, so the trigger receives:
//
//	{command: "validate", workflowType: "pipeline:cmd-validate"}
type CLITrigger struct {
	name     string
	commands map[string]string // command name → "pipeline:<name>"
	engine   WorkflowEngine
}

// NewCLITrigger creates a new CLITrigger.
func NewCLITrigger() *CLITrigger {
	return &CLITrigger{
		name:     CLITriggerName,
		commands: make(map[string]string),
	}
}

// Name returns the trigger's canonical name ("trigger.cli").
func (t *CLITrigger) Name() string { return t.name }

// Dependencies returns nil — the CLI trigger has no module dependencies.
func (t *CLITrigger) Dependencies() []string { return nil }

// Init is a no-op; triggers registered via engine.RegisterTrigger are not
// Init-ed through the module system. Registration as a service happens in
// Configure, which IS called during engine configuration.
func (t *CLITrigger) Init(_ modular.Application) error { return nil }

// Configure processes a single command→pipeline mapping. It is called once per
// pipeline that carries a "cli" inline trigger config. The enriched config map
// must contain:
//
//	command      — the CLI command name (e.g. "validate")
//	workflowType — the pipeline workflow identifier (e.g. "pipeline:cmd-validate")
//
// Configure also registers the trigger as the "trigger.cli" service so that
// CLIWorkflowHandler can discover it via the application service registry.
func (t *CLITrigger) Configure(app modular.Application, triggerConfig any) error {
	cfg, ok := triggerConfig.(map[string]any)
	if !ok {
		return fmt.Errorf("cli trigger: invalid config type %T (expected map[string]any)", triggerConfig)
	}

	// Register as a service so CLIWorkflowHandler can find us lazily.
	// If a service under the same name already exists, tolerate it only when
	// the existing entry is this same *CLITrigger instance (idempotent
	// re-registration across multiple pipeline Configure calls). A different
	// instance occupying the slot is a configuration error.
	if err := app.RegisterService(t.name, t); err != nil {
		sameInstance := false
		for _, svc := range app.SvcRegistry() {
			if existing, ok := svc.(*CLITrigger); ok && existing == t {
				sameInstance = true
				break
			}
		}
		if !sameInstance {
			return fmt.Errorf("cli trigger: registering service %q: %w", t.name, err)
		}
	}

	// Find the workflow engine in app services (engine registers itself as
	// "workflowEngine" during configureTriggers, before configurePipelines runs).
	if t.engine == nil {
		for _, svc := range app.SvcRegistry() {
			if e, ok := svc.(WorkflowEngine); ok {
				t.engine = e
				break
			}
		}
	}

	command, _ := cfg["command"].(string)
	workflowType, _ := cfg["workflowType"].(string)

	if command == "" {
		return fmt.Errorf("cli trigger: 'command' is required in trigger config")
	}
	if workflowType == "" {
		return fmt.Errorf("cli trigger: 'workflowType' is required in trigger config (injected by the engine)")
	}

	// Prevent ambiguous CLI routing: reject a different workflowType for a
	// command that is already registered. Re-registering the exact same
	// mapping (idempotent from hot-reload) is allowed.
	if existing, ok := t.commands[command]; ok && existing != workflowType {
		return fmt.Errorf("cli trigger: command %q already registered for workflow %q (cannot re-register for %q)", command, existing, workflowType)
	}
	t.commands[command] = workflowType
	return nil
}

// Start is a no-op for the CLI trigger — CLI commands are dispatched
// synchronously by the application, not by a background goroutine.
func (t *CLITrigger) Start(_ context.Context) error { return nil }

// Stop is a no-op for the CLI trigger.
func (t *CLITrigger) Stop(_ context.Context) error { return nil }

// DispatchCommand invokes TriggerWorkflow for the named CLI command, passing
// the original command name and its arguments as trigger data. The pipeline
// context will expose these values as pc.Current["command"] and
// pc.Current["args"] respectively.
func (t *CLITrigger) DispatchCommand(ctx context.Context, cmd string, args []string) error {
	workflowType, ok := t.commands[cmd]
	if !ok {
		return fmt.Errorf("cli trigger: no pipeline registered for command %q", cmd)
	}
	if t.engine == nil {
		return fmt.Errorf("cli trigger: workflow engine not available (engine was not registered as a service)")
	}
	return t.engine.TriggerWorkflow(ctx, workflowType, "", map[string]any{
		"command": cmd,
		"args":    args,
	})
}

// HasCommand returns true if a pipeline mapping is registered for cmd.
func (t *CLITrigger) HasCommand(cmd string) bool {
	_, ok := t.commands[cmd]
	return ok
}
