package module

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/CrisisTextLine/modular"
)

// LogStep logs a template-resolved message at a specified level.
type LogStep struct {
	name    string
	level   string
	message string
	tmpl    *TemplateEngine
}

// NewLogStepFactory returns a StepFactory that creates LogStep instances.
func NewLogStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		level, _ := config["level"].(string)
		if level == "" {
			level = "info"
		}

		switch level {
		case "debug", "info", "warn", "error":
			// valid
		default:
			return nil, fmt.Errorf("log step %q: invalid level %q (expected debug, info, warn, or error)", name, level)
		}

		message, _ := config["message"].(string)
		if message == "" {
			return nil, fmt.Errorf("log step %q: 'message' is required", name)
		}

		return &LogStep{
			name:    name,
			level:   level,
			message: message,
			tmpl:    NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *LogStep) Name() string { return s.name }

// Execute resolves the message template and logs it at the configured level.
func (s *LogStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	resolved, err := s.tmpl.Resolve(s.message, pc)
	if err != nil {
		return nil, fmt.Errorf("log step %q: failed to resolve message: %w", s.name, err)
	}

	switch s.level {
	case "debug":
		slog.Debug(resolved, "step", s.name)
	case "info":
		slog.Info(resolved, "step", s.name)
	case "warn":
		slog.Warn(resolved, "step", s.name)
	case "error":
		slog.Error(resolved, "step", s.name)
	}

	return &StepResult{Output: map[string]any{}}, nil
}
