package module

// MutationIntegration tests wire the infra.admin module end-to-end without
// BuildFromConfig (per ADR-0003→v4 lesson that BuildFromConfig makes test
// setup brittle — manual wiring is explicit about what's under test).
//
// Wiring: auth stub + recordingStateStore + stubprovider.New() (T2) +
// stubEnforcer (I-2) + infra.admin module + audit log.
//
// Assertions per T10 spec:
//   (1) state store gains the resource after apply (C-1: handler saves state)
//   (2) AdminAuditEntry{action:apply, result:ok} written to audit log
//   (3) applied[] in response body non-empty (C-2: non-empty desiredSpecs)
//   After DestroyResource: AdminAuditEntry{action:destroy, result:ok}

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/iac/stubprovider"
	"github.com/GoCodeAlone/workflow/interfaces"
	"google.golang.org/protobuf/encoding/protojson"
)

// recordingStateStore captures SaveResource calls so integration tests
// can assert assertion (1): state store gains the resource.
type recordingStateStore struct {
	mu        sync.Mutex
	resources []interfaces.ResourceState
}

func (s *recordingStateStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]interfaces.ResourceState, len(s.resources))
	copy(out, s.resources)
	return out, nil
}
func (s *recordingStateStore) GetResource(_ context.Context, name string) (*interfaces.ResourceState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.resources {
		if s.resources[i].Name == name {
			r := s.resources[i]
			return &r, nil
		}
	}
	return nil, nil
}
func (s *recordingStateStore) SaveResource(_ context.Context, state interfaces.ResourceState) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Upsert: replace existing by name or append.
	for i := range s.resources {
		if s.resources[i].Name == state.Name {
			s.resources[i] = state
			return nil
		}
	}
	s.resources = append(s.resources, state)
	return nil
}
func (s *recordingStateStore) DeleteResource(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.resources {
		if s.resources[i].Name == name {
			s.resources = append(s.resources[:i], s.resources[i+1:]...)
			return nil
		}
	}
	return nil
}
func (s *recordingStateStore) SavePlan(_ context.Context, _ interfaces.IaCPlan) error { return nil }
func (s *recordingStateStore) GetPlan(_ context.Context, _ string) (*interfaces.IaCPlan, error) {
	return nil, nil
}
func (s *recordingStateStore) Lock(_ context.Context, _ string, _ time.Duration) (interfaces.IaCLockHandle, error) {
	return nil, nil
}
func (s *recordingStateStore) Close() error { return nil }

// integrationEnforcer always grants access (integration happy-path).
// calls is bumped atomically so tests can assert Enforce was actually
// invoked (per spec-reviewer T10 non-blocking note).
type integrationEnforcer struct {
	calls int64
}

func (e *integrationEnforcer) Enforce(_, _, _ string, _ ...string) (bool, error) {
	atomic.AddInt64(&e.calls, 1)
	return true, nil
}

// mutationIntegrationApp manually wires the infra.admin module with:
// - auth stub + authz stub enforcer (I-2)
// - recordingStateStore (C-1 assertion)
// - stub iac.provider (T2: stubprovider.New())
// - WorkflowConfig with one infra.database resource (C-2: non-empty desiredSpecs)
func mutationIntegrationApp(t *testing.T, auditPath string) (*withConfigSectionApp, *InfraAdmin, *recordingStateStore, *integrationEnforcer) {
	t.Helper()
	providerName := "stub-provider"

	// Base app with auth + workflow config section.
	app, _, _ := newAppWithWorkflowSection(t, "stub")

	// WorkflowConfig: one iac.provider + one infra.database (C-2: non-empty
	// desiredSpecs so plan produces real actions and applied[] is non-empty).
	section := &wfConfigSection{cfg: &config.WorkflowConfig{
		Modules: []config.ModuleConfig{
			{Name: providerName, Type: "iac.provider", Config: map[string]any{"provider": "stub"}},
			{Name: "db1", Type: "infra.database", Config: map[string]any{"size": "s", "engine": "pg14"}},
		},
	}}
	app.services["__workflow_section__"] = section
	// withConfigSectionApp caches the section at construction time — replace it
	// so GetConfigSection("workflow") returns the new config.
	app.section = section

	// Auth stub + authz stub enforcer (I-2).
	auth := &authMwStub{name: "auth"}
	enforcer := &integrationEnforcer{}
	mustRegister(t, app, "auth", auth)
	mustRegister(t, app, "my-authz", enforcer)

	// Stub provider registered under its module name (per T2).
	mustRegister(t, app, providerName, stubprovider.New())

	// Recording state store so we can assert assertion (1).
	store := &recordingStateStore{}
	mustRegister(t, app, "iac-state", store)

	cfg := InfraAdminConfig{
		RoutePrefix:           "/api/infra-admin",
		AssetPrefix:           "/admin/infra-admin",
		StateModule:           "iac-state",
		HTTPModule:            "http-router",
		SecurityHeadersModule: "security-headers",
		AuthModule:            "auth",
		AuthzModule:           "my-authz", // I-2: wire authz so Enforce is called
		ProviderModules:       []string{providerName},
		AccessLogPath:         auditPath,
	}
	m := NewInfraAdmin("infra-admin", configToMap(t, cfg)).(*InfraAdmin)
	return app, m, store, enforcer
}

