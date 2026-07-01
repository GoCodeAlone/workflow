package module_test

import (
	"context"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	"github.com/GoCodeAlone/workflow/module"
)

// stubIaCProvider is a minimal interfaces.IaCProvider for step tests.
type stubIaCProvider struct {
	statusResult  []interfaces.ResourceStatus
	statusErr     error
	statusRefs    []interfaces.ResourceRef
	caps          []interfaces.IaCCapabilityDeclaration
	planResult    *interfaces.IaCPlan
	planErr       error
	planDesired   []interfaces.ResourceSpec
	destroyResult *interfaces.DestroyResult
	destroyErr    error
	destroyRefs   []interfaces.ResourceRef
	driftResult   []interfaces.DriftResult
	driftErr      error
}

func (s *stubIaCProvider) Name() string                                         { return "stub" }
func (s *stubIaCProvider) Version() string                                      { return "0.0.0" }
func (s *stubIaCProvider) Initialize(_ context.Context, _ map[string]any) error { return nil }
func (s *stubIaCProvider) Capabilities() []interfaces.IaCCapabilityDeclaration  { return s.caps }
func (s *stubIaCProvider) Plan(_ context.Context, desired []interfaces.ResourceSpec, _ []interfaces.ResourceState) (*interfaces.IaCPlan, error) {
	s.planDesired = append([]interfaces.ResourceSpec(nil), desired...)
	return s.planResult, s.planErr
}
func (s *stubIaCProvider) Destroy(_ context.Context, refs []interfaces.ResourceRef) (*interfaces.DestroyResult, error) {
	s.destroyRefs = append([]interfaces.ResourceRef(nil), refs...)
	return s.destroyResult, s.destroyErr
}
func (s *stubIaCProvider) Status(_ context.Context, refs []interfaces.ResourceRef) ([]interfaces.ResourceStatus, error) {
	s.statusRefs = append([]interfaces.ResourceRef(nil), refs...)
	return s.statusResult, s.statusErr
}
func (s *stubIaCProvider) DetectDrift(_ context.Context, _ []interfaces.ResourceRef) ([]interfaces.DriftResult, error) {
	return s.driftResult, s.driftErr
}
func (s *stubIaCProvider) Import(_ context.Context, _ string, _ string) (*interfaces.ResourceState, error) {
	return nil, nil
}
func (s *stubIaCProvider) ResolveSizing(_ string, _ interfaces.Size, _ *interfaces.ResourceHints) (*interfaces.ProviderSizing, error) {
	return nil, nil
}
func (s *stubIaCProvider) ResourceDriver(_ string) (interfaces.ResourceDriver, error) {
	return nil, nil
}
func (s *stubIaCProvider) SupportedCanonicalKeys() []string { return nil }
func (s *stubIaCProvider) BootstrapStateBackend(_ context.Context, _ map[string]any) (*interfaces.BootstrapResult, error) {
	return nil, nil
}
func (s *stubIaCProvider) Close() error { return nil }

// compile-time check
var _ interfaces.IaCProvider = (*stubIaCProvider)(nil)

// ─── step.iac_provider_list tests ────────────────────────────────────────────

