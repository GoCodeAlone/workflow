package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/GoCodeAlone/modular"
)

// AuthzCheckStep evaluates a policy engine decision for the current pipeline
// subject. On denial it writes a 403 Forbidden JSON response to the HTTP
// response writer (when present) and stops the pipeline, matching the
// pattern used by step.auth_validate for 401 responses.
type AuthzCheckStep struct {
	name         string
	engineName   string // service name of the PolicyEngineModule
	subjectField string // field in pc.Current that holds the subject
	inputFrom    string // optional: field in pc.Current to use as policy input
	app          modular.Application
}

// NewAuthzCheckStepFactory returns a StepFactory that creates AuthzCheckStep instances.
func NewAuthzCheckStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		engineName, _ := config["policy_engine"].(string)
		if engineName == "" {
			return nil, fmt.Errorf("authz_check step %q: 'policy_engine' is required", name)
		}

		subjectField, _ := config["subject_field"].(string)
		if subjectField == "" {
			subjectField = "subject"
		}

		inputFrom, _ := config["input_from"].(string)

		return &AuthzCheckStep{
			name:         name,
			engineName:   engineName,
			subjectField: subjectField,
			inputFrom:    inputFrom,
			app:          app,
		}, nil
	}
}

// Name returns the step name.
func (s *AuthzCheckStep) Name() string { return s.name }

// Execute evaluates the policy engine and writes a 403 response on denial.
func (s *AuthzCheckStep) Execute(ctx context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("authz_check step %q: no application context", s.name)
	}

	// Resolve the PolicyEngineModule from the service registry.
	eng, err := resolvePolicyEngine(s.app, s.engineName, s.name)
	if err != nil {
		return nil, err
	}

	// Build the policy input: use a named field if configured, otherwise use
	// the full pipeline context (same strategy as step.policy_evaluate).
	// Track whether the input shares the same backing data as pc.Current so we
	// can clone before adding the subject key.
	var input map[string]any
	inputIsShared := false
	if s.inputFrom != "" {
		if raw, ok := pc.Current[s.inputFrom]; ok {
			if m, ok := raw.(map[string]any); ok {
				input = m
			}
		}
	}
	if input == nil {
		input = pc.Current
		inputIsShared = true
	}

	// Map the configured subject field into the policy input so that
	// authorization decisions can depend on it. We read the subject from
	// pc.Current[s.subjectField] and expose it under the canonical "subject"
	// key in the input. Clone the input first when it shares data with
	// pc.Current to avoid side effects on the pipeline context.
	if s.subjectField != "" && s.subjectField != "subject" {
		if subj, ok := pc.Current[s.subjectField]; ok {
			if inputIsShared {
				cloned := make(map[string]any, len(input)+1)
				for k, v := range input {
					cloned[k] = v
				}
				input = cloned
			}
			input["subject"] = subj
		}
	}

	// Evaluate the policy.
	decision, err := eng.Engine().Evaluate(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("authz_check step %q: evaluate: %w", s.name, err)
	}

	if !decision.Allowed {
		reason := "authorization denied"
		if len(decision.Reasons) > 0 {
			reason = decision.Reasons[0]
		}
		return s.forbiddenResponse(pc, reason)
	}

	return &StepResult{Output: map[string]any{
		"allowed":  true,
		"reasons":  decision.Reasons,
		"metadata": decision.Metadata,
	}}, nil
}

// forbiddenResponse writes a 403 JSON error response to the HTTP response
// writer (when present) and stops the pipeline. The response body format
// matches the expected {"error":"forbidden: ..."} shape described in the issue.
func (s *AuthzCheckStep) forbiddenResponse(pc *PipelineContext, message string) (*StepResult, error) {
	errorMsg := fmt.Sprintf("forbidden: %s", message)
	errorBody := map[string]any{
		"error": errorMsg,
	}

	if w, ok := pc.Metadata["_http_response_writer"].(http.ResponseWriter); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(errorBody)
		pc.Metadata["_response_handled"] = true
	}

	return &StepResult{
		Output: map[string]any{
			"response_status": http.StatusForbidden,
			"response_body":   fmt.Sprintf(`{"error":%q}`, errorMsg),
			"response_headers": map[string]string{
				"Content-Type": "application/json",
			},
			"error": errorMsg,
		},
		Stop: true,
	}, nil
}