// TestMutationIntegration_Apply verifies the end-to-end apply path:
//   - POST /plan → plan_id + desired_hash non-empty (I-1)
//   - POST /apply → applied[] non-empty (C-2), state store gains resource (C-1),
//     audit entry {action:apply, result:ok} (assertion 2)
func TestMutationIntegration_Apply(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	app, m, store, enforcer := mutationIntegrationApp(t, auditPath)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatalf("router.Start: %v", err)
	}

	// Step 1: Plan.
	planBody := `{"evidence":{"authz_checked":true,"authz_allowed":true,"subject":"operator"}}`
	planReq := httptest.NewRequest(http.MethodPost, "/api/infra-admin/plan",
		bytes.NewReader([]byte(planBody)))
	planReq.Header.Set("Authorization", "Bearer test-token")
	ctx := context.WithValue(planReq.Context(), authClaimsContextKey, map[string]any{"sub": "operator"})
	planReq = planReq.WithContext(ctx)
	planRec := httptest.NewRecorder()
	m.router.ServeHTTP(planRec, planReq)
	if planRec.Code != http.StatusOK {
		t.Fatalf("plan: status %d; body=%s", planRec.Code, planRec.Body.String())
	}

	var planOut adminpb.AdminPlanOutput
	if err := protojson.Unmarshal(planRec.Body.Bytes(), &planOut); err != nil {
		t.Fatalf("plan: decode response: %v\n%s", err, planRec.Body.String())
	}

	// Assertion I-1: plan_id and desired_hash must be non-empty.
	if planOut.GetPlanId() == "" {
		t.Error("plan_id must be non-empty")
	}
	if planOut.GetDesiredHash() == "" {
		t.Error("desired_hash must be non-empty")
	}
	if planOut.GetError() != "" {
		t.Fatalf("plan: unexpected error: %s", planOut.GetError())
	}

	// Step 2: Apply with the hash from the plan response.
	applyPayload := mustMarshal(t, map[string]any{
		"plan_id":      planOut.GetPlanId(),
		"desired_hash": planOut.GetDesiredHash(),
		"evidence":     map[string]any{"authz_checked": true, "authz_allowed": true, "subject": "operator"},
	})
	applyReq := httptest.NewRequest(http.MethodPost, "/api/infra-admin/apply",
		bytes.NewReader(applyPayload))
	applyReq.Header.Set("Authorization", "Bearer test-token")
	applyCtx := context.WithValue(applyReq.Context(), authClaimsContextKey, map[string]any{"sub": "operator"})
	applyReq = applyReq.WithContext(applyCtx)
	applyRec := httptest.NewRecorder()
	m.router.ServeHTTP(applyRec, applyReq)

	if applyRec.Code != http.StatusOK {
		t.Fatalf("apply: status %d; body=%s", applyRec.Code, applyRec.Body.String())
	}

	var applyOut adminpb.AdminApplyOutput
	if err := protojson.Unmarshal(applyRec.Body.Bytes(), &applyOut); err != nil {
		t.Fatalf("apply: decode response: %v\n%s", err, applyRec.Body.String())
	}
	if applyOut.GetError() != "" {
		t.Errorf("apply: unexpected error: %s", applyOut.GetError())
	}

	// Assertion (3): applied[] non-empty (C-2 fix: WorkflowConfig has db1 infra.database).
	if len(applyOut.GetApplied()) == 0 {
		t.Error("apply: applied[] should be non-empty — desiredSpecs has 1 infra.database resource")
	}

	// Assertion (1): state store gains the resource (C-1 fix: handler now calls SaveResource).
	stateRows, err := store.ListResources(context.Background())
	if err != nil {
		t.Fatalf("apply: ListResources: %v", err)
	}
	if len(stateRows) == 0 {
		t.Error("apply: state store should have at least 1 resource after apply")
	} else {
		found := false
		for _, row := range stateRows {
			if row.Name == "db1" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("apply: state store missing 'db1'; rows: %v", stateRows)
		}
	}

	// Assertion (2): audit entry with action:apply result:ok.
	assertAuditEntry(t, auditPath, "apply", "ok")

	// I-2 verification: Enforce was invoked (spec-reviewer T10 non-blocking note).
	if atomic.LoadInt64(&enforcer.calls) == 0 {
		t.Error("authz Enforce was never called during apply — authz module not wired correctly")
	}
}

