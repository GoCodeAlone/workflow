package module

import (
	"context"
	"fmt"
	"regexp"

	"github.com/CrisisTextLine/modular"
)

// RegexMatchStep matches a regular expression against a template-resolved input string.
type RegexMatchStep struct {
	name    string
	pattern *regexp.Regexp
	input   string
	app     modular.Application
	tmpl    *TemplateEngine
}

// NewRegexMatchStepFactory returns a StepFactory that creates RegexMatchStep instances.
func NewRegexMatchStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		patternStr, _ := config["pattern"].(string)
		if patternStr == "" {
			return nil, fmt.Errorf("regex_match step %q: 'pattern' is required", name)
		}

		compiled, err := regexp.Compile(patternStr)
		if err != nil {
			return nil, fmt.Errorf("regex_match step %q: invalid pattern %q: %w", name, patternStr, err)
		}

		input, _ := config["input"].(string)
		if input == "" {
			return nil, fmt.Errorf("regex_match step %q: 'input' is required", name)
		}

		return &RegexMatchStep{
			name:    name,
			pattern: compiled,
			input:   input,
			app:     app,
			tmpl:    NewTemplateEngine(),
		}, nil
	}
}

// Name returns the step name.
func (s *RegexMatchStep) Name() string { return s.name }

// Execute resolves the input template, runs the regex match, and returns the result.
func (s *RegexMatchStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	resolved, err := s.tmpl.Resolve(s.input, pc)
	if err != nil {
		return nil, fmt.Errorf("regex_match step %q: failed to resolve input: %w", s.name, err)
	}

	submatches := s.pattern.FindStringSubmatch(resolved)
	matched := len(submatches) > 0

	output := map[string]any{
		"matched": matched,
		"match":   "",
		"groups":  []string{},
	}

	if matched {
		output["match"] = submatches[0]
		if len(submatches) > 1 {
			output["groups"] = submatches[1:]
		}
	}

	return &StepResult{Output: output}, nil
}
