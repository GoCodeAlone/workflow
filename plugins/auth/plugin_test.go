package auth

import (
	"testing"

	"github.com/GoCodeAlone/workflow/plugin"
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
