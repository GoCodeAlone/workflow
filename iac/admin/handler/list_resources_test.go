package handler_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/iac/admin/catalog"
	"github.com/GoCodeAlone/workflow/iac/admin/handler"
	adminpb "github.com/GoCodeAlone/workflow/iac/admin/proto"
	"github.com/GoCodeAlone/workflow/interfaces"
)

// fakeStateStore is a minimal interfaces.IaCStateStore for handler
// tests. Only the read-side subset {GetResource, ListResources} is
// exercised by the handler library; SaveResource is used by the test
// fixture setup. Out-of-subset methods panic per the wfctlhelpers
// design-cycle-5 convention so accidental misuse is loud.
type fakeStateStore struct {
	resources []interfaces.ResourceState
}

func (s *fakeStateStore) ListResources(_ context.Context) ([]interfaces.ResourceState, error) {
	out := make([]interfaces.ResourceState, len(s.resources))
	copy(out, s.resources)
	return out, nil
}
func (s *fakeStateStore) GetResource(_ context.Context, name string) (*interfaces.ResourceState, error) {
	for i := range s.resources {
		if s.resources[i].Name == name {
			r := s.resources[i]
			return &r, nil
		}
	}
	return nil, nil
}
func (s *fakeStateStore) SaveResource(_ context.Context, state interfaces.ResourceState) error {
	s.resources = append(s.resources, state)
	return nil
}
func (s *fakeStateStore) DeleteResource(_ context.Context, _ string) error { return nil }
func (s *fakeStateStore) SavePlan(_ context.Context, _ interfaces.IaCPlan) error {
	panic("fakeStateStore: SavePlan out-of-subset")
}
func (s *fakeStateStore) GetPlan(_ context.Context, _ string) (*interfaces.IaCPlan, error) {
	panic("fakeStateStore: GetPlan out-of-subset")
}
func (s *fakeStateStore) Lock(_ context.Context, _ string, _ time.Duration) (interfaces.IaCLockHandle, error) {
	panic("fakeStateStore: Lock out-of-subset")
}
func (s *fakeStateStore) Close() error { return nil }

// authzOK is the standard "host authz middleware ran + allowed"
// evidence pinned for happy-path tests. Default-deny tests pass nil
// or a partial evidence to trigger the refusal branch.
func authzOK() *adminpb.AdminAuthzEvidence {
	return &adminpb.AdminAuthzEvidence{
		AuthzChecked:       true,
		AuthzAllowed:       true,
		Subject:            "user:alice",
		GrantedPermissions: []string{"infra:read"},
	}
}

// planningProvider is a minimal interfaces.IaCProvider for handler tests.
// It replaces the deleted iac/stubprovider package — scenario fixtures must
// not live in the workflow engine core. Provides real Plan, Destroy,
// DetectDrift, and ResourceDriver behavior so tests can exercise the full
// dispatch path without an external package dependency.
type planningProvider struct{}

var _ interfaces.IaCProvider = (*planningProvider)(nil)

func (p *planningProvider) Name() string    { return "test-planning" }
func (p *planningProvider) Version() string { return "0.0.0-test" }
func (p *planningProvider) Initialize(_ context.Context, _ map[string]any) error {
	return nil
}
func (p *planningProvider) Capabilities() []interfaces.IaCCapabilityDeclaration { return nil }

func (p *planningProvider) Plan(_ context.Context, desired []interfaces.ResourceSpec, current []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	currentByName := make(map[string]*interfaces.ResourceState, len(current))
	for i := range current {
		currentByName[current[i].Name] = &current[i]
	}
	desiredByName := make(map[string]struct{}, len(desired))
	for _, s := range desired {
		desiredByName[s.Name] = struct{}{}
	}
	plan := &interfaces.IaCPlan{}
	for _, spec := range desired {
		if _, exists := currentByName[spec.Name]; exists {
			plan.Actions = append(plan.Actions, interfaces.PlanAction{Action: "update", Resource: spec, Current: currentByName[spec.Name]})
		} else {
			plan.Actions = append(plan.Actions, interfaces.PlanAction{Action: "create", Resource: spec})
		}
	}
	for i := range current {
		st := &current[i]
		if _, wanted := desiredByName[st.Name]; !wanted {
			plan.Actions = append(plan.Actions, interfaces.PlanAction{Action: "delete", Resource: interfaces.ResourceSpec{Name: st.Name, Type: st.Type}, Current: st})
		}
	}
	return plan, nil
}