// TestMutationIntegration_Destroy verifies the end-to-end destroy path:
// POST /destroy with confirm_hash → destroyed[] + audit entry ok
func TestMutationIntegration_Destroy(t *testing.T) {
	dir := t.TempDir()
	auditPath := filepath.Join(dir, "audit.jsonl")

	app, m, _, _ := mutationIntegrationApp(t, auditPath)
	if err := m.Init(app); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := m.router.Start(context.Background()); err != nil {
		t.Fatalf("router.Start: %v", err)
	}

	// Compute confirm_hash from refs for TOCTOU gate (T7 IMPORTANT-1 / I-3).
	refs := []*adminpb.AdminResourceRef{
		{Name: "vpc1", Type: "infra.vpc"},
		{Name: "db1", Type: "infra.database"},
	}
	confirmHash := handler.HashDestroyRefs(refs)
	destroyPayload := mustMarshal(t, map[string]any{
		"refs":         []map[string]string{{"name": "vpc1", "type": "infra.vpc"}, {"name": "db1", "type": "infra.database"}},
		"confirm_hash": confirmHash,
		"evidence":     map[string]any{"authz_checked": true, "authz_allowed": true, "subject": "operator"},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/infra-admin/destroy",
		bytes.NewReader(destroyPayload))
	req.Header.Set("Authorization", "Bearer test-token")
	ctx := context.WithValue(req.Context(), authClaimsContextKey, map[string]any{"sub": "operator"})
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	m.router.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("destroy: status %d; body=%s", rec.Code, rec.Body.String())
	}
	var out adminpb.AdminDestroyOutput
	if err := protojson.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("destroy: decode: %v\n%s", err, rec.Body.String())
	}
	if out.GetError() != "" {
		t.Errorf("destroy: unexpected error: %s", out.GetError())
	}
	if len(out.GetDestroyed()) != 2 {
		t.Errorf("destroy: expected 2 destroyed, got %d: %v", len(out.GetDestroyed()), out.GetDestroyed())
	}

	// Assertion (2): audit entry with action:destroy result:ok.
	assertAuditEntry(t, auditPath, "destroy", "ok")
}

// assertAuditEntry scans the audit JSONL file for an entry matching
// the given action + result. Fails the test if none is found.
func assertAuditEntry(t *testing.T, auditPath, action, result string) {
	t.Helper()
	f, err := os.Open(auditPath)
	if err != nil {
		t.Fatalf("open audit log %q: %v", auditPath, err)
	}
	defer f.Close() //nolint:errcheck

	type entry struct {
		Action string `json:"action"`
		Result string `json:"result"`
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		var e entry
		if err := json.Unmarshal(line, &e); err != nil {
			continue
		}
		if strings.EqualFold(e.Action, action) && strings.EqualFold(e.Result, result) {
			return // found
		}
	}
	t.Errorf("audit log %q missing entry {action:%q, result:%q}", auditPath, action, result)
}

// mustRegister is a test helper that calls app.RegisterService and
// fatalf on error, replacing _ = app.RegisterService(...) patterns.
func mustRegister(t *testing.T, app interface {
	RegisterService(string, any) error
}, name string, svc any) {
	t.Helper()
	if err := app.RegisterService(name, svc); err != nil {
		t.Fatalf("setup: RegisterService(%q): %v", name, err)
	}
}

// mustMarshal is a test helper that calls json.Marshal and fatalf on error.
func mustMarshal(t *testing.T, v any) []byte {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("setup: json.Marshal: %v", err)
	}
	return data
}

// Compile-time: interfaces.IaCProvider satisfied by stubprovider.Provider.
var _ interfaces.IaCProvider = (*stubprovider.Provider)(nil)
