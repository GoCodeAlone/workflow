package module

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/golang-jwt/jwt/v5"
)

// TokenRevokeStep extracts a JWT from the pipeline context, reads its jti and
// exp claims without signature validation, and adds the JTI to the configured
// token blacklist module.
type TokenRevokeStep struct {
	name            string
	blacklistModule string // service name of the TokenBlacklist module
	tokenSource     string // dot-path to the token in pipeline context
	app             modular.Application
}

// NewTokenRevokeStepFactory returns a StepFactory for step.token_revoke.
func NewTokenRevokeStepFactory() StepFactory {
	return func(name string, config map[string]any, app modular.Application) (PipelineStep, error) {
		blacklistModule, _ := config["blacklist_module"].(string)
		if blacklistModule == "" {
			return nil, fmt.Errorf("token_revoke step %q: 'blacklist_module' is required", name)
		}

		tokenSource, _ := config["token_source"].(string)
		if tokenSource == "" {
			return nil, fmt.Errorf("token_revoke step %q: 'token_source' is required", name)
		}

		return &TokenRevokeStep{
			name:            name,
			blacklistModule: blacklistModule,
			tokenSource:     tokenSource,
			app:             app,
		}, nil
	}
}

// Name returns the step name.
func (s *TokenRevokeStep) Name() string { return s.name }

// Execute revokes the JWT by extracting its JTI and adding it to the blacklist.
func (s *TokenRevokeStep) Execute(_ context.Context, pc *PipelineContext) (*StepResult, error) {
	// 1. Extract token string from pipeline context.
	rawToken := resolveBodyFrom(s.tokenSource, pc)
	tokenStr, _ := rawToken.(string)
	if tokenStr == "" {
		return &StepResult{Output: map[string]any{"revoked": false, "error": "missing token"}}, nil
	}

	// 2. Strip "Bearer " prefix if present.
	tokenStr = strings.TrimPrefix(tokenStr, "Bearer ")

	// 3. Parse claims without signature validation (token is being revoked, not authenticated).
	parser := jwt.NewParser()
	token, _, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return &StepResult{Output: map[string]any{"revoked": false, "error": "invalid token format"}}, nil
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return &StepResult{Output: map[string]any{"revoked": false, "error": "invalid claims"}}, nil
	}

	jti, _ := claims["jti"].(string)
	if jti == "" {
		return &StepResult{Output: map[string]any{"revoked": false, "error": "token has no jti claim"}}, nil
	}

	// 4. Determine token expiry from exp claim.
	var expiresAt time.Time
	switch exp := claims["exp"].(type) {
	case float64:
		expiresAt = time.Unix(int64(exp), 0)
	default:
		expiresAt = time.Now().Add(24 * time.Hour) // safe fallback
	}

	// 5. Resolve the blacklist module and add the JTI.
	var blacklist TokenBlacklist
	if err := s.app.GetService(s.blacklistModule, &blacklist); err != nil {
		return nil, fmt.Errorf("token_revoke step %q: blacklist module %q not found: %w", s.name, s.blacklistModule, err)
	}

	blacklist.Add(jti, expiresAt)

	return &StepResult{Output: map[string]any{"revoked": true, "jti": jti}}, nil
}
