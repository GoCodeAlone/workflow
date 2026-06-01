package module

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/modular"
	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/admin/handler"
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
		// T4: test-only insecure mode; auth is exercised separately
		// via standardAuthCfg() + newAuthEnabledApp().
		AllowUnauthenticated: true,
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
// design's Start contract: the expected engine.TriggerWorkflow calls
// fire for all registered admin-plugin contribution pipelines.
// Updated to 4 with the T12 audit-viewer (register-infra-admin-actions).
func TestInfraAdmin_Start_Fires3ContributionPipelines(t *testing.T) {
	app, engine, _ := newAppWithWorkflowSection(t, "digitalocean")
	m := NewInfraAdmin("infra-admin", configToMap(t, standardCfg())).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	wantNames := map[string]bool{
		"pipeline:register-infra-admin-resources":       false,
		"pipeline:register-infra-admin-resource-detail": false,
		"pipeline:register-infra-admin-new-resource":    false,
		"pipeline:register-infra-admin-actions":         false, // T12 audit-viewer
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
	if len(engine.triggers) != len(wantNames) {
		t.Errorf("TriggerWorkflow calls = %d, want %d", len(engine.triggers), len(wantNames))
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

// TestInfraAdmin_AuditTail_FiltersBySince pins the design's
// `?since=<unix>` query-param contract — the CLI's
// `wfctl infra admin audit-tail --since 1h` depends on the host
// filtering by timestamp; without this, --since is silently a
// no-op. Per spec-reviewer T15 F1 (commit 60971783d).
//
// Writes 3 audit entries with staggered timestamps, then queries
// the audit endpoint with since=<middle timestamp> and asserts
// only the 2 newest entries are returned.
func TestInfraAdmin_AuditTail_FiltersBySince(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	dir := t.TempDir()
	cfg := standardCfg()
	cfg.AccessLogPath = filepath.Join(dir, "audit.jsonl")
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Stamp three entries with explicit ts_unix values so we can
	// assert filtering deterministically. Use module's audit
	// writer directly so the file is in the exact protojson
	// shape the handler reads.
	t0 := int64(1716800000)
	t1 := int64(1716800100)
	t2 := int64(1716800200)
	for _, ts := range []int64{t0, t1, t2} {
		entry := &adminpb.AdminAuditEntry{
			TsUnix:  ts,
			Subject: fmt.Sprintf("user:t-%d", ts),
			Action:  "list_resources",
			Result:  "ok",
		}
		if err := m.audit.Write(entry); err != nil {
			t.Fatal(err)
		}
	}

	// Filter with since=t1 → expect t1 + t2 (entries with
	// ts_unix < t1 dropped). The handler uses < not <= on the
	// since threshold per design ("entries newer than this
	// duration" — strict).
	req := httptest.NewRequest(http.MethodGet,
		fmt.Sprintf("/api/infra-admin/audit?since=%d", t1), nil)
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Content-Type") != "application/x-ndjson" {
		t.Errorf("Content-Type = %q, want application/x-ndjson", rec.Header().Get("Content-Type"))
	}
	lines := strings.Split(strings.TrimRight(rec.Body.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("filtered ndjson has %d lines, want 2 (entries at t1 + t2)\n%s", len(lines), rec.Body.String())
	}
	// Decode each and verify the timestamps match what we expect.
	var got []int64
	for _, line := range lines {
		var entry adminpb.AdminAuditEntry
		if err := protojson.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("decode line: %v\n%s", err, line)
		}
		got = append(got, entry.GetTsUnix())
	}
	if got[0] != t1 || got[1] != t2 {
		t.Errorf("got ts_unix sequence %v, want [%d, %d]", got, t1, t2)
	}
}

// TestInfraAdmin_AuditTail_FiltersByLimit pins the `?limit=N`
// query-param contract — caller can cap the number of returned
// entries to avoid response-size explosions on long logs.
func TestInfraAdmin_AuditTail_FiltersByLimit(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	dir := t.TempDir()
	cfg := standardCfg()
	cfg.AccessLogPath = filepath.Join(dir, "audit.jsonl")
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	for i := range 5 {
		entry := &adminpb.AdminAuditEntry{
			TsUnix:  int64(1716800000 + i*10),
			Subject: fmt.Sprintf("user:%d", i),
			Action:  "list_resources",
			Result:  "ok",
		}
		if err := m.audit.Write(entry); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/infra-admin/audit?limit=2", nil)
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	lines := strings.Split(strings.TrimRight(rec.Body.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Errorf("got %d lines with limit=2, want 2\n%s", len(lines), rec.Body.String())
	}
}

// TestInfraAdmin_AuditTail_NoFilterReturnsAll verifies the
// no-query-param case (CLI invoked without --since / --limit)
// returns every line.
func TestInfraAdmin_AuditTail_NoFilterReturnsAll(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	dir := t.TempDir()
	cfg := standardCfg()
	cfg.AccessLogPath = filepath.Join(dir, "audit.jsonl")
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	for i := range 3 {
		if err := m.audit.Write(&adminpb.AdminAuditEntry{TsUnix: int64(1716800000 + i*10), Action: "list_resources", Result: "ok"}); err != nil {
			t.Fatal(err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/infra-admin/audit", nil)
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	lines := strings.Split(strings.TrimRight(rec.Body.String(), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d lines without filters, want 3", len(lines))
	}
}

// TestInfraAdmin_AuditTail_FileMissingReturns404 pins the F3
// status-code fix — file open failure surfaces as a clean 404
// rather than a 200-with-error-body. The earlier draft pre-set
// the 200 status before http.ServeFile, which then surfaced
// "404 page not found" as body content with status 200.
func TestInfraAdmin_AuditTail_FileMissingReturns404(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	dir := t.TempDir()
	cfg := standardCfg()
	cfg.AccessLogPath = filepath.Join(dir, "audit.jsonl")
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	// Delete the audit file AFTER Init creates it so the open
	// fails at handler-time.
	if err := os.Remove(cfg.AccessLogPath); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/infra-admin/audit", nil)
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (file missing); body=%s", rec.Code, rec.Body.String())
	}
}

// TestInfraAdmin_AuditTail_NotConfiguredReturns404 pins the
// "audit log not configured" 404 path — distinct from the
// file-missing 404 (different operator diagnosis).
func TestInfraAdmin_AuditTail_NotConfiguredReturns404(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	cfg := standardCfg()
	// cfg.AccessLogPath left empty — audit writer not opened.
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/infra-admin/audit", nil)
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 (not configured)", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "not configured") {
		t.Errorf("expected 'not configured' in body, got %q", rec.Body.String())
	}
}

// TestInfraAdmin_AuditAccess_RecordsDeniedResult pins F2:
// authz-denied requests MUST log result="denied", not "ok".
// Otherwise security-event review hides actual denial attempts.
func TestInfraAdmin_AuditAccess_RecordsDeniedResult(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	dir := t.TempDir()
	cfg := standardCfg()
	cfg.AccessLogPath = filepath.Join(dir, "audit.jsonl")
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// Send a request WITHOUT evidence — handler library rejects
	// with default-deny → out.Error non-empty → audit should
	// record result="denied".
	body := []byte(`{}`)
	req := httptest.NewRequest(http.MethodPost, "/api/infra-admin/resources", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (authz refusal surfaces via Output.error per tag-100)", rec.Code)
	}

	// Read the audit log + assert the recorded result is "denied".
	data, err := os.ReadFile(cfg.AccessLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"result":"denied"`) {
		t.Errorf("audit log missing denied result for refused request:\n%s", string(data))
	}
	if strings.Contains(string(data), `"result":"ok"`) {
		t.Errorf("audit log records 'ok' for a denied request — F2 regression:\n%s", string(data))
	}
}

// TestInfraAdmin_AuditAccess_RecordsOkResult is the positive
// counterpart — happy-path requests log result="ok".
func TestInfraAdmin_AuditAccess_RecordsOkResult(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	dir := t.TempDir()
	cfg := standardCfg()
	cfg.AccessLogPath = filepath.Join(dir, "audit.jsonl")
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	body := []byte(`{"evidence":{"authz_checked":true,"authz_allowed":true,"subject":"user:alice"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/infra-admin/resources", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	data, err := os.ReadFile(cfg.AccessLogPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"result":"ok"`) {
		t.Errorf("audit log missing 'ok' result for happy-path request:\n%s", string(data))
	}
}

// authMwStub is a Bearer-token HTTPMiddleware used by the T15
// auth-route-filter regression tests. It rejects every request
// missing an `Authorization: Bearer …` header with 401 BEFORE the
// handler runs — mirrors module.AuthMiddleware's production
// behaviour for the route-filter contract that closes the
// AdminAuthzEvidence-spoofing gap (design §Security Review).
type authMwStub struct {
	name string
	// validToken, when non-empty, is the only Bearer token accepted.
	// Empty string means "any non-empty Bearer token passes" (matches
	// AuthMiddleware default-provider behaviour when no provider
	// matches the token).
	validToken string
}

func (s *authMwStub) Process(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		if !strings.HasPrefix(authHeader, "Bearer ") {
			http.Error(w, "Authorization header required", http.StatusUnauthorized)
			return
		}
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if s.validToken != "" && token != s.validToken {
			http.Error(w, "Invalid credentials", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// newAuthEnabledApp builds a test app with the standard T15
// dependency surface plus an auth.jwt-shaped HTTPMiddleware
// registered under "auth". Pairs with standardAuthCfg() below.
func newAuthEnabledApp(t *testing.T, providerType string) (*withConfigSectionApp, *recordingEngine, *stateStoreStub, *authMwStub) {
	t.Helper()
	app, engine, store := newAppWithWorkflowSection(t, providerType)
	auth := &authMwStub{name: "auth"}
	_ = app.RegisterService("auth", auth)
	return app, engine, store, auth
}

// standardAuthCfg returns the design's reference config WITH the
// auth_module field set — the production shape per §Security
// Review. Mirrors the YAML the workflow-scenarios/92 config will
// declare in PR-2.
func standardAuthCfg() InfraAdminConfig {
	c := standardCfg()
	c.AuthModule = "auth"
	return c
}

// TestInfraAdmin_UnauthenticatedRequest_Returns401 — design §Security
// Review regression: a request with NO Bearer token MUST be rejected
// with 401 BEFORE the handler runs. Without the auth middleware in
// front of the route, the handler-side AdminAuthzEvidence
// default-deny gives 403 with a body — which is still a leak compared
// to the explicit auth-first 401 contract.
func TestInfraAdmin_UnauthenticatedRequest_Returns401(t *testing.T) {
	app, _, _, _ := newAuthEnabledApp(t, "digitalocean")
	m := NewInfraAdmin("infra-admin", configToMap(t, standardAuthCfg())).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	// No Authorization header at all.
	req := httptest.NewRequest(http.MethodPost, "/api/infra-admin/resources",
		bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated request: status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// TestInfraAdmin_ClientCannotSpoofAuthzEvidence — design §Security
// Review hard contract: a client supplying
// {evidence:{authz_checked:true,authz_allowed:true}} in the request
// body with NO Authorization header MUST still get 401. Without the
// auth route filter, this spoofing trivially bypasses the
// handler-side default-deny. With the auth filter, the request
// never reaches the handler.
//
// This test is the explicit regression gate for the security gap
// team-lead identified during PR-1 review: AdminAuthzEvidence is
// client-supplied data; the host MUST authenticate before
// believing it.
func TestInfraAdmin_ClientCannotSpoofAuthzEvidence(t *testing.T) {
	app, _, _, _ := newAuthEnabledApp(t, "digitalocean")
	m := NewInfraAdmin("infra-admin", configToMap(t, standardAuthCfg())).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	spoof := []byte(`{"evidence":{"authz_checked":true,"authz_allowed":true,"subject":"attacker"}}`)
	req := httptest.NewRequest(http.MethodPost, "/api/infra-admin/resources",
		bytes.NewReader(spoof))
	// Deliberately NO Authorization header — attacker sets the
	// evidence flags in the body but has no credential.
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("evidence-spoof attempt: status = %d, want 401 (security gap)\n"+
			"client supplied authz_checked+authz_allowed in body without Bearer token; auth filter should reject before handler\n"+
			"body=%s", rec.Code, rec.Body.String())
	}
	// Asset routes are equally protected.
	assetReq := httptest.NewRequest(http.MethodGet, "/admin/infra-admin/resources.html", nil)
	assetRec := httptest.NewRecorder()
	m.router.ServeHTTP(assetRec, assetReq)
	if assetRec.Code != http.StatusUnauthorized {
		t.Errorf("unauthenticated asset request: status = %d, want 401; body=%s", assetRec.Code, assetRec.Body.String())
	}
}

// TestInfraAdmin_AuthenticatedRequest_ReachesHandler is the
// positive counterpart — a valid Bearer token lets the request
// flow through the auth middleware to the handler, which then
// applies its own default-deny / authz check. Pins that auth is
// NOT a blanket-deny — properly authenticated requests still get
// to the typed protojson API surface.
func TestInfraAdmin_AuthenticatedRequest_ReachesHandler(t *testing.T) {
	app, _, _, _ := newAuthEnabledApp(t, "digitalocean")
	m := NewInfraAdmin("infra-admin", configToMap(t, standardAuthCfg())).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/infra-admin/resources",
		bytes.NewReader([]byte(`{"evidence":{"authz_checked":true,"authz_allowed":true,"subject":"user:alice"}}`)))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("authenticated request: status = %d, want 200; body=%s", rec.Code, rec.Body.String())
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

// ── T4: auth refuse-empty + authz_module + subject propagation ────────────

// recordingLogger captures log messages from app.Logger() calls so
// tests can assert on specific warning strings.
type recordingLogger struct {
	mu   sync.Mutex
	msgs []string
}

func (l *recordingLogger) Debug(msg string, _ ...any) {}
func (l *recordingLogger) Info(msg string, _ ...any)  {}
func (l *recordingLogger) Warn(msg string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.msgs = append(l.msgs, msg)
}
func (l *recordingLogger) Error(msg string, _ ...any) {}
func (l *recordingLogger) lastWarn() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.msgs) == 0 {
		return ""
	}
	return l.msgs[len(l.msgs)-1]
}

// recordingApp wraps infraMockApp and uses a recordingLogger.
type recordingApp struct {
	*infraMockApp
	logger *recordingLogger
}

func newRecordingApp(base *infraMockApp) *recordingApp {
	return &recordingApp{infraMockApp: base, logger: &recordingLogger{}}
}

func (a *recordingApp) Logger() modular.Logger { return a.logger }

// stubEnforcer is a minimal Enforcer for T4 tests.
type stubEnforcer struct {
	allowed bool
}

func (e *stubEnforcer) Enforce(_, _, _ string, _ ...string) (bool, error) {
	return e.allowed, nil
}

// TestInfraAdmin_Init_AuthModuleRequired asserts that Init returns an
// error when auth_module is empty and allow_unauthenticated is false.
func TestInfraAdmin_Init_AuthModuleRequired(t *testing.T) {
	app, _, _ := newInfraAdminTestApp(t, "digitalocean")
	cfg := standardCfg()
	cfg.AllowUnauthenticated = false // explicit false
	cfg.AuthModule = ""

	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	err := m.Init(app)
	if err == nil {
		t.Fatal("Init with no auth_module and allow_unauthenticated:false should return error")
	}
	const wantSubstr = "auth_module required"
	if !strings.Contains(err.Error(), wantSubstr) {
		t.Errorf("Init error %q should contain %q", err.Error(), wantSubstr)
	}
}

// TestInfraAdmin_Init_AllowUnauthenticatedNoError asserts that Init
// succeeds with allow_unauthenticated:true and no auth_module, and
// logs the exact warning string pinned by plan-review M-1.
func TestInfraAdmin_Init_AllowUnauthenticatedNoError(t *testing.T) {
	base, _, _ := newInfraAdminTestApp(t, "digitalocean")
	app := newRecordingApp(base)
	cfg := standardCfg() // already has AllowUnauthenticated:true
	cfg.AuthModule = ""

	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init with allow_unauthenticated:true should not error: %v", err)
	}

	const wantWarn = "infra.admin: mutation routes DISABLED (no auth_module); reads only"
	if got := app.logger.lastWarn(); got != wantWarn {
		t.Errorf("warning = %q, want %q", got, wantWarn)
	}
}

// TestInfraAdmin_Init_AuthzModuleResolved asserts that a configured
// authz_module is resolved as an Enforcer at Init.
func TestInfraAdmin_Init_AuthzModuleResolved(t *testing.T) {
	base, _, _ := newInfraAdminTestApp(t, "digitalocean")
	enforcer := &stubEnforcer{allowed: true}
	if err := base.RegisterService("my-authz", enforcer); err != nil {
		t.Fatalf("setup: %v", err)
	}

	cfg := standardCfg()
	cfg.AuthzModule = "my-authz"

	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(base); err != nil {
		t.Fatalf("Init with authz_module should not error: %v", err)
	}
	if m.authz == nil {
		t.Error("m.authz should be non-nil after Init with authz_module configured")
	}
}

// TestInfraAdmin_Init_AuthzModuleListedInDependencies asserts that a
// configured authz_module appears in both Dependencies() and
// RequiresServices() so the engine init-orders it before infra.admin.
func TestInfraAdmin_Init_AuthzModuleListedInDependencies(t *testing.T) {
	cfg := standardCfg()
	cfg.AuthzModule = "my-authz"

	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)

	foundDep := false
	for _, d := range m.Dependencies() {
		if d == "my-authz" {
			foundDep = true
		}
	}
	if !foundDep {
		t.Error("authz_module not in Dependencies()")
	}

	foundSvc := false
	for _, s := range m.RequiresServices() {
		if s.Name == "my-authz" {
			foundSvc = true
		}
	}
	if !foundSvc {
		t.Error("authz_module not in RequiresServices()")
	}
}

// TestInfraAdmin_SubjectFromRequest asserts that subjectFromRequest
// extracts the "sub" claim from the auth middleware's context value.
func TestInfraAdmin_SubjectFromRequest(t *testing.T) {
	m := &InfraAdmin{name: "test"}

	// No claims in context → empty string.
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if got := m.subjectFromRequest(req); got != "" {
		t.Errorf("no claims: want \"\", got %q", got)
	}

	// Claims with "sub" → return sub.
	claims := map[string]any{"sub": "user:alice", "email": "alice@example.com"}
	ctx := context.WithValue(req.Context(), authClaimsContextKey, claims)
	req2 := req.WithContext(ctx)
	if got := m.subjectFromRequest(req2); got != "user:alice" {
		t.Errorf("with claims: want \"user:alice\", got %q", got)
	}

	// Claims without "sub" → empty string.
	claims2 := map[string]any{"email": "bob@example.com"}
	ctx2 := context.WithValue(req.Context(), authClaimsContextKey, claims2)
	req3 := req.WithContext(ctx2)
	if got := m.subjectFromRequest(req3); got != "" {
		t.Errorf("claims without sub: want \"\", got %q", got)
	}
}

// ── T8: mutation route + requireBearer + audit 3-way tests ───────────────────

// startMutationModule is a helper that boots an InfraAdmin module with
// auth enabled (so mutation routes are registered).
func startMutationModule(t *testing.T) (*InfraAdmin, *authMwStub) {
	t.Helper()
	app, _, _, auth := newAuthEnabledApp(t, "digitalocean")
	m := NewInfraAdmin("infra-admin", configToMap(t, standardAuthCfg())).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatalf("router.Start: %v", err)
	}
	return m, auth
}

// TestInfraAdmin_MutationRoutesRegistered asserts that 4 mutation routes
// are registered when auth_module is configured.
func TestInfraAdmin_MutationRoutesRegistered(t *testing.T) {
	m, _ := startMutationModule(t)
	mutRoutes := []string{"/plan", "/apply", "/destroy", "/drift"}
	for _, route := range mutRoutes {
		req := httptest.NewRequest(http.MethodPost, "/api/infra-admin"+route,
			bytes.NewReader([]byte(`{}`)))
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		m.router.ServeHTTP(rec, req)
		// Should NOT be 404 (route must exist); anything else is acceptable
		// from the handler (may be 200 with error in body).
		if rec.Code == http.StatusNotFound {
			t.Errorf("mutation route %s not registered (got 404)", route)
		}
	}
}

// TestInfraAdmin_MutationRouteAbsentWithoutAuth asserts that mutation routes
// are NOT registered when allow_unauthenticated:true (no auth_module).
func TestInfraAdmin_MutationRouteAbsentWithoutAuth(t *testing.T) {
	app, _, _ := newAppWithWorkflowSection(t, "digitalocean")
	cfg := standardCfg() // AllowUnauthenticated:true, no AuthModule
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatalf("router.Start: %v", err)
	}
	for _, route := range []string{"/plan", "/apply", "/destroy", "/drift"} {
		req := httptest.NewRequest(http.MethodPost, "/api/infra-admin"+route,
			bytes.NewReader([]byte(`{}`)))
		rec := httptest.NewRecorder()
		m.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusNotFound {
			t.Errorf("mutation route %s should be absent (no auth_module), got %d", route, rec.Code)
		}
	}
}

// TestInfraAdmin_MutationRequiresBearerToken asserts that mutation routes
// return 401 when the Authorization: Bearer header is missing, even when
// the auth middleware lets the request through.
func TestInfraAdmin_MutationRequiresBearerToken(t *testing.T) {
	m, _ := startMutationModule(t)
	// No Authorization header at all.
	req := httptest.NewRequest(http.MethodPost, "/api/infra-admin/plan",
		bytes.NewReader([]byte(`{}`)))
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)
	// The auth stub lets it through, but requireBearer should reject it.
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("mutation without Bearer: status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
}

// TestInfraAdmin_AuditResultFor3Way asserts the 3-way classification
// of auditResultFor.
func TestInfraAdmin_AuditResultFor3Way(t *testing.T) {
	cases := []struct {
		errMsg string
		want   string
	}{
		{"", "ok"},
		{"authz evidence missing — admin middleware did not attach", "denied"},
		{"apply: infra:apply denied", "denied"},
		{"apply: plan is stale (desired_hash mismatch)", "denied"},
		{"apply: list state: connection refused", "error"},
		{"plan: no iac.provider registered", "error"},
	}
	for _, tc := range cases {
		got := auditResultFor(tc.errMsg)
		if got != tc.want {
			t.Errorf("auditResultFor(%q) = %q, want %q", tc.errMsg, got, tc.want)
		}
	}
}

// ── T9: named security regression suite ──────────────────────────────────────

// TestInfraAdmin_MutationRequiresBearer is the canonical CSRF regression:
// mutation routes MUST reject requests without Authorization: Bearer.
// (Renamed version of TestInfraAdmin_MutationRequiresBearerToken — same
// contract, keeps the T9 name the plan locked.)
func TestInfraAdmin_MutationRequiresBearer(t *testing.T) {
	m, _ := startMutationModule(t)
	for _, path := range []string{"/plan", "/apply", "/destroy", "/drift"} {
		req := httptest.NewRequest(http.MethodPost, "/api/infra-admin"+path,
			bytes.NewReader([]byte(`{}`)))
		// Explicitly no Authorization header.
		rec := httptest.NewRecorder()
		m.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("%s without Bearer: want 401, got %d; body=%s", path, rec.Code, rec.Body.String())
		}
	}
}

// TestInfraAdmin_ApplyRejectsStalePlanHash is the TOCTOU regression:
// an apply request whose desired_hash does not match the in-process config
// MUST be rejected before any cloud operation runs.
func TestInfraAdmin_ApplyRejectsStalePlanHash(t *testing.T) {
	m, _ := startMutationModule(t)

	body := `{"plan_id":"p1","desired_hash":"stale-deliberately-wrong","evidence":{"authz_checked":true,"authz_allowed":true}}`
	req := httptest.NewRequest(http.MethodPost, "/api/infra-admin/apply",
		bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)

	// Stale hash is a provider/backend error → HTTP 500 (Bug 3 fix: writeMutationResponse
	// maps non-authz output.Error → 500 so provider-error("denied" text) ≠ 403).
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("stale hash: want 500 (non-authz output.Error), got %d; body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "stale") {
		t.Errorf("expected stale-hash error in response, got: %s", rec.Body.String())
	}
}

// TestInfraAdmin_ConcurrentApplyReturns409 is the single-flight regression.
// It drives TWO goroutines concurrently against the same provider — one
// holds the mutex directly (simulating an in-flight apply) while the other
// hits the route and must see 409. A sequential variant would falsely pass
// (plan-review M-2).
func TestInfraAdmin_ConcurrentApplyReturns409(t *testing.T) {
	m, _ := startMutationModule(t)

	// Manually lock the first provider's mutex to simulate an in-flight apply.
	var held *sync.Mutex
	for _, pm := range m.config.ProviderModules {
		if mu, ok := m.providerMu[pm]; ok {
			held = mu
			break
		}
	}
	if held == nil {
		t.Skip("no provider mutex found (no ProviderModules configured)")
	}
	held.Lock()
	defer held.Unlock()

	// Now an apply request MUST see 409 (mutex already locked).
	body := `{"plan_id":"p1","desired_hash":"any","evidence":{"authz_checked":true,"authz_allowed":true}}`
	req := httptest.NewRequest(http.MethodPost, "/api/infra-admin/apply",
		bytes.NewReader([]byte(body)))
	req.Header.Set("Authorization", "Bearer test-token")
	rec := httptest.NewRecorder()

	// Run the request in a goroutine to properly simulate concurrency.
	done := make(chan struct{})
	go func() {
		defer close(done)
		m.router.ServeHTTP(rec, req)
	}()
	<-done

	if rec.Code != http.StatusConflict {
		t.Errorf("concurrent apply: want 409, got %d; body=%s", rec.Code, rec.Body.String())
	}
}

// TestInfraAdmin_ViewerCannotApply is the write-tier RBAC regression:
// a subject that the authz module grants only infra:read MUST receive an
// error on apply/destroy routes, server-side, regardless of what the
// client body asserts in evidence.granted_permissions.
func TestInfraAdmin_ViewerCannotApply(t *testing.T) {
	app, _, _, _ := newAuthEnabledApp(t, "digitalocean")
	enforcer := &stubEnforcer{allowed: false}                         // denies everything
	if err := app.RegisterService("my-authz", enforcer); err != nil { // F3 fix
		t.Fatalf("setup: %v", err)
	}

	cfg := standardAuthCfg()
	cfg.AuthzModule = "my-authz"

	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatalf("router.Start: %v", err)
	}

	viewerCtx := func(r *http.Request) *http.Request {
		ctx := context.WithValue(r.Context(), authClaimsContextKey, map[string]any{"sub": "viewer"})
		return r.WithContext(ctx)
	}

	// Apply: client claims allowed, server Enforcer denies → HTTP 403 (Bug 3 fix).
	applyReq := viewerCtx(httptest.NewRequest(http.MethodPost, "/api/infra-admin/apply",
		bytes.NewReader([]byte(`{"evidence":{"authz_checked":true,"authz_allowed":true},"desired_hash":"any"}`))))
	applyReq.Header.Set("Authorization", "Bearer test-token")
	applyRec := httptest.NewRecorder()
	m.router.ServeHTTP(applyRec, applyReq)
	if applyRec.Code != http.StatusForbidden {
		t.Fatalf("apply: want 403 (typed ErrAuthzDenied), got %d; body=%s", applyRec.Code, applyRec.Body.String())
	}
	if !strings.Contains(applyRec.Body.String(), "denied") {
		t.Errorf("viewer apply should be denied by server-side Enforcer; body=%s", applyRec.Body.String())
	}

	// Destroy: same enforcer denies infra:destroy too (F1 fix — cover destroy route).
	destroyReq := viewerCtx(httptest.NewRequest(http.MethodPost, "/api/infra-admin/destroy",
		bytes.NewReader([]byte(`{"refs":[{"name":"vpc1","type":"infra.vpc"}],"confirm_hash":"any","evidence":{"authz_checked":true,"authz_allowed":true}}`))))
	destroyReq.Header.Set("Authorization", "Bearer test-token")
	destroyRec := httptest.NewRecorder()
	m.router.ServeHTTP(destroyRec, destroyReq)
	if destroyRec.Code != http.StatusForbidden {
		t.Fatalf("destroy: want 403 (typed ErrAuthzDenied), got %d; body=%s", destroyRec.Code, destroyRec.Body.String())
	}
	if !strings.Contains(destroyRec.Body.String(), "denied") {
		t.Errorf("viewer destroy should be denied by server-side Enforcer; body=%s", destroyRec.Body.String())
	}
}

// deniedPlanProvider wraps infraMockProvider and overrides Plan to return
// an error whose message contains "denied" — used by the provider-error→500
// discriminator test to verify that strings.Contains("denied") is NOT used
// for HTTP status classification (typed ErrAuthzDenied sentinel instead).
type deniedPlanProvider struct {
	infraMockProvider
}

func (p *deniedPlanProvider) Plan(_ context.Context, _ []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	return nil, errors.New("provider: access denied to cloud API")
}

// TestInfraAdmin_TypedAuthzDenied_Returns403 pins the 4 typed HTTP-status
// discriminator behaviors introduced by Bug 3 + Bug 4:
//
//	viewer→/plan=403  (handlePlanResource Enforcer gate, Bug 4)
//	viewer→/apply=403 (handler.ErrAuthzDenied → writeMutationResponse, Bug 3)
//	operator→/apply=200 (happy path unaffected)
//	provider-error("denied" text)→500 NOT 403 (strings.Contains FP eliminated)
func TestInfraAdmin_TypedAuthzDenied_Returns403(t *testing.T) {
	// subjectCtx injects a JWT "sub" claim into the request context
	// so subjectFromRequest() extracts the right principal.
	subjectCtx := func(r *http.Request, sub string) *http.Request {
		ctx := context.WithValue(r.Context(), authClaimsContextKey, map[string]any{"sub": sub})
		return r.WithContext(ctx)
	}

	// startWithConfig boots an InfraAdmin with auth+authz+provider registered.
	startWithConfig := func(t *testing.T, enforcer Enforcer, prov interfaces.IaCProvider) *InfraAdmin {
		t.Helper()
		app, _, _, _ := newAuthEnabledApp(t, "digitalocean")
		if enforcer != nil {
			if err := app.RegisterService("my-authz", enforcer); err != nil {
				t.Fatalf("setup: RegisterService(my-authz): %v", err)
			}
		}
		if prov != nil {
			// Override the default do-provider so the module uses our custom one.
			if err := app.RegisterService("do-provider", prov); err != nil {
				t.Fatalf("setup: RegisterService(do-provider): %v", err)
			}
		}
		cfg := standardAuthCfg()
		if enforcer != nil {
			cfg.AuthzModule = "my-authz"
		}
		m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
		if err := m.Init(app); err != nil {
			t.Fatalf("Init: %v", err)
		}
		if err := m.Start(context.Background()); err != nil {
			t.Fatalf("Start: %v", err)
		}
		if err := m.router.Start(context.Background()); err != nil {
			t.Fatalf("router.Start: %v", err)
		}
		return m
	}

	// ── viewer→/plan=403 ─────────────────────────────────────────────────────
	// handlePlanResource now calls m.authz.Enforce before handler.PlanResource
	// (Bug 4 fix). Viewer subject denied → HTTP 403 directly from module layer.
	// Additional assertions (Copilot findings):
	//   - Body is proto-JSON AdminPlanOutput (not plaintext) — consistent with apply/destroy 403s.
	//   - Body does NOT contain the subject string "viewer" — no principal leak.
	t.Run("viewer/plan=403", func(t *testing.T) {
		enforcer := &stubEnforcer{allowed: false}
		m := startWithConfig(t, enforcer, nil)

		req := subjectCtx(httptest.NewRequest(http.MethodPost, "/api/infra-admin/plan",
			bytes.NewReader([]byte(`{"evidence":{"authz_checked":true,"authz_allowed":true}}`))), "viewer")
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		m.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("viewer/plan: want 403, got %d; body=%s", rec.Code, rec.Body.String())
		}
		// Body must be proto-JSON AdminPlanOutput, not plaintext (Copilot finding 1+2).
		if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
			t.Errorf("viewer/plan 403: Content-Type = %q, want application/json (proto-JSON body)", ct)
		}
		var planOut adminpb.AdminPlanOutput
		if err := protojson.Unmarshal(rec.Body.Bytes(), &planOut); err != nil {
			t.Errorf("viewer/plan 403: body is not valid AdminPlanOutput JSON: %v\n%s", err, rec.Body.String())
		}
		// Subject must NOT appear in response body — no principal leakage.
		if strings.Contains(rec.Body.String(), "viewer") {
			t.Errorf("viewer/plan 403: body contains subject 'viewer' — principal leak; body=%s", rec.Body.String())
		}
	})

	// ── viewer→/apply=403 ────────────────────────────────────────────────────
	// handler.ApplyResource Gate 2 returns ErrAuthzDenied → writeMutationResponse → 403.
	t.Run("viewer/apply=403", func(t *testing.T) {
		enforcer := &stubEnforcer{allowed: false}
		m := startWithConfig(t, enforcer, nil)

		// Compute correct desired_hash for empty desiredSpecs + empty state.
		hash := handler.DesiredHash(nil, nil, nil)
		body := `{"desired_hash":"` + hash + `","evidence":{"authz_checked":true,"authz_allowed":true}}`
		req := subjectCtx(httptest.NewRequest(http.MethodPost, "/api/infra-admin/apply",
			bytes.NewReader([]byte(body))), "viewer")
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		m.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("viewer/apply: want 403, got %d; body=%s", rec.Code, rec.Body.String())
		}
	})

	// ── operator→/apply=200 ──────────────────────────────────────────────────
	// operator is allowed → plan succeeds (empty desired specs) → apply succeeds → 200.
	t.Run("operator/apply=200", func(t *testing.T) {
		enforcer := &stubEnforcer{allowed: true}
		m := startWithConfig(t, enforcer, nil)

		hash := handler.DesiredHash(nil, nil, nil)
		body := `{"desired_hash":"` + hash + `","evidence":{"authz_checked":true,"authz_allowed":true}}`
		req := subjectCtx(httptest.NewRequest(http.MethodPost, "/api/infra-admin/apply",
			bytes.NewReader([]byte(body))), "operator")
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		m.router.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("operator/apply: want 200, got %d; body=%s", rec.Code, rec.Body.String())
		}
	})

	// ── provider-error("denied" text)→500 NOT 403 ───────────────────────────
	// The provider's Plan returns an error whose message contains "denied".
	// writeMutationResponse must use errors.Is(err, ErrAuthzDenied) — NOT
	// strings.Contains — so this maps to 500 (backend error), NOT 403 (authz).
	t.Run("provider-error-denied-text/apply=500-not-403", func(t *testing.T) {
		enforcer := &stubEnforcer{allowed: true}
		prov := &deniedPlanProvider{infraMockProvider: infraMockProvider{name: "digitalocean"}}
		m := startWithConfig(t, enforcer, prov)

		hash := handler.DesiredHash(nil, nil, nil)
		body := `{"desired_hash":"` + hash + `","evidence":{"authz_checked":true,"authz_allowed":true}}`
		req := subjectCtx(httptest.NewRequest(http.MethodPost, "/api/infra-admin/apply",
			bytes.NewReader([]byte(body))), "operator")
		req.Header.Set("Authorization", "Bearer test-token")
		rec := httptest.NewRecorder()
		m.router.ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Errorf("provider-error with 'denied' text: got 403 (strings.Contains FP!) want 500; body=%s", rec.Body.String())
		}
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("provider-error with 'denied' text: want 500, got %d; body=%s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "denied") {
			t.Errorf("provider-error body should contain 'denied' (provider error text); got: %s", rec.Body.String())
		}
	})
}

