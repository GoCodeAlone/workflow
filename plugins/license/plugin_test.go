package license

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/CrisisTextLine/modular"
	"github.com/GoCodeAlone/workflow/capability"
	"github.com/GoCodeAlone/workflow/licensing"
	"github.com/GoCodeAlone/workflow/pkg/license"
	"github.com/GoCodeAlone/workflow/plugin"
	"github.com/GoCodeAlone/workflow/schema"
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
	if m.Name != "license" {
		t.Errorf("expected name %q, got %q", "license", m.Name)
	}
	found := false
	for _, h := range m.WiringHooks {
		if h == "license-validator-wiring" {
			found = true
		}
	}
	if !found {
		t.Error("manifest missing wiring hook 'license-validator-wiring'")
	}
}

func TestWiringHooks(t *testing.T) {
	p := New()
	hooks := p.WiringHooks()
	if len(hooks) != 1 {
		t.Fatalf("expected 1 wiring hook, got %d", len(hooks))
	}
	if hooks[0].Name != "license-validator-wiring" {
		t.Errorf("unexpected hook name: %q", hooks[0].Name)
	}
	if hooks[0].Hook == nil {
		t.Error("wiring hook function is nil")
	}
}

func TestModuleFactories(t *testing.T) {
	p := New()
	factories := p.ModuleFactories()
	if _, ok := factories["license.validator"]; !ok {
		t.Error("missing factory for license.validator")
	}
}

func TestModuleSchemas(t *testing.T) {
	p := New()
	schemas := p.ModuleSchemas()
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}
	if schemas[0].Type != "license.validator" {
		t.Errorf("unexpected schema type: %q", schemas[0].Type)
	}
}

// stubApp is a minimal modular.Application for wiring hook tests.
type stubApp struct {
	services modular.ServiceRegistry
}

func newStubApp(services map[string]any) *stubApp {
	return &stubApp{services: services}
}

func (a *stubApp) SvcRegistry() modular.ServiceRegistry        { return a.services }
func (a *stubApp) RegisterService(name string, svc any) error  { a.services[name] = svc; return nil }
func (a *stubApp) ConfigProvider() modular.ConfigProvider      { return nil }
func (a *stubApp) RegisterModule(_ modular.Module)             {}
func (a *stubApp) RegisterConfigSection(_ string, _ modular.ConfigProvider) {}
func (a *stubApp) GetConfigSection(_ string) (modular.ConfigProvider, error) {
	return nil, nil
}
func (a *stubApp) Init() error   { return nil }
func (a *stubApp) Run() error    { return nil }
func (a *stubApp) Start() error  { return nil }
func (a *stubApp) Stop() error   { return nil }
func (a *stubApp) Logger() modular.Logger { return nil }
func (a *stubApp) Service(name string) (any, bool) {
	svc, ok := a.services[name]
	return svc, ok
}
func (a *stubApp) Must(name string) any {
	svc, ok := a.services[name]
	if !ok {
		panic("service not found: " + name)
	}
	return svc
}
func (a *stubApp) Inject(name string, svc any) { a.services[name] = svc }
func (a *stubApp) GetService(name string, out any) error { return nil }
func (a *stubApp) ConfigSections() map[string]modular.ConfigProvider { return nil }
func (a *stubApp) OnConfigLoaded(_ func(app modular.Application) error)            {}
func (a *stubApp) SetLogger(_ modular.Logger)                                      {}
func (a *stubApp) SetVerboseConfig(_ bool)                                         {}
func (a *stubApp) IsVerboseConfig() bool                                           { return false }
func (a *stubApp) GetServicesByModule(_ string) []string                           { return nil }
func (a *stubApp) StartTime() time.Time                                            { return time.Time{} }
func (a *stubApp) GetModule(_ string) modular.Module                               { return nil }
func (a *stubApp) GetAllModules() map[string]modular.Module                        { return nil }
func (a *stubApp) GetServiceEntry(_ string) (*modular.ServiceRegistryEntry, bool)  { return nil, false }
func (a *stubApp) GetServicesByInterface(_ reflect.Type) []*modular.ServiceRegistryEntry {
	return nil
}

// stubEngineLoader exposes a PluginLoader for the wiring hook to find via
// the engineWithLoader interface.
type stubEngineLoader struct {
	loader *plugin.PluginLoader
}

func (s *stubEngineLoader) PluginLoader() *plugin.PluginLoader { return s.loader }

