package module

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
	"google.golang.org/protobuf/encoding/protojson"

	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
)

// recordingEngine captures TriggerWorkflow calls so T15 module
// unit tests can assert the 3 contribution registrations fire in
// Start. Implements WorkflowEngine (declared in http_trigger.go).
type recordingEngine struct {
	mu       sync.Mutex
	triggers []recordedTrigger
	err      error
}

type recordedTrigger struct {
	WorkflowType string
	Action       string
	Data         map[string]any
}

func (r *recordingEngine) TriggerWorkflow(_ context.Context, workflowType, action string, data map[string]any) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.err != nil {
		return r.err
	}
	r.triggers = append(r.triggers, recordedTrigger{
		WorkflowType: workflowType, Action: action, Data: data,
	})
	return nil
}

// secHdrsStub is a no-op HTTPMiddleware used by T15 tests so we
// can assert the route-mount wrapper is exercised without standing
// up a real security-headers module.
type secHdrsStub struct{ name string }

func (s *secHdrsStub) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Test-Sec-Hdrs", s.name)
		next.ServeHTTP(w, r)
	})
}

// stateStoreStub is a minimal interfaces.IaCStateStore for the T15
// unit tests. ListResources returns a fixed slice; other methods
// behave as no-ops so the handler library can exercise its full
// dispatch surface without panicking.
type stateStoreStub struct {
	resources []interfaces.ResourceState
}

func (s *stateStoreStub) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	out := make([]interfaces.ResourceState, len(s.resources))
	copy(out, s.resources)
	return out, nil
}
func (s *stateStoreStub) GetResource(_ context.Context, name string) (*interfaces.ResourceState, error) {
	for i := range s.resources {
		if s.resources[i].Name == name {
			r := s.resources[i]
			return &r, nil
		}
	}
	return nil, nil
}
func (s *stateStoreStub) SaveResource(_ context.Context, _ interfaces.ResourceState) error {
	return nil
}
func (s *stateStoreStub) DeleteResource(_ context.Context, _ string) error       { return nil }
func (s *stateStoreStub) SavePlan(_ context.Context, _ interfaces.IaCPlan) error { return nil }
func (s *stateStoreStub) GetPlan(_ context.Context, _ string) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (s *stateStoreStub) Lock(_ context.Context, _ string, _ time.Duration) (interfaces.IaCLockHandle, error) {
	return nil, nil
}
func (s *stateStoreStub) Close() error { return nil }

// wfConfigSection wraps a *config.WorkflowConfig as a
// modular.ConfigProvider so the providerTypeByModule walk in
// populateProviderTypes can find it via app.GetConfigSection.
type wfConfigSection struct{ cfg *config.WorkflowConfig }

func (s wfConfigSection) GetConfig() any { return s.cfg }

// withWorkflowConfig wires a workflow config section into the mock
// app. Returns a fake-app pre-populated with the standard T15
// dependency surface (state store, router, security-headers,
// provider, workflow config).
func newInfraAdminTestApp(t *testing.T, providerType string) (*infraMockApp, *recordingEngine, *stateStoreStub) {
	t.Helper()
	app := newInfraMockApp()
	store := &stateStoreStub{}
	router := NewStandardHTTPRouter("http-router")
	secMw := &secHdrsStub{name: "test-sec"}
	provider := &infraMockProvider{name: providerType}
	engine := &recordingEngine{}

	// Register all dependency services the InfraAdmin module's Init
	// + Start will look up.
	_ = app.RegisterService("iac-state", store)
	_ = app.RegisterService("http-router", router)
	_ = app.RegisterService("security-headers", secMw)
	_ = app.RegisterService("do-provider", provider)
	_ = app.RegisterService("workflowEngine", engine)

	// Wire the workflow config so populateProviderTypes can find
	// "do-provider" → providerType. The fake's GetConfigSection
	// returns nil by default; we install the workflow section
	// in-test via a wrapper.
	app.services["__workflow_section__"] = &wfConfigSection{cfg: &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: "do-provider", Type: "iac.provider", Config: map[string]any{"provider": providerType}},
		},
	}}
	return app, engine, store
}

