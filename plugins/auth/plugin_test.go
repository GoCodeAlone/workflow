package auth

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/module"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/golang-jwt/jwt/v5"
)

func TestPluginImplementsEnginePlugin(t *testing.T) {
	p := New()
	var _ plugin.EnginePlugin = p
}

func TestPluginManifest(t *testing.T) {
	p := New()
	m := p.EngineManifest()

	if err := m.Validate(); err != nil {
		t.Fatalf("manifest validation failed: %v", err)
	}
	if m.Name != "auth" {
		t.Errorf("expected name %q, got %q", "auth", m.Name)
	}
	if len(m.ModuleTypes) != 6 {
		t.Errorf("expected 6 module types, got %d", len(m.ModuleTypes))
	}
	if len(m.WiringHooks) != 4 {
		t.Errorf("expected 4 wiring hooks, got %d", len(m.WiringHooks))
	}
}

func TestPluginCapabilities(t *testing.T) {
	p := New()
	caps := p.Capabilities()
	if len(caps) != 2 {
		t.Fatalf("expected 2 capabilities, got %d", len(caps))
	}
	names := map[string]bool{}
	for _, c := range caps {
		names[c.Name] = true
	}
	for _, expected := range []string{"authentication", "user-management"} {
		if !names[expected] {
			t.Errorf("missing capability %q", expected)
		}
	}
}

func TestModuleFactories(t *testing.T) {
	// field-protection factory requires a master key via env var
	t.Setenv("FIELD_ENCRYPTION_KEY", "test-master-key-32-bytes-long!!")

	p := New()
	factories := p.ModuleFactories()

	expectedTypes := []string{"auth.jwt", "auth.user-store", "auth.oauth2", "auth.m2m", "auth.token-blacklist", "security.field-protection"}
	for _, typ := range expectedTypes {
		factory, ok := factories[typ]
		if !ok {
			t.Errorf("missing factory for %q", typ)
			continue
		}
		mod := factory("test-"+typ, map[string]any{})
		if mod == nil {
			t.Errorf("factory for %q returned nil", typ)
		}
	}
}

func TestModuleFactoryJWTWithConfig(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["auth.jwt"]("jwt-test", map[string]any{
		"secret":         "test-secret",
		"tokenExpiry":    "1h",
		"issuer":         "test-issuer",
		"seedFile":       "data/users.json",
		"responseFormat": "oauth2",
	})
	if mod == nil {
		t.Fatal("auth.jwt factory returned nil with config")
	}
}

func TestWiringHooks(t *testing.T) {
	p := New()
	hooks := p.WiringHooks()
	if len(hooks) != 4 {
		t.Fatalf("expected 4 wiring hooks, got %d", len(hooks))
	}
	hookNames := map[string]bool{}
	for _, h := range hooks {
		hookNames[h.Name] = true
		if h.Hook == nil {
			t.Errorf("wiring hook %q function is nil", h.Name)
		}
	}
	for _, expected := range []string{"auth-provider-wiring", "oauth2-jwt-wiring", "token-blacklist-wiring", "field-protection-wiring"} {
		if !hookNames[expected] {
			t.Errorf("missing wiring hook %q", expected)
		}
	}
}

func TestModuleSchemas(t *testing.T) {
	p := New()
	schemas := p.ModuleSchemas()
	if len(schemas) != 4 {
		t.Fatalf("expected 4 module schemas, got %d", len(schemas))
	}

	types := map[string]bool{}
	for _, s := range schemas {
		types[s.Type] = true
	}
	for _, expected := range []string{"auth.jwt", "auth.user-store", "auth.oauth2", "auth.m2m"} {
		if !types[expected] {
			t.Errorf("missing schema for %q", expected)
		}
	}
}

func TestModuleFactoryM2MWithClaims(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["auth.m2m"]("m2m-test", map[string]any{
		"algorithm": "HS256",
		"secret":    "this-is-a-valid-secret-32-bytes!",
		"clients": []any{
			map[string]any{
				"clientId":     "org-alpha",
				"clientSecret": "secret-alpha",
				"scopes":       []any{"read"},
				"claims": map[string]any{
					"tenant_id": "alpha",
				},
			},
		},
	})
	if mod == nil {
		t.Fatal("auth.m2m factory returned nil")
	}

	m2mMod, ok := mod.(*module.M2MAuthModule)
	if !ok {
		t.Fatal("expected *module.M2MAuthModule")
	}

	// Issue a token via the Handle method.
	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"org-alpha"},
		"client_secret": {"secret-alpha"},
	}
	req := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	m2mMod.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d; body: %s", w.Code, w.Body.String())
	}
}

