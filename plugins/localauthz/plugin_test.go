package localauthz_test

import (
	"testing"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/plugins/localauthz"
)

// nopLogger satisfies modular.Logger for tests.
type nopLogger struct{}

func (nopLogger) Debug(string, ...any) {}
func (nopLogger) Info(string, ...any)  {}
func (nopLogger) Warn(string, ...any)  {}
func (nopLogger) Error(string, ...any) {}

// TestPlugin_ModuleFactories asserts the plugin registers "authz.local".
func TestPlugin_ModuleFactories(t *testing.T) {
	p := localauthz.New()
	factories := p.ModuleFactories()
	if factories == nil {
		t.Fatal("ModuleFactories returned nil")
	}
	if _, ok := factories["authz.local"]; !ok {
		t.Fatalf("expected 'authz.local' in ModuleFactories, got %v", keys(factories))
	}
}

// TestEnforce_Table covers the exact-match, allow-effect, default-deny contract.
func TestEnforce_Table(t *testing.T) {
	p := localauthz.New()
	factory := p.ModuleFactories()["authz.local"]

	policies := []any{
		[]any{"operator", "infra:read", "allow"},
		[]any{"operator", "infra:apply", "allow"},
		[]any{"operator", "infra:destroy", "allow"},
		[]any{"viewer", "infra:read", "allow"},
	}
	mod := factory("my-authz", map[string]any{"policies": policies})

	app := modular.NewStdApplication(nil, nopLogger{})
	if err := mod.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Resolve the Enforcer from the service it registered.
	type enforcer interface {
		Enforce(sub, obj, act string, extra ...string) (bool, error)
	}
	sa, ok := mod.(modular.ServiceAware)
	if !ok {
		t.Fatalf("module does not implement modular.ServiceAware; got %T", mod)
	}
	var enf enforcer
	for _, svc := range sa.ProvidesServices() {
		if e, ok := svc.Instance.(enforcer); ok {
			enf = e
			break
		}
	}
	if enf == nil {
		t.Fatal("no enforcer service found after Init")
	}

	cases := []struct {
		sub, obj, act string
		want          bool
	}{
		{"operator", "infra:read", "allow", true},
		{"operator", "infra:apply", "allow", true},
		{"operator", "infra:destroy", "allow", true},
		{"viewer", "infra:read", "allow", true},
		{"viewer", "infra:apply", "allow", false},   // not in policies → deny
		{"viewer", "infra:destroy", "allow", false}, // not in policies → deny
		{"unknown", "infra:read", "allow", false},   // unknown subject → deny
		{"operator", "infra:apply", "deny", false},  // wrong act → deny
		{"operator", "infra:noop", "allow", false},  // unknown obj → deny
	}
	for _, tc := range cases {
		got, err := enf.Enforce(tc.sub, tc.obj, tc.act)
		if err != nil {
			t.Errorf("Enforce(%q,%q,%q): unexpected error: %v", tc.sub, tc.obj, tc.act, err)
			continue
		}
		if got != tc.want {
			t.Errorf("Enforce(%q,%q,%q) = %v, want %v", tc.sub, tc.obj, tc.act, got, tc.want)
		}
	}
}

// TestEnforce_VariadicCompatible asserts extra args are silently accepted
// (matches the concrete Casbin wrapper's variadic signature).
func TestEnforce_VariadicCompatible(t *testing.T) {
	p := localauthz.New()
	factory := p.ModuleFactories()["authz.local"]
	mod := factory("authz", map[string]any{
		"policies": []any{[]any{"u", "o", "a"}},
	})
	app := modular.NewStdApplication(nil, nopLogger{})
	if err := mod.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sa := mod.(modular.ServiceAware)
	type enforcer interface {
		Enforce(sub, obj, act string, extra ...string) (bool, error)
	}
	var enf enforcer
	for _, svc := range sa.ProvidesServices() {
		if e, ok := svc.Instance.(enforcer); ok {
			enf = e
		}
	}
	// Variadic: extra args must not panic or cause error.
	got, err := enf.Enforce("u", "o", "a", "extra1", "extra2")
	if err != nil {
		t.Fatalf("variadic Enforce: %v", err)
	}
	if !got {
		t.Error("variadic Enforce should return true for matching policy")
	}
}

// TestEnforce_EmptyPolicies asserts default-deny with no policies configured.
func TestEnforce_EmptyPolicies(t *testing.T) {
	p := localauthz.New()
	factory := p.ModuleFactories()["authz.local"]
	mod := factory("authz", map[string]any{})

	app := modular.NewStdApplication(nil, nopLogger{})
	if err := mod.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	sa := mod.(modular.ServiceAware)
	type enforcer interface {
		Enforce(sub, obj, act string, extra ...string) (bool, error)
	}
	var enf enforcer
	for _, svc := range sa.ProvidesServices() {
		if e, ok := svc.Instance.(enforcer); ok {
			enf = e
		}
	}
	got, err := enf.Enforce("anyone", "infra:apply", "allow")
	if err != nil {
		t.Fatalf("Enforce: %v", err)
	}
	if got {
		t.Error("empty policies: all requests should be denied")
	}
}

func keys[V any](m map[string]V) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}