// withConfigSectionApp is a wrapper around infraMockApp that
// returns a workflow-config section in GetConfigSection. The base
// infraMockApp's GetConfigSection always returns (nil, nil); we
// override here so populateProviderTypes finds the workflow cfg.
type withConfigSectionApp struct {
	*infraMockApp
	section modular.ConfigProvider
}

func (a *withConfigSectionApp) GetConfigSection(name string) (modular.ConfigProvider, error) {
	if name == "workflow" {
		return a.section, nil
	}
	return nil, errors.New("config section not found: " + name)
}

func newAppWithWorkflowSection(t *testing.T, providerType string) (*withConfigSectionApp, *recordingEngine, *stateStoreStub) {
	t.Helper()
	base, engine, store := newInfraAdminTestApp(t, providerType)
	sec, _ := base.services["__workflow_section__"].(*wfConfigSection)
	return &withConfigSectionApp{infraMockApp: base, section: sec}, engine, store
}

// helper config matching the design's reference app YAML.
func standardCfg() InfraAdminConfig {
	return InfraAdminConfig{
		RoutePrefix:           "/api/infra-admin",
		AssetPrefix:           "/admin/infra-admin",
		StateModule:           "iac-state",
		HTTPModule:            "http-router",
		SecurityHeadersModule: "security-headers",
		ProviderModules:       []string{"do-provider"},
	}
}

// TestInfraAdmin_Init_ResolvesAllServices pins the design's Init
// contract: state + router + security-headers + every declared
// provider must resolve cleanly, the workflowEngine MUST NOT be
// resolved at Init (it's registered later by configureTriggers),
// and providerTypeByModule must be populated from the WorkflowConfig
// (the F1 fix per spec-reviewer T6).
func TestInfraAdmin_Init_ResolvesAllServices(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	m := NewInfraAdmin("infra-admin", configToMap(t, standardCfg())).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.state == nil {
		t.Error("state unresolved after Init")
	}
	if m.router == nil {
		t.Error("router unresolved after Init")
	}
	if m.secHdrs == nil {
		t.Error("security-headers middleware unresolved after Init")
	}
	if len(m.providers) != 1 || m.providers["do-provider"] == nil {
		t.Errorf("providers = %v, want one do-provider entry", m.providers)
	}
	if m.providerTypeByModule["do-provider"] != "digitalocean" {
		t.Errorf("providerTypeByModule[do-provider] = %q, want digitalocean (F1 contract)", m.providerTypeByModule["do-provider"])
	}
	// workflowEngine must NOT be resolved at Init.
	if m.engine != nil {
		t.Error("engine should be nil after Init — it's resolved at Start")
	}
}

func TestInfraAdmin_Init_FailsWhenStateModuleMissing(t *testing.T) {
	app := newInfraMockApp()
	// no iac-state registered
	m := NewInfraAdmin("infra-admin", configToMap(t, standardCfg())).(*InfraAdmin)
	if err := m.Init(app); err == nil {
		t.Fatal("expected Init to fail when state module missing, got nil")
	}
}

func TestInfraAdmin_Init_FailsWhenRouterIsWrongType(t *testing.T) {
	app := newInfraMockApp()
	_ = app.RegisterService("iac-state", &stateStoreStub{})
	// http-router is NOT a *StandardHTTPRouter
	_ = app.RegisterService("http-router", "not-a-router")
	_ = app.RegisterService("security-headers", &secHdrsStub{})
	_ = app.RegisterService("do-provider", &infraMockProvider{name: "digitalocean"})
	m := NewInfraAdmin("infra-admin", configToMap(t, standardCfg())).(*InfraAdmin)
	err := m.Init(app)
	if err == nil {
		t.Fatal("expected Init to fail when http_module is wrong type, got nil")
	}
}

