package module

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

func boundaryToken(t *testing.T, issuer string, audience any) string {
	t.Helper()
	claims := jwt.MapClaims{
		"sub": "boundary-user", "email": "boundary@example.invalid", "type": "refresh",
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	}
	if issuer != "" {
		claims["iss"] = issuer
	}
	if audience != nil {
		claims["aud"] = audience
	}
	token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(strings.Repeat("boundary-fixture-", 3)))
	if err != nil {
		t.Fatal("signing synthetic token failed")
	}
	return token
}

func boundaryModule(issuer, audience string) *JWTAuthModule {
	m := NewJWTAuthModule("auth-jwt", strings.Repeat("boundary-fixture-", 3), time.Hour, issuer)
	if audience != "" {
		m.SetAudience(audience)
	}
	m.users["boundary@example.invalid"] = &User{ID: "boundary-user", Email: "boundary@example.invalid"}
	return m
}

func TestJWTAuthEnvironmentBoundary(t *testing.T) {
	for _, environment := range []string{"staging", "production"} {
		t.Run(environment, func(t *testing.T) {
			issuer := "buymywishlist:" + environment
			audience := "bmw:" + environment
			other := "production"
			if environment == "production" {
				other = "staging"
			}
			m := boundaryModule(issuer, audience)
			for _, tc := range []struct {
				name, issuer string
				audience     any
				valid        bool
			}{
				{"matching", issuer, audience, true},
				{"audience-array-matching", issuer, []string{audience}, true},
				{"other-environment", "buymywishlist:" + other, "bmw:" + other, false},
				{"wrong-issuer", "buymywishlist:" + other, audience, false},
				{"wrong-audience", issuer, "bmw:" + other, false},
				{"missing-issuer", "", audience, false},
				{"missing-audience", issuer, nil, false},
				{"empty-audience-array", issuer, []string{}, false},
				{"malformed-audience", issuer, 123, false},
				{"legacy", "buymywishlist", nil, false},
			} {
				t.Run(tc.name, func(t *testing.T) {
					token := boundaryToken(t, tc.issuer, tc.audience)
					valid, claims, err := m.Authenticate(token)
					if err != nil || valid != tc.valid || (!tc.valid && claims != nil) {
						t.Fatal("unexpected authentication boundary result")
					}

					// Profile parsing and refresh must use the same boundary as Authenticate.
					req := httptest.NewRequest(http.MethodGet, "/auth/profile", nil)
					req.Header.Set("Authorization", "Bearer "+token)
					user, profileErr := m.extractUserFromRequest(req)
					if (profileErr == nil && user != nil) != tc.valid {
						t.Fatal("profile entry point bypassed the boundary")
					}
					body, err := json.Marshal(map[string]string{"refresh_token": token})
					if err != nil {
						t.Fatal("encoding synthetic request failed")
					}
					refresh := httptest.NewRecorder()
					m.handleRefresh(refresh, httptest.NewRequest(http.MethodPost, "/auth/refresh", bytes.NewReader(body)))
					wantStatus := http.StatusUnauthorized
					if tc.valid {
						wantStatus = http.StatusOK
					}
					if refresh.Code != wantStatus {
						t.Fatalf("refresh status = %d; want %d", refresh.Code, wantStatus)
					}
				})
			}
		})
	}
}

func TestJWTAuthEnvironmentBoundaryGeneratedTokens(t *testing.T) {
	m := boundaryModule("buymywishlist:staging", "bmw:staging")
	user := m.users["boundary@example.invalid"]
	for _, mint := range []func(*User) (string, error){m.generateToken, m.generateRefreshToken} {
		token, err := mint(user)
		if err != nil {
			t.Fatal("minting synthetic token failed")
		}
		valid, claims, err := m.Authenticate(token)
		if err != nil || !valid || claims["iss"] != "buymywishlist:staging" || claims["aud"] != "bmw:staging" {
			t.Fatal("generated token lacks enforced environment claims")
		}
	}
}

func TestJWTAuthEnvironmentBoundaryLegacyCompatibility(t *testing.T) {
	m := boundaryModule("legacy-configured-issuer", "")
	valid, _, err := m.Authenticate(boundaryToken(t, "different-legacy-issuer", nil))
	if err != nil || !valid {
		t.Fatal("unconfigured audience changed legacy validation")
	}
}

func TestJWTAuthEnvironmentBoundaryEmptyAudienceFailsClosed(t *testing.T) {
	for _, audience := range []string{"", "   "} {
		m := boundaryModule("buymywishlist:staging", "")
		m.SetAudience(audience)
		if err := m.Init(NewMockApplication()); err == nil {
			t.Fatal("empty configured audience did not fail initialization")
		}
		valid, _, err := m.Authenticate(boundaryToken(t, "buymywishlist:staging", nil))
		if err != nil || valid {
			t.Fatal("empty configured audience accepted a legacy token")
		}
		for _, mint := range []func(*User) (string, error){m.generateToken, m.generateRefreshToken} {
			if token, err := mint(m.users["boundary@example.invalid"]); err == nil || token != "" {
				t.Fatal("empty configured audience minted a token")
			}
		}
	}
}

func TestAuthValidateStepJWTEnvironmentBoundary(t *testing.T) {
	m := boundaryModule("buymywishlist:staging", "bmw:staging")
	app := newTestAuthApp("auth-jwt", m)
	step, err := NewAuthValidateStepFactory()("authorize", map[string]any{
		"auth_module": "auth-jwt", "token_source": "steps.request.headers.Authorization",
	}, app)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, issuer, audience string
		stop                   bool
	}{
		{"same-environment", "buymywishlist:staging", "bmw:staging", false},
		{"other-environment", "buymywishlist:production", "bmw:production", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			pc := NewPipelineContext(nil, nil)
			pc.MergeStepOutput("request", map[string]any{"headers": map[string]any{
				"Authorization": "Bearer " + boundaryToken(t, tc.issuer, tc.audience),
			}})
			response := httptest.NewRecorder()
			pc.Metadata["_http_response_writer"] = response
			result, err := step.Execute(context.Background(), pc)
			if err != nil || result.Stop != tc.stop {
				t.Fatal("unexpected pipeline authentication result")
			}
			if tc.stop && (response.Code != http.StatusUnauthorized || result.Output["status"] != http.StatusUnauthorized || pc.Metadata["_response_handled"] != true) {
				t.Fatal("cross-environment request did not stop with HTTP 401")
			}
		})
	}
}
