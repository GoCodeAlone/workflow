package module

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// makeTestJWT creates a signed JWT with the given claims using HS256.
func makeTestJWT(t *testing.T, secret string, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	s, err := tok.SignedString([]byte(secret))
	if err != nil {
		t.Fatalf("makeTestJWT: %v", err)
	}
	return s
}

// mockBlacklist is a simple in-memory TokenBlacklist for testing.
type mockBlacklist struct {
	entries map[string]time.Time
}

func newMockBlacklist() *mockBlacklist {
	return &mockBlacklist{entries: make(map[string]time.Time)}
}

func (m *mockBlacklist) Add(jti string, expiresAt time.Time) {
	m.entries[jti] = expiresAt
}

func (m *mockBlacklist) IsBlacklisted(jti string) bool {
	exp, ok := m.entries[jti]
	return ok && time.Now().Before(exp)
}

func newTokenRevokeApp(blName string, bl TokenBlacklist) *MockApplication {
	app := NewMockApplication()
	app.Services[blName] = bl
	return app
}

func TestTokenRevokeStep_RevokesToken(t *testing.T) {
	bl := newMockBlacklist()
	app := newTokenRevokeApp("my-blacklist", bl)

	factory := NewTokenRevokeStepFactory()
	step, err := factory("revoke", map[string]any{
		"blacklist_module": "my-blacklist",
		"token_source":     "steps.parse.authorization",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	tokenStr := makeTestJWT(t, "aaaabbbbccccddddeeeeffffgggghhhh", jwt.MapClaims{
		"jti": "test-jti-123",
		"sub": "user-1",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{
		"authorization": "Bearer " + tokenStr,
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["revoked"] != true {
		t.Errorf("expected revoked=true, got %v", result.Output["revoked"])
	}
	if result.Output["jti"] != "test-jti-123" {
		t.Errorf("expected jti=test-jti-123, got %v", result.Output["jti"])
	}
	if !bl.IsBlacklisted("test-jti-123") {
		t.Error("expected test-jti-123 to be in blacklist after revoke")
	}
}

func TestTokenRevokeStep_NoBearerPrefix(t *testing.T) {
	bl := newMockBlacklist()
	app := newTokenRevokeApp("bl", bl)

	factory := NewTokenRevokeStepFactory()
	step, err := factory("revoke", map[string]any{
		"blacklist_module": "bl",
		"token_source":     "steps.parse.token",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	// Token without Bearer prefix.
	tokenStr := makeTestJWT(t, "aaaabbbbccccddddeeeeffffgggghhhh", jwt.MapClaims{
		"jti": "bare-jti",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{"token": tokenStr})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["revoked"] != true {
		t.Errorf("expected revoked=true, got %v", result.Output["revoked"])
	}
	if !bl.IsBlacklisted("bare-jti") {
		t.Error("expected bare-jti to be blacklisted")
	}
}

func TestTokenRevokeStep_MissingToken(t *testing.T) {
	factory := NewTokenRevokeStepFactory()
	app := newTokenRevokeApp("bl", newMockBlacklist())
	step, err := factory("revoke", map[string]any{
		"blacklist_module": "bl",
		"token_source":     "steps.parse.token",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["revoked"] != false {
		t.Errorf("expected revoked=false for missing token, got %v", result.Output["revoked"])
	}
}

func TestTokenRevokeStep_NoJTIClaim(t *testing.T) {
	bl := newMockBlacklist()
	app := newTokenRevokeApp("bl", bl)

	factory := NewTokenRevokeStepFactory()
	step, err := factory("revoke", map[string]any{
		"blacklist_module": "bl",
		"token_source":     "steps.parse.token",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	// Token without jti claim.
	tokenStr := makeTestJWT(t, "aaaabbbbccccddddeeeeffffgggghhhh", jwt.MapClaims{
		"sub": "user-1",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{"token": tokenStr})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Output["revoked"] != false {
		t.Errorf("expected revoked=false for token without jti, got %v", result.Output["revoked"])
	}
	if result.Output["error"] != "token has no jti claim" {
		t.Errorf("expected 'token has no jti claim' error, got %v", result.Output["error"])
	}
}

func TestTokenRevokeStep_FactoryMissingBlacklistModule(t *testing.T) {
	factory := NewTokenRevokeStepFactory()
	_, err := factory("revoke", map[string]any{"token_source": "token"}, nil)
	if err == nil {
		t.Fatal("expected error for missing blacklist_module")
	}
}

func TestTokenRevokeStep_FactoryMissingTokenSource(t *testing.T) {
	factory := NewTokenRevokeStepFactory()
	_, err := factory("revoke", map[string]any{"blacklist_module": "bl"}, nil)
	if err == nil {
		t.Fatal("expected error for missing token_source")
	}
}

func TestTokenRevokeStep_BlacklistModuleNotFound(t *testing.T) {
	app := NewMockApplication() // no services registered
	factory := NewTokenRevokeStepFactory()
	step, err := factory("revoke", map[string]any{
		"blacklist_module": "missing-bl",
		"token_source":     "steps.parse.token",
	}, app)
	if err != nil {
		t.Fatalf("factory: %v", err)
	}

	tokenStr := makeTestJWT(t, "aaaabbbbccccddddeeeeffffgggghhhh", jwt.MapClaims{
		"jti": "some-jti",
		"exp": float64(time.Now().Add(time.Hour).Unix()),
	})
	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{"token": tokenStr})

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when blacklist module is not registered")
	}
}

// --- Integration test ---

// TestBlacklistedTokenFailsAuth tests the full flow: issue token, revoke it,
// then verify that JWTAuthModule rejects it.
func TestBlacklistedTokenFailsAuth(t *testing.T) {
	secret := "a-very-long-secret-key-for-testing-purposes-1234"
	jwtMod := NewJWTAuthModule("auth", secret, time.Hour, "test")

	app := NewMockApplication()
	if err := jwtMod.Init(app); err != nil {
		t.Fatalf("JWTAuthModule.Init: %v", err)
	}

	// Issue a token.
	user := &User{ID: "1", Email: "test@example.com", Name: "Test"}
	tokenStr, err := jwtMod.generateToken(user)
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}

	// Token should be valid before revocation.
	valid, claims, err := jwtMod.Authenticate(tokenStr)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !valid {
		t.Fatal("expected token to be valid before revocation")
	}

	jti, ok := claims["jti"].(string)
	if !ok || jti == "" {
		t.Fatal("expected jti claim in token")
	}

	// Wire a blacklist and revoke the token.
	bl := NewTokenBlacklistModule("bl", "memory", "", time.Minute)
	jwtMod.SetTokenBlacklist(bl)
	bl.Add(jti, time.Now().Add(time.Hour))

	// Token should now be rejected.
	valid, _, err = jwtMod.Authenticate(tokenStr)
	if err != nil {
		t.Fatalf("Authenticate after revocation: %v", err)
	}
	if valid {
		t.Fatal("expected token to be rejected after revocation")
	}
}

// TestNonRevokedTokenStillValid ensures that revoking one token does not
// affect other valid tokens.
func TestNonRevokedTokenStillValid(t *testing.T) {
	secret := "a-very-long-secret-key-for-testing-purposes-5678"
	jwtMod := NewJWTAuthModule("auth", secret, time.Hour, "test")
	app := NewMockApplication()
	if err := jwtMod.Init(app); err != nil {
		t.Fatalf("JWTAuthModule.Init: %v", err)
	}

	bl := NewTokenBlacklistModule("bl", "memory", "", time.Minute)
	jwtMod.SetTokenBlacklist(bl)

	user := &User{ID: "2", Email: "other@example.com", Name: "Other"}
	tokenStr, err := jwtMod.generateToken(user)
	if err != nil {
		t.Fatalf("generateToken: %v", err)
	}

	// Revoke a *different* JTI.
	bl.Add("some-other-jti", time.Now().Add(time.Hour))

	valid, _, err := jwtMod.Authenticate(tokenStr)
	if err != nil {
		t.Fatalf("Authenticate: %v", err)
	}
	if !valid {
		t.Fatal("expected un-revoked token to remain valid")
	}
}

// Compile-time check: TokenBlacklistModule satisfies TokenBlacklist.
var _ TokenBlacklist = (*TokenBlacklistModule)(nil)

// Compile-time check: mockBlacklist satisfies TokenBlacklist.
var _ TokenBlacklist = (*mockBlacklist)(nil)

// fakeTokenRevokeBlacklistError implements TokenBlacklist but always errors on GetService.
type alwaysErrorApp struct {
	*MockApplication
	serviceErr error
}

func (a *alwaysErrorApp) GetService(name string, out any) error {
	return fmt.Errorf("forced error: %w", a.serviceErr)
}