func TestInfraAdmin_Init_OpensAuditWriter(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	dir := t.TempDir()
	cfg := standardCfg()
	cfg.AccessLogPath = filepath.Join(dir, "audit.jsonl")
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if m.audit == nil {
		t.Error("audit writer not opened when access_log_path set")
	}
	if _, err := os.Stat(cfg.AccessLogPath); err != nil {
		t.Errorf("audit file not created: %v", err)
	}
}

func TestInfraAdmin_Init_AuditFailureIsFatal(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	cfg := standardCfg()
	cfg.AccessLogPath = t.TempDir() // path is an existing directory; Open errors
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err == nil {
		t.Fatal("Init should fail FATAL when audit Open fails, got nil")
	}
}

// TestInfraAdmin_Start_Fires3ContributionPipelines pins the
// design's Start contract: exactly 3 engine.TriggerWorkflow calls
// fire, with the expected pipeline names + contribution payloads.
func TestInfraAdmin_Start_Fires3ContributionPipelines(t *testing.T) {
	app, engine, _ := newAppWithWorkflowSection(t, "digitalocean")
	m := NewInfraAdmin("infra-admin", configToMap(t, standardCfg())).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if len(engine.triggers) != 3 {
		t.Fatalf("TriggerWorkflow calls = %d, want 3", len(engine.triggers))
	}
	wantNames := map[string]bool{
		"pipeline:register-infra-admin-resources":       false,
		"pipeline:register-infra-admin-resource-detail": false,
		"pipeline:register-infra-admin-new-resource":    false,
	}
	for _, tr := range engine.triggers {
		if _, ok := wantNames[tr.WorkflowType]; ok {
			wantNames[tr.WorkflowType] = true
		} else {
			t.Errorf("unexpected pipeline trigger: %s", tr.WorkflowType)
		}
	}
	for name, fired := range wantNames {
		if !fired {
			t.Errorf("expected trigger for %s, not fired", name)
		}
	}
}

func TestInfraAdmin_Start_PropagatesEngineFailure(t *testing.T) {
	app, engine, _ := newAppWithWorkflowSection(t, "digitalocean")
	engine.err = errors.New("simulated pipeline failure")
	m := NewInfraAdmin("infra-admin", configToMap(t, standardCfg())).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err == nil {
		t.Fatal("expected Start to propagate engine.TriggerWorkflow failure, got nil")
	}
}

// TestInfraAdmin_Start_MountsRoutesWithMiddleware verifies the API
// routes are registered on the router AND the security-headers
// middleware wraps each. Hits the route via the router's
// ServeHTTP + checks the test middleware's X-Test-Sec-Hdrs header
// appears on the response — proves the middleware was actually
// attached to the route adapter.
func TestInfraAdmin_Start_MountsRoutesWithMiddleware(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	m := NewInfraAdmin("infra-admin", configToMap(t, standardCfg())).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Modular framework calls Start on all modules; routes don't
	// land in the live mux until the router's own Start builds it.
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Drive the router with a real HTTP request for the list-resources route.
	req := httptest.NewRequest(http.MethodPost, "/api/infra-admin/resources", bytes.NewReader([]byte(`{"evidence":{"authz_checked":true,"authz_allowed":true}}`)))
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Test-Sec-Hdrs") == "" {
		t.Error("security-headers middleware not attached to API route")
	}
	// Response must be valid protojson decoding into the typed proto.
	var out adminpb.AdminListResourcesOutput
	if err := protojson.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Errorf("response is not valid AdminListResourcesOutput protojson: %v\n%s", err, rec.Body.String())
	}
}

// TestInfraAdmin_Start_MountsAssetRoute exercises the asset
// FileServer mount — GET /admin/infra-admin/resources.html must
// return the embedded HTML content.
func TestInfraAdmin_Start_MountsAssetRoute(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	m := NewInfraAdmin("infra-admin", configToMap(t, standardCfg())).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodGet, "/admin/infra-admin/resources.html", nil)
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("<!doctype html")) && !bytes.Contains(rec.Body.Bytes(), []byte("<!DOCTYPE html")) {
		t.Errorf("expected embedded resources.html content, got: %s", rec.Body.String())
	}
}

