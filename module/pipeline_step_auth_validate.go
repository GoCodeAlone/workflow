package module

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/CrisisTextLine/modular"
)

// AuthValidateStep validates a Bearer token against a registered AuthProvider
// module and outputs the claims returned by the provider into the pipeline context.
type AuthValidateStep struct {
	name         string
	authModule   string // service name of the AuthProvider module
	tokenSource  string // dot-path to the token in pipeline context
	subjectField string // output field name for the subject claim
	app          modular.Application
}

// NewAuthValidateStepFactory returns a StepFactory that creates AuthValidateStep instances.
func NewAuthValidateStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		authModule, _ := config["auth_module"].(string)
		if authModule == "" {
			return nil, fmt.Errorf("auth_validate step %q: 'auth_module' is required", name)
		}

		tokenSource, _ := config["token_source"].(string)
		if tokenSource == "" {
			return nil, fmt.Errorf("auth_validate step %q: 'token_source' is required", name)
		}

		subjectField, _ := config["subject_field"].(string)
		if subjectField == "" {
			subjectField = "auth_user_id"
		}

		return &AuthValidateStep{
			name:         name,
			authModule:   authModule,
			tokenSource:  tokenSource,
			subjectField: subjectField,
			app:          app,
		}, nil
	}
}

// Name returns the step name.
func (s *AuthValidateStep) Name() string { return s.name }

// Execute validates the Bearer token and outputs claims from the AuthProvider.
func (s *AuthValidateStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	if s.app == nil {
		return nil, fmt.Errorf("auth_validate step %q: no application context", s.name)
	}

	// 1. Extract the token value from the pipeline context using the configured dot-path.
	rawToken := resolveBodyFrom(s.tokenSource, pc)
	tokenStr, _ := rawToken.(string)
	if tokenStr == "" {
		return s.unauthorizedResponse(pc, "missing or empty authorization header")
	}

	// 2. Strip "Bearer " prefix.
	if !strings.HasPrefix(tokenStr, "Bearer ") {
		return s.unauthorizedResponse(pc, "malformed authorization header")
	}
	token := strings.TrimPrefix(tokenStr, "Bearer ")
	if token == "" {
		return s.unauthorizedResponse(pc, "empty bearer token")
	}

	// 3. Resolve the AuthProvider from the service registry.
	var provider AuthProvider
	if err := s.app.GetService(s.authModule, &provider); err != nil {
		return nil, fmt.Errorf("auth_validate step %q: auth module %q not found: %w", s.name, s.authModule, err)
	}

	// 4. Authenticate the token.
	valid, claims, err := provider.Authenticate(token)
	if err != nil {
		return s.unauthorizedResponse(pc, "authentication error")
	}
	if !valid {
		return s.unauthorizedResponse(pc, "invalid token")
	}

	// 5. Build output: all claims as flat keys + configured subject_field from "sub".
	output := make(map[string]any, len(claims)+1)
	for k, v := range claims {
		output[k] = v
	}
	if sub, ok := claims["sub"]; ok {
		output[s.subjectField] = sub
	}

	return &StepResult{Output: output}, nil
}

// unauthorizedResponse writes a 401 JSON error response and stops the pipeline.
func (s *AuthValidateStep) unauthorizedResponse(pc *PipelineContext, message string) (*StepResult, error) {
	errorBody := map[string]any{
		"error":   "unauthorized",
		"message": message,
	}

	if w, ok := pc.Metadata["_http_response_writer"].(http.ResponseWriter); ok {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(errorBody)
		pc.Metadata["_response_handled"] = true
	}

	return &StepResult{
		Output: map[string]any{
			"status":  http.StatusUnauthorized,
			"error":   "unauthorized",
			"message": message,
		},
		Stop: true,
	}, nil
}