func (p *planningProvider) Destroy(_ context.Context, refs []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		names = append(names, r.Name)
	}
	return &interfaces.DestroyResult{Destroyed: names}, nil
}

func (p *planningProvider) Status(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	return nil, nil
}

func (p *planningProvider) DetectDrift(_ context.Context, refs []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	results := make([]interfaces.DriftResult, 0, len(refs))
	for _, r := range refs {
		results = append(results, interfaces.DriftResult{Name: r.Name, Type: r.Type, Drifted: false, Class: interfaces.DriftClassInSync})
	}
	return results, nil
}

func (p *planningProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}

func (p *planningProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}

func (p *planningProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return &planningDriver{}, nil
}

func (p *planningProvider) SupportedCanonicalKeys() []string { return nil }

func (p *planningProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}

func (p *planningProvider) Close() error { return nil }

// planningDriver is a minimal interfaces.ResourceDriver for handler tests.
type planningDriver struct{}

var _ interfaces.ResourceDriver = (*planningDriver)(nil)

func (d *planningDriver) Create(_ context.Context, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: "test-" + spec.Name}, nil
}

func (d *planningDriver) Read(_ context.Context, ref interfaces.ResourceRef) (*interfaces.ResourceOutput, error) {
	return &interfaces.ResourceOutput{Name: ref.Name, Type: ref.Type, ProviderID: ref.ProviderID}, nil
}

func (d *planningDriver) Update(_ context.Context, ref interfaces.ResourceRef, spec interfaces.ResourceSpec) (*interfaces.ResourceOutput, error) {
	pid := ref.ProviderID
	if pid == "" {
		pid = "test-" + spec.Name
	}
	return &interfaces.ResourceOutput{Name: spec.Name, Type: spec.Type, ProviderID: pid}, nil
}

func (d *planningDriver) Delete(_ context.Context, _ interfaces.ResourceRef) error { return nil }

func (d *planningDriver) Diff(_ context.Context, _ interfaces.ResourceSpec, _ *interfaces.ResourceOutput) (*interfaces.DiffResult, error) {
	return &interfaces.DiffResult{NeedsUpdate: false, NeedsReplace: false}, nil
}

func (d *planningDriver) HealthCheck(_ context.Context, _ interfaces.ResourceRef) (*interfaces.HealthResult, error) {
	return nil, nil
}

func (d *planningDriver) Scale(_ context.Context, _ interfaces.ResourceRef, _ int) (*interfaces.ResourceOutput, error) {
	return nil, nil
}

func (d *planningDriver) SensitiveKeys() []string { return nil }

// seedFixture returns a 3-resource store + label-bearing state covering
// the filter dimensions: type (infra.vpc vs infra.database), provider
// module (do-prod vs do-staging), and app_context (web vs api).
func seedFixture() *fakeStateStore {
	now := time.Date(2026, 5, 27, 12, 0, 0, 0, time.UTC)
	return &fakeStateStore{
		resources: []interfaces.ResourceState{
			{
				ID:          "vpc-prod-web",
				Name:        "vpc-prod-web",
				Type:        "infra.vpc",
				Provider:    "digitalocean",
				ProviderRef: "do-prod",
				ProviderID:  "vpc-001",
				AppliedConfig: map[string]any{
					"region": "nyc3",
					"labels": map[string]any{"app_context": "web"},
				},
				UpdatedAt: now,
			},
			{
				ID:          "db-prod-web",
				Name:        "db-prod-web",
				Type:        "infra.database",
				Provider:    "digitalocean",
				ProviderRef: "do-prod",
				ProviderID:  "db-001",
				AppliedConfig: map[string]any{
					"engine": "postgres",
					"labels": map[string]any{"app_context": "web"},
				},
				UpdatedAt: now.Add(time.Hour),
			},
			{
				ID:          "vpc-staging-api",
				Name:        "vpc-staging-api",
				Type:        "infra.vpc",
				Provider:    "digitalocean",
				ProviderRef: "do-staging",
				ProviderID:  "vpc-002",
				AppliedConfig: map[string]any{
					"region": "ams3",
					"labels": map[string]any{"app_context": "api"},
				},
				UpdatedAt: now.Add(2 * time.Hour),
			},
		},
	}
}

