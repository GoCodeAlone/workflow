package module

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/GoCodeAlone/modular"
)

// JSONParseStep parses a JSON string value from the pipeline context into a
// structured Go value (map, slice, etc.) and stores the result as step output.
//
// This is useful when a pipeline step (e.g. step.db_query against a legacy
// driver, or step.http_call) returns a JSON column/field as a raw string rather
// than as a pre-parsed Go type. It is the explicit counterpart to the automatic
// json/jsonb detection that step.db_query performs for the pgx driver.
//
// Configuration:
//
//	source: "steps.fetch.row.json_column"  # dot-path to the JSON string value (required)
//	target: "parsed_data"                  # output key name (optional, defaults to "value")
type JSONParseStep struct {
	name   string
	source string
	target string
}

// NewJSONParseStepFactory returns a StepFactory that creates JSONParseStep instances.
func NewJSONParseStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		source, _ := config["source"].(string)
		if source == "" {
			return nil, fmt.Errorf("json_parse step %q: 'source' is required", name)
		}

		target, _ := config["target"].(string)
		if target == "" {
			target = "value"
		}

		return &JSONParseStep{
			name:   name,
			source: source,
			target: target,
		}, nil
	}
}

// Name returns the step name.
func (s *JSONParseStep) Name() string { return s.name }

// Execute resolves the source path, parses the value as JSON if it is a string,
// and stores the result under the configured target key.
func (s *JSONParseStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	raw := resolveBodyFrom(s.source, pc)
	if raw == nil {
		return nil, fmt.Errorf("json_parse step %q: source %q not found or resolved to nil", s.name, s.source)
	}

	var parsed any
	switch v := raw.(type) {
	case string:
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return nil, fmt.Errorf("json_parse step %q: failed to parse JSON from %q: %w", s.name, s.source, err)
		}
	case []byte:
		if err := json.Unmarshal(v, &parsed); err != nil {
			return nil, fmt.Errorf("json_parse step %q: failed to parse JSON bytes from %q: %w", s.name, s.source, err)
		}
	default:
		// Value is already a structured type (map, slice, number, bool, nil).
		// Pass it through unchanged so that pipelines are idempotent when the
		// upstream step already returns a parsed value (e.g. after the db_query
		// fix lands, json_parse is a no-op for json/jsonb columns).
		parsed = raw
	}

	return &StepResult{Output: map[string]any{
		s.target: parsed,
	}}, nil
}