// TestInfraAdmin_AuditDistinguishesDeniedFromError verifies that the
// 3-way audit classification correctly distinguishes authz denials from
// backend errors (extended from T8's TestInfraAdmin_AuditResultFor3Way).
func TestInfraAdmin_AuditDistinguishesDeniedFromError(t *testing.T) {
	// Denial (authz/evidence/stale markers) → "denied"
	for _, msg := range []string{
		"authz evidence missing",
		"infra:apply denied",
		"plan is stale (desired_hash mismatch)",
	} {
		if got := auditResultFor(msg); got != "denied" {
			t.Errorf("auditResultFor(%q) = %q, want 'denied'", msg, got)
		}
	}
	// Error (provider failure) → "error"
	for _, msg := range []string{
		"apply: list state: connection refused",
		"plan: no iac.provider registered",
		"destroy: provider timeout",
	} {
		if got := auditResultFor(msg); got != "error" {
			t.Errorf("auditResultFor(%q) = %q, want 'error'", msg, got)
		}
	}
}

// TestInfraAdmin_AuditResultFromErr pins auditResultFromErr — the typed
// mutation-route classifier that replaces strings.Contains in the audit
// path. The key regression: a provider error whose message contains
// "denied" must log as "error", NOT "denied".
func TestInfraAdmin_AuditResultFromErr(t *testing.T) {
	cases := []struct {
		name   string
		err    error
		outErr string
		want   string
	}{
		{"success", nil, "", "ok"},
		{"authz sentinel", handler.ErrAuthzDenied, "apply: infra:apply denied", "denied"},
		// The critical false-positive regression: provider error containing
		// "denied" must NOT be classified as "denied" (strings.Contains would
		// have done so). Only errors.Is(ErrAuthzDenied) triggers "denied".
		{"provider error with denied text", nil, "apply: plan: provider: access denied to cloud API", "error"},
		{"stale hash", nil, "apply: plan is stale (desired_hash mismatch)", "error"},
		{"no provider registered", nil, "plan: no iac.provider registered", "error"},
		{"evidence denial via sentinel", handler.ErrAuthzDenied, "authz evidence missing", "denied"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := auditResultFromErr(tc.err, tc.outErr)
			if got != tc.want {
				t.Errorf("auditResultFromErr(%v, %q) = %q, want %q", tc.err, tc.outErr, got, tc.want)
			}
		})
	}
}
