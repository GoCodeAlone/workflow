package module

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// RequestParseStep extracts path parameters, query parameters, and request body
// from the HTTP request stored in pipeline metadata.
type RequestParseStep struct {
	name        string
	pathParams  []string
	queryParams []string
	parseBody   bool
}

// NewRequestParseStepFactory returns a StepFactory that creates RequestParseStep instances.
func NewRequestParseStepFactory() StepFactory {
	return func(name string, config map[string]any, _ modular.Application) (PipelineStep, error) {
		var pathParams []string
		if pp, ok := config["path_params"]; ok {
			if list, ok := pp.([]any); ok {
				for _, item := range list {
					if s, ok := item.(string); ok {
						pathParams = append(pathParams, s)
					}
				}
			}
		}

		var queryParams []string
		if qp, ok := config["query_params"]; ok {
			if list, ok := qp.([]any); ok {
				for _, item := range list {
					if s, ok := item.(string); ok {
						queryParams = append(queryParams, s)
					}
				}
			}
		}

		parseBody, _ := config["parse_body"].(bool)

		return &RequestParseStep{
			name:        name,
			pathParams:  pathParams,
			queryParams: queryParams,
			parseBody:   parseBody,
		}, nil
	}
}

// Name returns the step name.
func (s *RequestParseStep) Name() string { return s.name }

// Execute extracts path parameters, query parameters, and/or request body
// from the HTTP request stored in pipeline context metadata.
func (s *RequestParseStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	output := make(map[string]any)

	// Extract path parameters using _route_pattern and actual request path
	if len(s.pathParams) > 0 {
		pathParamValues := make(map[string]any)

		routePattern, _ := pc.Metadata["_route_pattern"].(string)
		req, _ := pc.Metadata["_http_request"].(*http.Request)

		if routePattern != "" && req != nil {
			actualPath := req.URL.Path
			patternParts := strings.Split(strings.Trim(routePattern, "/"), "/")
			actualParts := strings.Split(strings.Trim(actualPath, "/"), "/")

			// Build a map of param name -> value by matching {param} segments
			paramMap := make(map[string]string)
			for i, pp := range patternParts {
				if strings.HasPrefix(pp, "{") && strings.HasSuffix(pp, "}") {
					paramName := pp[1 : len(pp)-1]
					if i < len(actualParts) {
						paramMap[paramName] = actualParts[i]
					}
				}
			}

			// Extract only requested path params
			for _, name := range s.pathParams {
				if val, ok := paramMap[name]; ok {
					pathParamValues[name] = val
				}
			}
		}
		output["path_params"] = pathParamValues
	}

	// Extract query parameters
	if len(s.queryParams) > 0 {
		queryValues := make(map[string]any)
		req, _ := pc.Metadata["_http_request"].(*http.Request)
		if req != nil {
			q := req.URL.Query()
			for _, name := range s.queryParams {
				if val := q.Get(name); val != "" {
					queryValues[name] = val
				}
			}
		}
		output["query"] = queryValues
	}

	// Parse request body — first try trigger data (command handler pre-parses body),
	// then fall back to reading from the HTTP request directly
	if s.parseBody {
		if body, ok := pc.TriggerData["body"].(map[string]any); ok {
			output["body"] = body
		} else if body, ok := pc.Current["body"].(map[string]any); ok {
			output["body"] = body
		} else {
			req, _ := pc.Metadata["_http_request"].(*http.Request)
			if req != nil && req.Body != nil {
				bodyBytes, err := io.ReadAll(req.Body)
				if err == nil && len(bodyBytes) > 0 {
					var bodyData map[string]any
					if json.Unmarshal(bodyBytes, &bodyData) == nil {
						output["body"] = bodyData
					}
				}
			}
		}
	}

	return &StepResult{Output: output}, nil
}