func TestListResources_HappyPath(t *testing.T) {
	store := seedFixture()
	in := &adminpb.AdminListResourcesInput{Evidence: authzOK()}
	out, err := handler.ListResources(context.Background(), store, nil, catalog.New(), in)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if out == nil {
		t.Fatal("nil output with nil error")
	}
	if out.Error != "" {
		t.Errorf("unexpected error field: %q", out.Error)
	}
	if len(out.Resources) != 3 {
		t.Fatalf("got %d resources, want 3 (no filters applied)", len(out.Resources))
	}
}

func TestListResources_DefaultDenyOnMissingEvidence(t *testing.T) {
	store := seedFixture()
	in := &adminpb.AdminListResourcesInput{} // no Evidence
	out, err := handler.ListResources(context.Background(), store, nil, catalog.New(), in)
	if err != nil {
		t.Fatalf("ListResources should NOT error on auth refusal — it returns Output.error: %v", err)
	}
	if out == nil {
		t.Fatal("nil output with nil error on auth refusal")
	}
	if out.Error == "" {
		t.Error("expected non-empty Error on missing evidence (default-deny)")
	}
	if len(out.Resources) != 0 {
		t.Errorf("expected empty Resources on auth refusal, got %d", len(out.Resources))
	}
}

func TestListResources_DefaultDenyOnAuthzNotChecked(t *testing.T) {
	store := seedFixture()
	in := &adminpb.AdminListResourcesInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: false, AuthzAllowed: true},
	}
	out, _ := handler.ListResources(context.Background(), store, nil, catalog.New(), in)
	if out.Error == "" {
		t.Error("expected non-empty Error when authz_checked=false (default-deny)")
	}
}

func TestListResources_DefaultDenyOnAuthzDenied(t *testing.T) {
	store := seedFixture()
	in := &adminpb.AdminListResourcesInput{
		Evidence: &adminpb.AdminAuthzEvidence{AuthzChecked: true, AuthzAllowed: false},
	}
	out, _ := handler.ListResources(context.Background(), store, nil, catalog.New(), in)
	if out.Error == "" {
		t.Error("expected non-empty Error when authz_allowed=false")
	}
}

func TestListResources_TypeFilter(t *testing.T) {
	store := seedFixture()
	in := &adminpb.AdminListResourcesInput{TypeFilter: "infra.vpc", Evidence: authzOK()}
	out, _ := handler.ListResources(context.Background(), store, nil, catalog.New(), in)
	if len(out.Resources) != 2 {
		t.Fatalf("got %d resources, want 2 (vpc-prod-web + vpc-staging-api)", len(out.Resources))
	}
	for _, r := range out.Resources {
		if r.Type != "infra.vpc" {
			t.Errorf("type_filter leak: got %s", r.Type)
		}
	}
}

func TestListResources_ProviderFilterByModuleName(t *testing.T) {
	store := seedFixture()
	in := &adminpb.AdminListResourcesInput{ProviderFilter: "do-prod", Evidence: authzOK()}
	out, _ := handler.ListResources(context.Background(), store, nil, catalog.New(), in)
	if len(out.Resources) != 2 {
		t.Fatalf("got %d resources, want 2 (vpc-prod-web + db-prod-web)", len(out.Resources))
	}
	for _, r := range out.Resources {
		if r.ProviderModule != "do-prod" {
			t.Errorf("provider_filter leak: got module %q", r.ProviderModule)
		}
	}
}

