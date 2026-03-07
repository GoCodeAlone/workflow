package module

import (
	"context"
	"fmt"

	"github.com/CrisisTextLine/modular"
)

// CLIInvokeStep calls a registered Go CLI command function from within a
// pipeline. It looks up the CLICommandRegistry service in the application
// and invokes the function registered under the configured command name.
//
// Step type: step.cli_invoke
//
// Config fields:
//   - command  (required) — the command name as registered in CLICommandRegistry
//
// Pipeline context inputs:
//   - args  ([]string) — forwarded as the args parameter to the Go function;
//     passed from the CLI trigger data as pc.Current["args"]
//
// Step output:
//   - command  (string) — the command name that was executed
//   - success  (bool)   — always true on success (error path returns a non-nil error)
type CLIInvokeStep struct {
	name        string
	commandName string
	app         modular.Application
}

// NewCLIInvokeStepFactory returns a StepFactory that creates CLIInvokeStep instances.
func NewCLIInvokeStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		commandName, _ := config["command"].(string)
		if commandName == "" {
			return nil, fmt.Errorf("cli_invoke step %q: 'command' is required", name)
		}
		return &CLIInvokeStep{
			name:        name,
			commandName: commandName,
			app:         app,
		}, nil
	}
}

// Name returns the step name.
func (s *CLIInvokeStep) Name() string { return s.name }

// Execute resolves the CLICommandRegistry service, looks up the configured
// command, extracts args from the pipeline context, and calls the function.
func (s *CLIInvokeStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	registry, err := s.resolveRegistry()
	if err != nil {
		return nil, fmt.Errorf("cli_invoke step %q: %w", s.name, err)
	}

	fn, ok := registry.Get(s.commandName)
	if !ok {
		return nil, fmt.Errorf("cli_invoke step %q: no runner registered for command %q", s.name, s.commandName)
	}

	// Extract args from the pipeline context (injected by CLITrigger.DispatchCommand).
	// The value may be []string (from a direct Go call) or []any (when trigger
	// data was round-tripped through JSON/YAML decoding). Both are accepted.
	var args []string
	switch v := pc.Current["args"].(type) {
	case []string:
		args = v
	case []any:
		args = make([]string, 0, len(v))
		for i, a := range v {
			str, ok := a.(string)
			if !ok {
				return nil, fmt.Errorf("cli_invoke step %q: args[%d] is not a string (got %T)", s.name, i, a)
			}
			args = append(args, str)
		}
	}

	if err := fn(args); err != nil {
		return nil, fmt.Errorf("cli_invoke step %q (command %q): %w", s.name, s.commandName, err)
	}

	return &StepResult{Output: map[string]any{
		"command": s.commandName,
		"success": true,
	}}, nil
}

// resolveRegistry returns the CLICommandRegistry from the app service registry.
// It first tries the well-known service name, then falls back to scanning all
// services for a *CLICommandRegistry value.
func (s *CLIInvokeStep) resolveRegistry() (*CLICommandRegistry, error) {
	if s.app == nil {
		return nil, fmt.Errorf("application is nil; CLICommandRegistry cannot be resolved")
	}

	var registry *CLICommandRegistry
	if err := s.app.GetService(CLICommandRegistryServiceName, &registry); err == nil && registry != nil {
		return registry, nil
	}

	// Fallback: scan all services.
	for _, svc := range s.app.SvcRegistry() {
		if r, ok := svc.(*CLICommandRegistry); ok {
			return r, nil
		}
	}

	return nil, fmt.Errorf("CLICommandRegistry service %q not found in app — "+
		"register one via app.RegisterService(%q, module.NewCLICommandRegistry()) "+
		"before calling engine.BuildFromConfig",
		CLICommandRegistryServiceName, CLICommandRegistryServiceName)
}