// TestInfraAdmin_HandleListResources_ReturnsProtojson hits the
// list-resources route directly and asserts the response wire
// shape — protojson with snake_case keys per the cross-task
// contract.
func TestInfraAdmin_HandleListResources_ReturnsProtojson(t *testing.T) {
	app, _, store := newAppWithWorkflowSection(t, "digitalocean")
	store.resources = []interfaces.ResourceState{{
		Name: "vpc-1", Type: "infra.vpc", Provider: "digitalocean", ProviderRef: "do-provider",
		UpdatedAt: time.Unix(1716800000, 0).UTC(),
	}}
	m := NewInfraAdmin("infra-admin", configToMap(t, standardCfg())).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"evidence":{"authz_checked":true,"authz_allowed":true}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/infra-admin/resources", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	// Wire-shape: response uses snake_case `provider_module` per
	// protojson UseProtoNames=true. If a future regression flips to
	// camelCase, the JS pages break + this test catches it.
	bodyStr := rec.Body.String()
	if !bytes.Contains(rec.Body.Bytes(), []byte(`"provider_module"`)) {
		t.Errorf("response missing snake_case provider_module key: %s", bodyStr)
	}
}

// TestInfraAdmin_Stop_ClosesAudit verifies Stop closes the audit
// writer cleanly + is idempotent.
func TestInfraAdmin_Stop_ClosesAudit(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	dir := t.TempDir()
	cfg := standardCfg()
	cfg.AccessLogPath = filepath.Join(dir, "audit.jsonl")
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(context.Background()); err != nil {
		t.Errorf("Stop: %v", err)
	}
	// Idempotent double-Stop (audit.Writer.Close is also idempotent).
	if err := m.Stop(context.Background()); err != nil {
		t.Errorf("second Stop: %v", err)
	}
}

// TestInfraAdmin_NewInfraAdmin_AppliesDefaults pins the factory's
// default-fill behavior — callers omitting route_prefix /
// asset_prefix get the design's reference values.
func TestInfraAdmin_NewInfraAdmin_AppliesDefaults(t *testing.T) {
	m := NewInfraAdmin("infra-admin", map[string]any{}).(*InfraAdmin)
	if m.config.RoutePrefix != "/api/infra-admin" {
		t.Errorf("default RoutePrefix = %q, want /api/infra-admin", m.config.RoutePrefix)
	}
	if m.config.AssetPrefix != "/admin/infra-admin" {
		t.Errorf("default AssetPrefix = %q, want /admin/infra-admin", m.config.AssetPrefix)
	}
}

func TestInfraAdmin_RequiresServices_ListsAllDependencies(t *testing.T) {
	m := NewInfraAdmin("infra-admin", configToMap(t, standardCfg())).(*InfraAdmin)
	deps := m.RequiresServices()
	wantNames := map[string]bool{
		"iac-state":        false,
		"http-router":      false,
		"security-headers": false,
		"do-provider":      false,
	}
	for _, d := range deps {
		if _, ok := wantNames[d.Name]; ok {
			wantNames[d.Name] = true
		}
	}
	for name, listed := range wantNames {
		if !listed {
			t.Errorf("missing service dependency: %s", name)
		}
	}
	// workflowEngine must NOT be listed — it's resolved at Start.
	for _, d := range deps {
		if d.Name == "workflowEngine" {
			t.Error("RequiresServices must NOT list workflowEngine — it's registered after Init")
		}
	}
}

// configToMap round-trips a typed InfraAdminConfig through JSON
// into the map shape the factory expects (matching how the engine
// passes config maps to module factories at BuildFromConfig time).
func configToMap(t *testing.T, cfg InfraAdminConfig) map[string]any {
	t.Helper()
	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("marshal cfg: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal cfg map: %v", err)
	}
	return m
}