func TestListResources_AppContextFilter(t *testing.T) {
	store := seedFixture()
	in := &adminpb.AdminListResourcesInput{AppContextFilter: "api", Evidence: authzOK()}
	out, _ := handler.ListResources(context.Background(), store, nil, catalog.New(), in)
	if len(out.Resources) != 1 {
		t.Fatalf("got %d resources, want 1 (vpc-staging-api only)", len(out.Resources))
	}
	if out.Resources[0].Name != "vpc-staging-api" {
		t.Errorf("got %s, want vpc-staging-api", out.Resources[0].Name)
	}
	if out.Resources[0].AppContext != "api" {
		t.Errorf("app_context not populated: got %q", out.Resources[0].AppContext)
	}
}

func TestListResources_CombinedFilters(t *testing.T) {
	store := seedFixture()
	in := &adminpb.AdminListResourcesInput{
		TypeFilter:       "infra.vpc",
		ProviderFilter:   "do-prod",
		AppContextFilter: "web",
		Evidence:         authzOK(),
	}
	out, _ := handler.ListResources(context.Background(), store, nil, catalog.New(), in)
	if len(out.Resources) != 1 {
		t.Fatalf("got %d resources, want 1 (vpc-prod-web only)", len(out.Resources))
	}
	if out.Resources[0].Name != "vpc-prod-web" {
		t.Errorf("got %s, want vpc-prod-web", out.Resources[0].Name)
	}
}

func TestListResources_PopulatesProviderTypeAndModule(t *testing.T) {
	store := seedFixture()
	in := &adminpb.AdminListResourcesInput{TypeFilter: "infra.vpc", ProviderFilter: "do-prod", Evidence: authzOK()}
	out, _ := handler.ListResources(context.Background(), store, nil, catalog.New(), in)
	if len(out.Resources) != 1 {
		t.Fatalf("got %d, want 1", len(out.Resources))
	}
	r := out.Resources[0]
	if r.ProviderType != "digitalocean" {
		t.Errorf("provider_type = %q, want digitalocean (from state.Provider)", r.ProviderType)
	}
	if r.ProviderModule != "do-prod" {
		t.Errorf("provider_module = %q, want do-prod (from state.ProviderRef)", r.ProviderModule)
	}
}

func TestListResources_EmptyAppContextSurvivesFilter(t *testing.T) {
	// Edge case: when AppContextFilter is empty, resources with empty
	// app_context label must still pass through.
	now := time.Now().UTC()
	store := &fakeStateStore{
		resources: []interfaces.ResourceState{{
			Name: "no-context", Type: "infra.vpc",
			Provider: "digitalocean", ProviderRef: "do-prod",
			AppliedConfig: map[string]any{}, // no labels
			UpdatedAt:     now,
		}},
	}
	in := &adminpb.AdminListResourcesInput{Evidence: authzOK()}
	out, _ := handler.ListResources(context.Background(), store, nil, catalog.New(), in)
	if len(out.Resources) != 1 {
		t.Errorf("empty AppContextFilter should pass through unlabeled resources; got %d", len(out.Resources))
	}
}

// containsError fails the test when out.Error does not contain the
// expected substring. Used by default-deny tests to verify the
// operator-facing message is actionable.
func containsError(t *testing.T, out *adminpb.AdminListResourcesOutput, want string) {
	t.Helper()
	if !strings.Contains(out.Error, want) {
		t.Errorf("Error = %q, want substring %q", out.Error, want)
	}
}

func TestListResources_DenyMessageMentionsAuthz(t *testing.T) {
	store := seedFixture()
	in := &adminpb.AdminListResourcesInput{} // no Evidence
	out, _ := handler.ListResources(context.Background(), store, nil, catalog.New(), in)
	containsError(t, out, "authz")
}
