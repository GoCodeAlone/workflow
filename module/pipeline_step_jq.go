package module

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/CrisisTextLine/modular"
	"github.com/itchyny/gojq"
)

// JQStep applies JQ expressions to pipeline data for complex transformations.
// It uses the gojq library (a pure-Go JQ implementation) to support the full
// JQ expression language including field access, pipes, map/select, object
// construction, arithmetic, conditionals, and more.
type JQStep struct {
	name       string
	expression string
	inputFrom  string // optional dotted path for custom input
	query      *gojq.Code
	app        modular.Application
}

// NewJQStepFactory returns a StepFactory that creates JQStep instances.
func NewJQStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		expression, _ := config["expression"].(string)
		if expression == "" {
			return nil, fmt.Errorf("jq step %q: 'expression' is required", name)
		}

		// Parse and compile the JQ expression at construction time so syntax
		// errors are caught early rather than at execution time.
		parsed, err := gojq.Parse(expression)
		if err != nil {
			return nil, fmt.Errorf("jq step %q: invalid expression %q: %w", name, expression, err)
		}

		code, err := gojq.Compile(parsed)
		if err != nil {
			return nil, fmt.Errorf("jq step %q: failed to compile expression %q: %w", name, expression, err)
		}

		inputFrom, _ := config["input_from"].(string)

		return &JQStep{
			name:       name,
			expression: expression,
			inputFrom:  inputFrom,
			query:      code,
			app:        app,
		}, nil
	}
}

// Name returns the step name.
func (s *JQStep) Name() string { return s.name }

// Execute applies the compiled JQ expression to the pipeline context's current
// data and returns the result. If input_from is configured, the expression is
// applied to the value at that path instead of the full current map.
func (s *JQStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	// Determine the input data for the JQ expression.
	input, err := s.resolveInput(pc)
	if err != nil {
		return nil, fmt.Errorf("jq step %q: %w", s.name, err)
	}

	// gojq operates on JSON-compatible Go types (map[string]any, []any, etc.).
	// The pipeline context already uses these types, but nested structs or
	// typed values might cause issues. Round-trip through JSON to normalize.
	normalized, err := normalizeForJQ(input)
	if err != nil {
		return nil, fmt.Errorf("jq step %q: failed to normalize input: %w", s.name, err)
	}

	// Run the JQ query. Iter yields all results; we collect them.
	iter := s.query.Run(normalized)
	var results []any
	for {
		v, ok := iter.Next()
		if !ok {
			break
		}
		if err, isErr := v.(error); isErr {
			return nil, fmt.Errorf("jq step %q: expression error: %w", s.name, err)
		}
		results = append(results, v)
	}

	// Build the step output.
	output := make(map[string]any)
	switch len(results) {
	case 0:
		output["result"] = nil
	case 1:
		output["result"] = results[0]
		// If the single result is a map, also merge its keys into the output
		// so downstream steps can access fields directly.
		if m, ok := results[0].(map[string]any); ok {
			for k, v := range m {
				output[k] = v
			}
		}
	default:
		output["result"] = results
	}

	return &StepResult{Output: output}, nil
}

// resolveInput determines the input value for the JQ expression.
// If input_from is set, it traverses the dotted path through pc.Current
// and pc.StepOutputs (supporting "steps.<name>.<field>" paths).
func (s *JQStep) resolveInput(pc *PipelineContext) (any, error) {
	if s.inputFrom == "" {
		return pc.Current, nil
	}

	// Build a data map that includes step outputs under "steps" key,
	// matching the template engine convention.
	data := make(map[string]any)
	for k, v := range pc.Current {
		data[k] = v
	}
	if len(pc.StepOutputs) > 0 {
		// Convert map[string]map[string]any to map[string]any so
		// resolveDottedPath can traverse it uniformly.
		steps := make(map[string]any, len(pc.StepOutputs))
		for k, v := range pc.StepOutputs {
			steps[k] = v
		}
		data["steps"] = steps
	}

	// Walk the dotted path.
	return resolveDottedPath(data, s.inputFrom)
}

// resolveDottedPath traverses a dotted key path (e.g. "steps.fetch.items")
// through nested maps, returning the value at the end of the path.
func resolveDottedPath(data any, path string) (any, error) {
	current := data
	start := 0
	for i := 0; i <= len(path); i++ {
		if i == len(path) || path[i] == '.' {
			key := path[start:i]
			if key == "" {
				start = i + 1
				continue
			}
			m, ok := current.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("cannot traverse into non-map at %q", path[:i])
			}
			val, exists := m[key]
			if !exists {
				return nil, fmt.Errorf("key %q not found at path %q", key, path[:i])
			}
			current = val
			start = i + 1
		}
	}
	return current, nil
}

// normalizeForJQ converts an arbitrary Go value into JSON-compatible types
// that gojq can process. It does this via JSON marshal/unmarshal round-trip.
func normalizeForJQ(v any) (any, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var result any
	if err := json.Unmarshal(b, &result); err != nil {
		return nil, err
	}
	return result, nil
}
