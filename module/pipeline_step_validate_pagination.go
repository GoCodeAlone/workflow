package module

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/CrisisTextLine/modular"
)

// ValidatePaginationStep validates and normalises page/limit query parameters.
// It reads from the HTTP request in pipeline metadata and outputs resolved
// pagination values with defaults applied.
type ValidatePaginationStep struct {
	name         string
	maxLimit     int
	defaultLimit int
	defaultPage  int
}

// NewValidatePaginationStepFactory returns a StepFactory that creates ValidatePaginationStep instances.
func NewValidatePaginationStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		maxLimit := 100
		if v, ok := config["max_limit"]; ok {
			switch val := v.(type) {
			case int:
				maxLimit = val
			case float64:
				maxLimit = int(val)
			}
		}
		if maxLimit <= 0 {
			return nil, fmt.Errorf("validate_pagination step %q: max_limit must be positive", name)
		}

		defaultLimit := 20
		if v, ok := config["default_limit"]; ok {
			switch val := v.(type) {
			case int:
				defaultLimit = val
			case float64:
				defaultLimit = int(val)
			}
		}

		defaultPage := 1
		if v, ok := config["default_page"]; ok {
			switch val := v.(type) {
			case int:
				defaultPage = val
			case float64:
				defaultPage = int(val)
			}
		}

		return &ValidatePaginationStep{
			name:         name,
			maxLimit:     maxLimit,
			defaultLimit: defaultLimit,
			defaultPage:  defaultPage,
		}, nil
	}
}

// Name returns the step name.
func (s *ValidatePaginationStep) Name() string { return s.name }

// Execute reads page and limit query parameters from the HTTP request,
// validates their ranges, applies defaults, and outputs the resolved values.
func (s *ValidatePaginationStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	page := s.defaultPage
	limit := s.defaultLimit

	req, _ := pc.Metadata["_http_request"].(*http.Request)
	if req != nil {
		q := req.URL.Query()

		if pageStr := q.Get("page"); pageStr != "" {
			p, err := strconv.Atoi(pageStr)
			if err != nil || p < 1 {
				return nil, fmt.Errorf("validate_pagination step %q: invalid page parameter %q — must be a positive integer", s.name, pageStr)
			}
			page = p
		}

		if limitStr := q.Get("limit"); limitStr != "" {
			l, err := strconv.Atoi(limitStr)
			if err != nil || l < 1 {
				return nil, fmt.Errorf("validate_pagination step %q: invalid limit parameter %q — must be a positive integer", s.name, limitStr)
			}
			if l > s.maxLimit {
				return nil, fmt.Errorf("validate_pagination step %q: limit %d exceeds maximum %d", s.name, l, s.maxLimit)
			}
			limit = l
		}
	}

	offset := (page - 1) * limit

	return &StepResult{
		Output: map[string]any{
			"page":   page,
			"limit":  limit,
			"offset": offset,
		},
	}, nil
}
