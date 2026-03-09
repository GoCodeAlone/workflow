package module

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/GoCodeAlone/modular"
)

// CLIPrintStep writes a template-resolved message to stdout or stderr.
// Use this step to produce output in workflow-powered CLI applications.
//
// Step type: step.cli_print
//
// Config fields:
//   - message  (required) — Go template expression resolved against the pipeline
//     context (e.g. "PASS {{.config_file}}")
//   - newline  (optional, default true) — append a trailing newline
//   - target   (optional, "stdout" | "stderr", default "stdout")
type CLIPrintStep struct {
	name    string
	message string
	newline bool
	target  io.Writer
	tmpl    *TemplateEngine
}

// NewCLIPrintStepFactory returns a StepFactory that creates CLIPrintStep instances.
func NewCLIPrintStepFactory() StepFactory {
	return newCLIPrintStepFactoryWithWriters(os.Stdout, os.Stderr)
}

// newCLIPrintStepFactoryWithWriters is the testable variant that accepts explicit writers.
func newCLIPrintStepFactoryWithWriters(stdout, stderr io.Writer) StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		message, _ := config["message"].(string)
		if message == "" {
			return nil, fmt.Errorf("cli_print step %q: 'message' is required", name)
		}

		newline := true
		if nl, ok := config["newline"].(bool); ok {
			newline = nl
		}

		target := stdout
		if rawTarget, ok := config["target"]; ok {
			if t, ok := rawTarget.(string); ok && t != "" {
				switch t {
				case "stdout":
					target = stdout
				case "stderr":
					target = stderr
				default:
					return nil, fmt.Errorf("cli_print step %q: invalid 'target' %q; must be \"stdout\" or \"stderr\"", name, t)
				}
			}
		}

		return &CLIPrintStep{
			name:    name,
			message: message,
			newline: newline,
			target:  target,
			tmpl:    NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *CLIPrintStep) Name() string { return s.name }

// Execute resolves the message template and writes it to the configured target.
// It sets "printed" in the step output to the resolved message string.
func (s *CLIPrintStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	resolved, err := s.tmpl.Resolve(s.message, pc)
	if err != nil {
		return nil, fmt.Errorf("cli_print step %q: failed to resolve message: %w", s.name, err)
	}

	if s.newline {
		fmt.Fprintln(s.target, resolved)
	} else {
		fmt.Fprint(s.target, resolved)
	}

	return &StepResult{Output: map[string]any{"printed": resolved}}, nil
}