func TestWiringHook_NoToken(t *testing.T) {
	t.Setenv("WORKFLOW_LICENSE_TOKEN", "")

	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	eng := &stubEngineLoader{loader: loader}
	app := newStubApp(map[string]any{"workflowEngine": eng})

	hook := New().WiringHooks()[0]
	if err := hook.Hook(app, nil); err != nil {
		t.Fatalf("wiring hook failed: %v", err)
	}
	// No validator should be set when there's no token or HTTP validator
	// (loader.ValidateTier works without a validator in permissive mode)
}

func TestWiringHook_WithValidToken(t *testing.T) {
	// Generate test keypair and override embedded public key for this test
	pub, priv, err := license.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	// Temporarily override the embedded key for the test
	original := embeddedPublicKey
	embeddedPublicKey = license.MarshalPublicKeyPEM(pub)
	defer func() { embeddedPublicKey = original }()

	// Create a signed token
	tok := &license.LicenseToken{
		LicenseID:    "test-lic",
		TenantID:     "test-tenant",
		Organization: "Test Org",
		Tier:         "enterprise",
		Features:     []string{"my-plugin"},
		MaxWorkflows: 10,
		MaxPlugins:   5,
		IssuedAt:     time.Now().Unix(),
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}
	tokenStr, err := tok.Sign(priv)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("WORKFLOW_LICENSE_TOKEN", tokenStr)

	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	eng := &stubEngineLoader{loader: loader}
	app := newStubApp(map[string]any{"workflowEngine": eng})

	hook := New().WiringHooks()[0]
	if err := hook.Hook(app, nil); err != nil {
		t.Fatalf("wiring hook failed: %v", err)
	}
}

func TestWiringHook_InvalidToken(t *testing.T) {
	t.Setenv("WORKFLOW_LICENSE_TOKEN", "wflic.v1.invalid.token")

	loader := plugin.NewPluginLoader(capability.NewRegistry(), schema.NewModuleSchemaRegistry())
	eng := &stubEngineLoader{loader: loader}
	app := newStubApp(map[string]any{"workflowEngine": eng})

	hook := New().WiringHooks()[0]
	if err := hook.Hook(app, nil); err == nil {
		t.Error("expected wiring hook to fail with invalid token")
	}
}

func TestWiringHook_NoEngineService(t *testing.T) {
	t.Setenv("WORKFLOW_LICENSE_TOKEN", "")

	// No engine registered — hook should succeed silently
	app := newStubApp(map[string]any{})
	hook := New().WiringHooks()[0]
	if err := hook.Hook(app, nil); err != nil {
		t.Fatalf("wiring hook should not fail when no engine is registered: %v", err)
	}
}

func TestLicenseValidatorAdapter_WithOffline(t *testing.T) {
	pub, priv, err := license.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	tok := &license.LicenseToken{
		LicenseID:    "lic-1",
		Tier:         "enterprise",
		Features:     []string{"plugin-a"},
		IssuedAt:     time.Now().Unix(),
		ExpiresAt:    time.Now().Add(time.Hour).Unix(),
	}
	tokenStr, err := tok.Sign(priv)
	if err != nil {
		t.Fatal(err)
	}

	offline, err := licensing.NewOfflineValidator(license.MarshalPublicKeyPEM(pub), tokenStr)
	if err != nil {
		t.Fatal(err)
	}

	adapter := &licenseValidatorAdapter{validator: offline, offline: offline}

	if err := adapter.ValidatePlugin("plugin-a"); err != nil {
		t.Errorf("ValidatePlugin(plugin-a) should succeed: %v", err)
	}
	if err := adapter.ValidatePlugin("plugin-b"); err == nil {
		t.Error("ValidatePlugin(plugin-b) should fail")
	}
}

func TestLicenseValidatorAdapter_HTTPFallback(t *testing.T) {
	// HTTP validator with empty server URL returns a starter license
	httpV := licensing.NewHTTPValidator(licensing.ValidatorConfig{}, nil)
	// Force the HTTP validator to have a cached result
	_, _ = httpV.Validate(context.Background(), "test-key")

	adapter := &licenseValidatorAdapter{
		validator: httpV,
		offline:   nil,
	}

	// HTTP starter license has tier "starter" which is not professional/enterprise
	// so premium plugin validation should fail
	if err := adapter.ValidatePlugin("some-plugin"); err == nil {
		t.Error("expected ValidatePlugin to fail for HTTP starter license (no cached info yet)")
	}
}
