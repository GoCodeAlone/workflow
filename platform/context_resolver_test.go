package platform

import (
	"context"
	"fmt"
	"testing"
	"time"
)

// mockStateStore is a minimal in-memory StateStore for testing the context resolver.
type mockStateStore struct {
	resources map[string]map[string]*ResourceOutput // contextPath -> name -> output
	plans     map[string]*Plan
	deps      []DependencyRef
	// If set, any operation returns this error.
	forceErr error
}

func newMockStateStore() *mockStateStore {
	return &mockStateStore{
		resources: make(map[string]map[string]*ResourceOutput),
		plans:     make(map[string]*Plan),
	}
}

func (m *mockStateStore) SaveResource(_ context.Context, contextPath string, output *ResourceOutput) error {
	if m.forceErr != nil {
		return m.forceErr
	}
	if m.resources[contextPath] == nil {
		m.resources[contextPath] = make(map[string]*ResourceOutput)
	}
	m.resources[contextPath][output.Name] = output
	return nil
}

func (m *mockStateStore) GetResource(_ context.Context, contextPath, resourceName string) (*ResourceOutput, error) {
	if m.forceErr != nil {
		return nil, m.forceErr
	}
	bucket, ok := m.resources[contextPath]
	if !ok {
		return nil, &ResourceNotFoundError{Name: resourceName}
	}
	res, ok := bucket[resourceName]
	if !ok {
		return nil, &ResourceNotFoundError{Name: resourceName}
	}
	return res, nil
}

func (m *mockStateStore) ListResources(_ context.Context, contextPath string) ([]*ResourceOutput, error) {
	if m.forceErr != nil {
		return nil, m.forceErr
	}
	bucket, ok := m.resources[contextPath]
	if !ok {
		return nil, nil
	}
	var result []*ResourceOutput
	for _, r := range bucket {
		result = append(result, r)
	}
	return result, nil
}

func (m *mockStateStore) DeleteResource(_ context.Context, contextPath, resourceName string) error {
	if m.forceErr != nil {
		return m.forceErr
	}
	if bucket, ok := m.resources[contextPath]; ok {
		delete(bucket, resourceName)
	}
	return nil
}

func (m *mockStateStore) SavePlan(_ context.Context, plan *Plan) error {
	if m.forceErr != nil {
		return m.forceErr
	}
	m.plans[plan.ID] = plan
	return nil
}

func (m *mockStateStore) GetPlan(_ context.Context, planID string) (*Plan, error) {
	if m.forceErr != nil {
		return nil, m.forceErr
	}
	p, ok := m.plans[planID]
	if !ok {
		return nil, &ResourceNotFoundError{Name: planID}
	}
	return p, nil
}

func (m *mockStateStore) ListPlans(_ context.Context, _ string, _ int) ([]*Plan, error) {
	return nil, nil
}

func (m *mockStateStore) Lock(_ context.Context, _ string, _ time.Duration) (LockHandle, error) {
	return &mockLock{}, nil
}

func (m *mockStateStore) Dependencies(_ context.Context, _, _ string) ([]DependencyRef, error) {
	return m.deps, nil
}

func (m *mockStateStore) AddDependency(_ context.Context, dep DependencyRef) error {
	m.deps = append(m.deps, dep)
	return nil
}

type mockLock struct{}

func (l *mockLock) Unlock(_ context.Context) error                   { return nil }
func (l *mockLock) Refresh(_ context.Context, _ time.Duration) error { return nil }

// --- Tests ---

func TestStdContextResolver_ResolveTier1(t *testing.T) {
	store := newMockStateStore()
	resolver := NewStdContextResolver(store)
	ctx := context.Background()

	pctx, err := resolver.ResolveContext(ctx, "acme", "production", "", TierInfrastructure)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if pctx.Org != "acme" {
		t.Errorf("expected org 'acme', got %q", pctx.Org)
	}
	if pctx.Environment != "production" {
		t.Errorf("expected env 'production', got %q", pctx.Environment)
	}
	if pctx.Tier != TierInfrastructure {
		t.Errorf("expected tier 1, got %d", pctx.Tier)
	}
	if len(pctx.ParentOutputs) != 0 {
		t.Errorf("tier 1 should have no parent outputs, got %d", len(pctx.ParentOutputs))
	}
	if len(pctx.Constraints) != 0 {
		t.Errorf("tier 1 should have no constraints, got %d", len(pctx.Constraints))
	}
}

