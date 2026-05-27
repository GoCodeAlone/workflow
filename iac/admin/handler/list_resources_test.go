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