func TestIaCProviderListStep_Execute_ReturnsSummaries(t *testing.T) {
	app := module.NewMockApplication()
	provider := &stubIaCProvider{
		statusResult: []interfaces.ResourceStatus{
			{Name: "db", Type: "infra.database", ProviderID: "pid-1", Status: "running", Outputs: map[string]any{"host": "localhost"}},
			{Name: "vpc", Type: "infra.vpc", ProviderID: "pid-2", Status: "running"},
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderListStepFactory()
	step, err := factory("list-step", map[string]any{"provider": "my-provider"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}

	resources, ok := result.Output["resources"].([]map[string]any)
	if !ok {
		t.Fatalf("expected resources slice, got %T", result.Output["resources"])
	}
	if len(resources) != 2 {
		t.Errorf("expected 2 resources, got %d", len(resources))
	}
	if resources[0]["name"] != "db" {
		t.Errorf("expected first resource name 'db', got %v", resources[0]["name"])
	}
	if result.Output["count"] != 2 {
		t.Errorf("expected count=2, got %v", result.Output["count"])
	}
}

func TestIaCProviderListStep_ResourcesResolveInfraModuleRefs(t *testing.T) {
	app := module.NewMockApplication()
	provider := &stubIaCProvider{
		statusResult: []interfaces.ResourceStatus{
			{Name: "staging-ecs", Type: "infra.container_service", ProviderID: "pid-ecs", Status: "running"},
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}
	infra := module.NewInfraModule("staging-ecs", "infra.container_service", map[string]any{"provider": "my-provider"})
	if err := app.RegisterService("staging-ecs.driver", infra); err != nil {
		t.Fatal(err)
	}

	factory := module.NewIaCProviderListStepFactory()
	step, err := factory("list-step", map[string]any{
		"provider":  "my-provider",
		"resources": []any{"staging-ecs"},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	if _, err := step.Execute(context.Background(), &module.PipelineContext{}); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if len(provider.statusRefs) != 1 {
		t.Fatalf("expected one status ref, got %d", len(provider.statusRefs))
	}
	if provider.statusRefs[0].Name != "staging-ecs" || provider.statusRefs[0].Type != "infra.container_service" {
		t.Fatalf("unexpected status refs: %#v", provider.statusRefs)
	}
}

func TestIaCProviderListStep_Execute_UnregisteredProvider(t *testing.T) {
	app := module.NewMockApplication()
	factory := module.NewIaCProviderListStepFactory()
	step, err := factory("list-step", map[string]any{"provider": "nonexistent"}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	_, err = step.Execute(context.Background(), &module.PipelineContext{})
	if err == nil {
		t.Fatal("expected error for unregistered provider, got nil")
	}
	if want := "not registered"; !containsString(err.Error(), want) {
		t.Errorf("expected error containing %q, got %q", want, err.Error())
	}
}

func TestIaCProviderListStep_Factory_RequiresProvider(t *testing.T) {
	factory := module.NewIaCProviderListStepFactory()
	_, err := factory("list-step", map[string]any{}, nil)
	if err == nil {
		t.Fatal("expected error when 'provider' missing, got nil")
	}
}

func TestIaCProviderListStep_Factory_MalformedRefs_WrongTopType(t *testing.T) {
	// refs present but wrong top-level type (string instead of []any) — must error
	// at factory time, not silently fall through to unfiltered list-all.
	factory := module.NewIaCProviderListStepFactory()
	_, err := factory("list-step", map[string]any{
		"provider": "my-provider",
		"refs":     "not-a-list",
	}, nil)
	if err == nil {
		t.Fatal("expected factory error for non-list 'refs', got nil")
	}
	if want := "refs' must be a list"; !containsString(err.Error(), want) {
		t.Errorf("expected error containing %q, got: %v", want, err)
	}
}

func TestIaCProviderListStep_Factory_MalformedRefs_WrongItemType(t *testing.T) {
	// refs is a list but contains a non-map item — must error at factory time.
	factory := module.NewIaCProviderListStepFactory()
	_, err := factory("list-step", map[string]any{
		"provider": "my-provider",
		"refs":     []any{"not-a-map"},
	}, nil)
	if err == nil {
		t.Fatal("expected factory error for non-map refs item, got nil")
	}
	if want := "refs[0] must be a map"; !containsString(err.Error(), want) {
		t.Errorf("expected error containing %q, got: %v", want, err)
	}
}

func TestIaCProviderListStep_Factory_AbsentRefs_ListsAll(t *testing.T) {
	// Absent refs key is fine — the step queries all resources.
	app := module.NewMockApplication()
	provider := &stubIaCProvider{
		statusResult: []interfaces.ResourceStatus{
			{Name: "db", Type: "infra.database", ProviderID: "pid-1", Status: "running"},
		},
	}
	if err := app.RegisterService("my-provider", provider); err != nil {
		t.Fatal(err)
	}
	factory := module.NewIaCProviderListStepFactory()
	step, err := factory("list-step", map[string]any{"provider": "my-provider"}, app)
	if err != nil {
		t.Fatalf("factory error for absent refs: %v", err)
	}
	result, err := step.Execute(context.Background(), &module.PipelineContext{})
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if result.Output["count"] != 1 {
		t.Errorf("expected count=1 for list-all, got %v", result.Output["count"])
	}
}

// containsString is a test helper used across iac_provider step tests.
func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