func TestModuleFactoryM2MWithCustomEndpoints(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["auth.m2m"]("m2m-ep-test", map[string]any{
		"algorithm": "HS256",
		"secret":    "this-is-a-valid-secret-32-bytes!",
		"clients": []any{
			map[string]any{
				"clientId":     "ep-client",
				"clientSecret": "ep-secret",
			},
		},
		"endpoints": map[string]any{
			"token":      "/v2/oauth/token",
			"revoke":     "/oauth/token/revoke",
			"introspect": "/oauth/token/introspect",
			"jwks":       "/v2/oauth/jwks",
		},
	})
	if mod == nil {
		t.Fatal("auth.m2m factory returned nil")
	}

	m2mMod, ok := mod.(*module.M2MAuthModule)
	if !ok {
		t.Fatal("expected *module.M2MAuthModule")
	}

  // Custom token path should work.
	params := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {"ep-client"},
		"client_secret": {"ep-secret"},
	}
	req := httptest.NewRequest(http.MethodPost, "/v2/oauth/token", strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	m2mMod.Handle(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 on custom token path, got %d; body: %s", w.Code, w.Body.String())
	}

	// Default token path should return 404 when overridden.
	req2 := httptest.NewRequest(http.MethodPost, "/oauth/token", strings.NewReader(params.Encode()))
	req2.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w2 := httptest.NewRecorder()
	m2mMod.Handle(w2, req2)
	if w2.Code != http.StatusNotFound {
		t.Errorf("expected 404 on default path after override, got %d", w2.Code)
	}
}

func TestModuleFactoryM2MWithTrustedKeys(t *testing.T) {
	// Generate a key pair to represent an external trusted issuer.
	clientKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	pkixBytes, err := x509.MarshalPKIXPublicKey(&clientKey.PublicKey)
	if err != nil {
		t.Fatalf("MarshalPKIXPublicKey: %v", err)
	}
	pubKeyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pkixBytes}))

	p := New()
	factories := p.ModuleFactories()

	mod := factories["auth.m2m"]("m2m-test", map[string]any{
		"algorithm": "ES256",
		"trustedKeys": []any{
			map[string]any{
				"issuer":       "https://external-issuer.example.com",
				"publicKeyPEM": pubKeyPEM,
				"audiences":    []any{"test-audience"},
				"claimMapping": map[string]any{
					"user_id": "ext_user",
				},
			},
		},
  })
	if mod == nil {
		t.Fatal("auth.m2m factory returned nil")
	}

	m2mMod, ok := mod.(*module.M2MAuthModule)
	if !ok {
		t.Fatal("expected *module.M2MAuthModule")
	}

	// Issue a JWT assertion signed by the external issuer's key.
	claims := jwt.MapClaims{
		"iss": "https://external-issuer.example.com",
		"sub": "external-service",
		"aud": "test-audience",
		"iat": time.Now().Unix(),
		"exp": time.Now().Add(5 * time.Minute).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	assertion, err := tok.SignedString(clientKey)
	if err != nil {
		t.Fatalf("sign assertion: %v", err)
	}

	params := url.Values{
		"grant_type": {module.GrantTypeJWTBearer},
		"assertion":  {assertion},
	}
	req := httptest.NewRequest("POST", "/oauth/token", strings.NewReader(params.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	m2mMod.Handle(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 for JWT-bearer with trusted key, got %d: %s", w.Code, w.Body.String())
	}
}

func TestModuleFactoryM2MWithTrustedKeys_MissingIssuer(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["auth.m2m"]("m2m-test", map[string]any{
		"algorithm": "ES256",
		"trustedKeys": []any{
			map[string]any{
				// issuer is missing
				"publicKeyPEM": "-----BEGIN PUBLIC KEY-----\nMFkwEwYHKoZIzj0CAQYIKoZIzj0DAQcDQgAEtest==\n-----END PUBLIC KEY-----",
			},
		},
	})
	if mod == nil {
		t.Fatal("auth.m2m factory returned nil")
	}
	m2mMod, ok := mod.(*module.M2MAuthModule)
	if !ok {
		t.Fatal("expected *module.M2MAuthModule")
	}

	// Init should fail because trustedKeys[0] is missing issuer.
	if err := m2mMod.Init(nil); err == nil {
		t.Error("expected Init to return error for trustedKeys entry missing issuer")
	}
}

func TestModuleFactoryM2MWithTrustedKeys_MissingPEM(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()

	mod := factories["auth.m2m"]("m2m-test", map[string]any{
		"algorithm": "ES256",
		"trustedKeys": []any{
			map[string]any{
				"issuer": "https://external.example.com",
				// publicKeyPEM is missing
			},
		},
	})
	if mod == nil {
		t.Fatal("auth.m2m factory returned nil")
	}
	m2mMod, ok := mod.(*module.M2MAuthModule)
	if !ok {
		t.Fatal("expected *module.M2MAuthModule")
	}

	// Init should fail because trustedKeys[0] is missing publicKeyPEM.
	if err := m2mMod.Init(nil); err == nil {
		t.Error("expected Init to return error for trustedKeys entry missing publicKeyPEM")
	}
}