func TestStdContextResolver_ResolveTier2SeesT1Outputs(t *testing.T) {
	store := newMockStateStore()
	resolver := NewStdContextResolver(store)
	ctx := context.Background()

	// Simulate Tier 1 outputs.
	tier1Path := "acme/production/tier1"
	store.resources[tier1Path] = map[string]*ResourceOutput{
		"primary-cluster": {
			Name:       "primary-cluster",
			Type:       "kubernetes_cluster",
			Endpoint:   "https://eks.us-east-1.amazonaws.com",
			Properties: map[string]any{"version": "1.29"},
			Status:     ResourceStatusActive,
		},
	}

	pctx, err := resolver.ResolveContext(ctx, "acme", "production", "", TierSharedPrimitive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(pctx.ParentOutputs) != 1 {
		t.Fatalf("expected 1 parent output, got %d", len(pctx.ParentOutputs))
	}
	cluster, ok := pctx.ParentOutputs["primary-cluster"]
	if !ok {
		t.Fatal("expected 'primary-cluster' in parent outputs")
	}
	if cluster.Endpoint != "https://eks.us-east-1.amazonaws.com" {
		t.Errorf("unexpected endpoint: %s", cluster.Endpoint)
	}
}

func TestStdContextResolver_ResolveTier3SeesT1AndT2(t *testing.T) {
	store := newMockStateStore()
	resolver := NewStdContextResolver(store)
	ctx := context.Background()

	// Tier 1 outputs.
	store.resources["acme/production/tier1"] = map[string]*ResourceOutput{
		"primary-cluster": {
			Name:     "primary-cluster",
			Type:     "kubernetes_cluster",
			Endpoint: "https://eks.us-east-1.amazonaws.com",
			Status:   ResourceStatusActive,
		},
	}

	// Tier 1 constraints.
	store.resources["acme/production/tier1"]["__constraints__"] = &ResourceOutput{
		Name: "__constraints__",
		Type: "constraints",
		Properties: map[string]any{
			"constraint_0": map[string]any{
				"field":    "memory",
				"operator": "<=",
				"value":    "4Gi",
				"source":   "tier1",
			},
		},
		Status: ResourceStatusActive,
	}

	// Tier 2 outputs.
	store.resources["acme/production/tier2"] = map[string]*ResourceOutput{
		"shared-postgres": {
			Name:          "shared-postgres",
			Type:          "database",
			ConnectionStr: "postgresql://db.internal:5432/app",
			Status:        ResourceStatusActive,
		},
	}

	// Tier 2 constraints.
	store.resources["acme/production/tier2"]["__constraints__"] = &ResourceOutput{
		Name: "__constraints__",
		Type: "constraints",
		Properties: map[string]any{
			"constraint_0": map[string]any{
				"field":    "replicas",
				"operator": "<=",
				"value":    10,
				"source":   "tier2",
			},
		},
		Status: ResourceStatusActive,
	}

	pctx, err := resolver.ResolveContext(ctx, "acme", "production", "api-service", TierApplication)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Should see both T1 and T2 outputs (minus __constraints__ entries, but they're
	// also in the map — that's fine, the constraint extractor handles them).
	if _, ok := pctx.ParentOutputs["primary-cluster"]; !ok {
		t.Error("expected 'primary-cluster' from tier 1 in parent outputs")
	}
	if _, ok := pctx.ParentOutputs["shared-postgres"]; !ok {
		t.Error("expected 'shared-postgres' from tier 2 in parent outputs")
	}

	// Should see constraints from both tiers.
	if len(pctx.Constraints) != 2 {
		t.Fatalf("expected 2 constraints (one from T1, one from T2), got %d", len(pctx.Constraints))
	}

	foundMemory := false
	foundReplicas := false
	for _, c := range pctx.Constraints {
		if c.Field == "memory" {
			foundMemory = true
		}
		if c.Field == "replicas" {
			foundReplicas = true
		}
	}
	if !foundMemory {
		t.Error("expected memory constraint from tier 1")
	}
	if !foundReplicas {
		t.Error("expected replicas constraint from tier 2")
	}
}

func TestStdContextResolver_PropagateOutputs(t *testing.T) {
	store := newMockStateStore()
	resolver := NewStdContextResolver(store)
	ctx := context.Background()

	pctx := &PlatformContext{
		Org:         "acme",
		Environment: "production",
		Application: "",
		Tier:        TierInfrastructure,
	}

	outputs := []*ResourceOutput{
		{
			Name:     "primary-cluster",
			Type:     "kubernetes_cluster",
			Endpoint: "https://eks.example.com",
			Status:   ResourceStatusActive,
		},
		{
			Name:   "primary-vpc",
			Type:   "network",
			Status: ResourceStatusActive,
			Properties: map[string]any{
				"vpc_id": "vpc-123",
			},
		},
	}

	err := resolver.PropagateOutputs(ctx, pctx, outputs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify persisted in state store.
	contextPath := pctx.ContextPath()
	resources, err := store.ListResources(ctx, contextPath)
	if err != nil {
		t.Fatalf("unexpected error listing resources: %v", err)
	}
	if len(resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(resources))
	}
}

func TestStdContextResolver_RegisterConstraints(t *testing.T) {
	store := newMockStateStore()
	resolver := NewStdContextResolver(store)
	ctx := context.Background()

	pctx := &PlatformContext{
		Org:         "acme",
		Environment: "production",
		Tier:        TierInfrastructure,
	}

	constraints := []Constraint{
		{Field: "memory", Operator: "<=", Value: "4Gi", Source: "tier1"},
		{Field: "cpu", Operator: "<=", Value: "2000m", Source: "tier1"},
	}

	err := resolver.RegisterConstraints(ctx, pctx, constraints)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify the constraints resource was saved.
	contextPath := pctx.ContextPath()
	res, err := store.GetResource(ctx, contextPath, "__constraints__")
	if err != nil {
		t.Fatalf("unexpected error getting constraints: %v", err)
	}
	if res == nil {
		t.Fatal("expected constraints resource")
	}
	if len(res.Properties) != 2 {
		t.Errorf("expected 2 constraint properties, got %d", len(res.Properties))
	}
}

func TestStdContextResolver_RegisterConstraintsEmpty(t *testing.T) {
	store := newMockStateStore()
	resolver := NewStdContextResolver(store)
	ctx := context.Background()

	pctx := &PlatformContext{
		Org:         "acme",
		Environment: "production",
		Tier:        TierInfrastructure,
	}

	err := resolver.RegisterConstraints(ctx, pctx, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// No resource should be stored.
	_, err = store.GetResource(ctx, pctx.ContextPath(), "__constraints__")
	if err == nil {
		t.Error("expected not-found error for empty constraints")
	}
}

func TestStdContextResolver_ValidateTierBoundary(t *testing.T) {
	store := newMockStateStore()
	resolver := NewStdContextResolver(store)

	pctx := &PlatformContext{
		Org:         "acme",
		Environment: "production",
		Application: "api-service",
		Tier:        TierApplication,
		Constraints: []Constraint{
			{Field: "memory", Operator: "<=", Value: "4Gi", Source: "tier1"},
			{Field: "replicas", Operator: "<=", Value: 10, Source: "tier2"},
		},
	}

	declarations := []CapabilityDeclaration{
		{
			Name: "api-service",
			Type: "container_runtime",
			Tier: TierApplication,
			Properties: map[string]any{
				"memory":   "512Mi",
				"replicas": 3,
			},
		},
	}

	violations := resolver.ValidateTierBoundary(pctx, declarations)
	if len(violations) != 0 {
		t.Errorf("expected no violations for valid declarations, got %d: %v", len(violations), violations)
	}
}

func TestStdContextResolver_ValidateTierBoundaryViolation(t *testing.T) {
	store := newMockStateStore()
	resolver := NewStdContextResolver(store)

	pctx := &PlatformContext{
		Org:         "acme",
		Environment: "production",
		Application: "api-service",
		Tier:        TierApplication,
		Constraints: []Constraint{
			{Field: "memory", Operator: "<=", Value: "4Gi", Source: "tier1"},
			{Field: "replicas", Operator: "<=", Value: 10, Source: "tier2"},
		},
	}

	declarations := []CapabilityDeclaration{
		{
			Name: "api-service",
			Type: "container_runtime",
			Tier: TierApplication,
			Properties: map[string]any{
				"memory":   "8Gi",
				"replicas": 15,
			},
		},
	}

	violations := resolver.ValidateTierBoundary(pctx, declarations)
	if len(violations) != 2 {
		t.Fatalf("expected 2 violations, got %d", len(violations))
	}
}

func TestStdContextResolver_ValidateTierBoundaryWrongTier(t *testing.T) {
	store := newMockStateStore()
	resolver := NewStdContextResolver(store)

	pctx := &PlatformContext{
		Org:         "acme",
		Environment: "production",
		Tier:        TierApplication,
	}

	declarations := []CapabilityDeclaration{
		{
			Name: "infra-resource",
			Type: "kubernetes_cluster",
			Tier: TierInfrastructure,
		},
	}

	violations := resolver.ValidateTierBoundary(pctx, declarations)
	if len(violations) != 1 {
		t.Fatalf("expected 1 tier boundary violation, got %d", len(violations))
	}
	if violations[0].Constraint.Field != "tier" {
		t.Errorf("expected 'tier' field in violation, got %q", violations[0].Constraint.Field)
	}
}

func TestStdContextResolver_InvalidTier(t *testing.T) {
	store := newMockStateStore()
	resolver := NewStdContextResolver(store)
	ctx := context.Background()

	_, err := resolver.ResolveContext(ctx, "acme", "prod", "", Tier(99))
	if err == nil {
		t.Error("expected error for invalid tier")
	}
}

func TestStdContextResolver_StateStoreError(t *testing.T) {
	store := newMockStateStore()
	store.forceErr = fmt.Errorf("connection refused")
	resolver := NewStdContextResolver(store)
	ctx := context.Background()

	_, err := resolver.ResolveContext(ctx, "acme", "prod", "", TierSharedPrimitive)
	if err == nil {
		t.Error("expected error when state store fails")
	}
}

func TestStdContextResolver_PropagateOutputsError(t *testing.T) {
	store := newMockStateStore()
	store.forceErr = fmt.Errorf("write failed")
	resolver := NewStdContextResolver(store)
	ctx := context.Background()

	pctx := &PlatformContext{Org: "acme", Environment: "prod", Tier: TierInfrastructure}
	err := resolver.PropagateOutputs(ctx, pctx, []*ResourceOutput{
		{Name: "res", Type: "test", Status: ResourceStatusActive},
	})
	if err == nil {
		t.Error("expected error when state store write fails")
	}
}

func TestContextPath(t *testing.T) {
	pctx := &PlatformContext{
		Org:         "acme",
		Environment: "production",
		Application: "api-service",
	}
	if got := pctx.ContextPath(); got != "acme/production/api-service" {
		t.Errorf("expected 'acme/production/api-service', got %q", got)
	}

	pctx.Application = ""
	if got := pctx.ContextPath(); got != "acme/production" {
		t.Errorf("expected 'acme/production', got %q", got)
	}
}

func TestContextPathForTier(t *testing.T) {
	tests := []struct {
		org, env, app string
		tier          Tier
		want          string
	}{
		{"acme", "prod", "", TierInfrastructure, "acme/prod/tier1"},
		{"acme", "prod", "", TierSharedPrimitive, "acme/prod/tier2"},
		{"acme", "prod", "api", TierApplication, "acme/prod/api/tier3"},
		{"acme", "prod", "", TierApplication, "acme/prod/tier3"},
	}

	for _, tt := range tests {
		got := contextPathForTier(tt.org, tt.env, tt.app, tt.tier)
		if got != tt.want {
			t.Errorf("contextPathForTier(%q, %q, %q, %d) = %q, want %q",
				tt.org, tt.env, tt.app, tt.tier, got, tt.want)
		}
	}
}

func TestStdContextResolver_ResolveContextNoConstraintsResource(t *testing.T) {
	store := newMockStateStore()
	resolver := NewStdContextResolver(store)
	ctx := context.Background()

	// Tier 1 has resources but no __constraints__ resource.
	store.resources["acme/prod/tier1"] = map[string]*ResourceOutput{
		"cluster": {Name: "cluster", Type: "k8s", Status: ResourceStatusActive},
	}

	pctx, err := resolver.ResolveContext(ctx, "acme", "prod", "", TierSharedPrimitive)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(pctx.ParentOutputs) != 1 {
		t.Errorf("expected 1 parent output, got %d", len(pctx.ParentOutputs))
	}
	if len(pctx.Constraints) != 0 {
		t.Errorf("expected 0 constraints when none stored, got %d", len(pctx.Constraints))
	}
}

func TestIsNotFound(t *testing.T) {
	if isNotFound(nil) {
		t.Error("nil should not be not-found")
	}
	if isNotFound(fmt.Errorf("some error")) {
		t.Error("generic error should not be not-found")
	}
	if !isNotFound(&ResourceNotFoundError{Name: "test"}) {
		t.Error("ResourceNotFoundError should be not-found")
	}
}
