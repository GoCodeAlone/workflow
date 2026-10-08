package auth

import (
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/golang-jwt/jwt/v5"
)

func TestJWTFactoryEnvironmentBoundary(t *testing.T) {
	secret := strings.Repeat("boundary-fixture-", 3)
	m, ok := New().ModuleFactories()["auth.jwt"]("auth-jwt", map[string]any{
		"secret": secret, "issuer": "buymywishlist:staging", "audience": "bmw:staging",
	}).(*module.JWTAuthModule)
	if !ok {
		t.Fatal("factory returned the wrong module")
	}
	for _, audience := range []string{"bmw:staging", "bmw:production"} {
		claims := jwt.MapClaims{"iss": "buymywishlist:staging", "aud": audience, "exp": time.Now().Add(time.Hour).Unix()}
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
		if err != nil {
			t.Fatal("signing synthetic token failed")
		}
		valid, _, err := m.Authenticate(token)
		if err != nil || valid != (audience == "bmw:staging") {
			t.Fatal("factory did not enable the configured boundary")
		}
	}
}

func TestJWTFactoryInvalidAudienceFailsClosed(t *testing.T) {
	secret := strings.Repeat("boundary-fixture-", 3)
	for _, audience := range []any{"", "   ", 123, nil, []string{"bmw:staging"}} {
		m, ok := New().ModuleFactories()["auth.jwt"]("auth-jwt", map[string]any{
			"secret": secret, "issuer": "buymywishlist:staging", "audience": audience,
		}).(*module.JWTAuthModule)
		if !ok {
			t.Fatal("factory returned the wrong module")
		}
		claims := jwt.MapClaims{"iss": "buymywishlist:staging", "exp": time.Now().Add(time.Hour).Unix()}
		token, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
		if err != nil {
			t.Fatal("signing synthetic token failed")
		}
		valid, _, err := m.Authenticate(token)
		if err != nil || valid {
			t.Fatal("invalid configured audience selected legacy validation")
		}
	}
}
