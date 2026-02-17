package state

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
)

func newTestSQLiteStore(t *testing.T) *SQLiteStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	store, err := NewSQLiteStore(dbPath)
	if err != nil {
		t.Fatalf("NewSQLiteStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}

func TestSQLiteStore_SaveAndGetResource(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	res := &platform.ResourceOutput{
		Name:          "my-db",
		Type:          "database",
		ProviderType:  "aws.rds",
		Endpoint:      "my-db.cluster.amazonaws.com:5432",
		ConnectionStr: "postgres://user:pass@my-db.cluster.amazonaws.com:5432/mydb",
		CredentialRef: "vault:aws/rds/my-db",
		Properties:    map[string]any{"engine": "postgresql", "version": "15"},
		Status:        platform.ResourceStatusActive,
		LastSynced:    now,
	}

	// Save
	if err := store.SaveResource(ctx, "acme/prod", res); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}

	// Get
	got, err := store.GetResource(ctx, "acme/prod", "my-db")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}

	if got.Name != res.Name {
		t.Errorf("Name = %q, want %q", got.Name, res.Name)
	}
	if got.Type != res.Type {
		t.Errorf("Type = %q, want %q", got.Type, res.Type)
	}
	if got.ProviderType != res.ProviderType {
		t.Errorf("ProviderType = %q, want %q", got.ProviderType, res.ProviderType)
	}
	if got.Endpoint != res.Endpoint {
		t.Errorf("Endpoint = %q, want %q", got.Endpoint, res.Endpoint)
	}
	if got.ConnectionStr != res.ConnectionStr {
		t.Errorf("ConnectionStr = %q, want %q", got.ConnectionStr, res.ConnectionStr)
	}
	if got.CredentialRef != res.CredentialRef {
		t.Errorf("CredentialRef = %q, want %q", got.CredentialRef, res.CredentialRef)
	}
	if got.Status != res.Status {
		t.Errorf("Status = %q, want %q", got.Status, res.Status)
	}
	if got.Properties["engine"] != "postgresql" {
		t.Errorf("Properties[engine] = %v, want postgresql", got.Properties["engine"])
	}
}

func TestSQLiteStore_UpdateResource(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	res := &platform.ResourceOutput{
		Name:       "svc",
		Type:       "container_runtime",
		Status:     platform.ResourceStatusCreating,
		Properties: map[string]any{"replicas": float64(1)},
		LastSynced: time.Now().UTC().Truncate(time.Second),
	}

	if err := store.SaveResource(ctx, "acme/prod", res); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}

	// Update status and properties
	res.Status = platform.ResourceStatusActive
	res.Properties["replicas"] = float64(3)

	if err := store.SaveResource(ctx, "acme/prod", res); err != nil {
		t.Fatalf("SaveResource (update): %v", err)
	}

	got, err := store.GetResource(ctx, "acme/prod", "svc")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}

	if got.Status != platform.ResourceStatusActive {
		t.Errorf("Status = %q, want %q", got.Status, platform.ResourceStatusActive)
	}
	if got.Properties["replicas"] != float64(3) {
		t.Errorf("Properties[replicas] = %v, want 3", got.Properties["replicas"])
	}
}

func TestSQLiteStore_ListResources(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	for _, name := range []string{"alpha", "beta", "gamma"} {
		res := &platform.ResourceOutput{
			Name:       name,
			Type:       "container_runtime",
			Status:     platform.ResourceStatusActive,
			Properties: map[string]any{},
			LastSynced: now,
		}
		if err := store.SaveResource(ctx, "acme/prod", res); err != nil {
			t.Fatalf("SaveResource(%s): %v", name, err)
		}
	}

	// Save one in a different context to verify filtering
	if err := store.SaveResource(ctx, "acme/staging", &platform.ResourceOutput{
		Name:       "delta",
		Type:       "database",
		Status:     platform.ResourceStatusActive,
		Properties: map[string]any{},
		LastSynced: now,
	}); err != nil {
		t.Fatalf("SaveResource(delta): %v", err)
	}

	resources, err := store.ListResources(ctx, "acme/prod")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 3 {
		t.Fatalf("ListResources returned %d resources, want 3", len(resources))
	}

	// Verify alphabetical ordering
	if resources[0].Name != "alpha" {
		t.Errorf("resources[0].Name = %q, want alpha", resources[0].Name)
	}
	if resources[2].Name != "gamma" {
		t.Errorf("resources[2].Name = %q, want gamma", resources[2].Name)
	}
}

func TestSQLiteStore_DeleteResource(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	res := &platform.ResourceOutput{
		Name:       "to-delete",
		Type:       "container_runtime",
		Status:     platform.ResourceStatusActive,
		Properties: map[string]any{},
		LastSynced: time.Now().UTC(),
	}

	if err := store.SaveResource(ctx, "acme/prod", res); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}

	if err := store.DeleteResource(ctx, "acme/prod", "to-delete"); err != nil {
		t.Fatalf("DeleteResource: %v", err)
	}

	_, err := store.GetResource(ctx, "acme/prod", "to-delete")
	if err == nil {
		t.Fatal("expected error after delete, got nil")
	}
}

func TestSQLiteStore_DeleteResource_NotFound(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	err := store.DeleteResource(ctx, "acme/prod", "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent resource, got nil")
	}
	if _, ok := err.(*platform.ResourceNotFoundError); !ok {
		t.Errorf("expected ResourceNotFoundError, got %T: %v", err, err)
	}
}

func TestSQLiteStore_GetResource_NotFound(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	_, err := store.GetResource(ctx, "acme/prod", "nonexistent")
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if _, ok := err.(*platform.ResourceNotFoundError); !ok {
		t.Errorf("expected ResourceNotFoundError, got %T: %v", err, err)
	}
}

func TestSQLiteStore_SaveAndGetPlan(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	plan := &platform.Plan{
		ID:      "plan-001",
		Tier:    platform.TierApplication,
		Context: "acme/prod/api",
		Actions: []platform.PlanAction{
			{
				Action:       "create",
				ResourceName: "api-svc",
				ResourceType: "aws.ecs_service",
				Provider:     "aws",
				After:        map[string]any{"replicas": float64(3)},
			},
		},
		CreatedAt: now,
		Status:    "pending",
		Provider:  "aws",
	}

	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	got, err := store.GetPlan(ctx, "plan-001")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}

	if got.ID != plan.ID {
		t.Errorf("ID = %q, want %q", got.ID, plan.ID)
	}
	if got.Tier != plan.Tier {
		t.Errorf("Tier = %d, want %d", got.Tier, plan.Tier)
	}
	if got.Context != plan.Context {
		t.Errorf("Context = %q, want %q", got.Context, plan.Context)
	}
	if len(got.Actions) != 1 {
		t.Fatalf("len(Actions) = %d, want 1", len(got.Actions))
	}
	if got.Actions[0].Action != "create" {
		t.Errorf("Actions[0].Action = %q, want create", got.Actions[0].Action)
	}
	if got.ApprovedAt != nil {
		t.Errorf("ApprovedAt should be nil, got %v", got.ApprovedAt)
	}
}

func TestSQLiteStore_SavePlan_WithApproval(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	approvedAt := now.Add(5 * time.Minute)
	plan := &platform.Plan{
		ID:         "plan-approved",
		Tier:       platform.TierInfrastructure,
		Context:    "acme/prod",
		Actions:    []platform.PlanAction{},
		CreatedAt:  now,
		ApprovedAt: &approvedAt,
		ApprovedBy: "admin@acme.com",
		Status:     "approved",
		Provider:   "aws",
	}

	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	got, err := store.GetPlan(ctx, "plan-approved")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}

	if got.ApprovedAt == nil {
		t.Fatal("ApprovedAt should not be nil")
	}
	if got.ApprovedBy != "admin@acme.com" {
		t.Errorf("ApprovedBy = %q, want admin@acme.com", got.ApprovedBy)
	}
}

func TestSQLiteStore_ListPlans(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	for i := 0; i < 5; i++ {
		plan := &platform.Plan{
			ID:        fmt.Sprintf("plan-%03d", i),
			Context:   "acme/prod",
			Actions:   []platform.PlanAction{},
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
			Status:    "pending",
		}
		if err := store.SavePlan(ctx, plan); err != nil {
			t.Fatalf("SavePlan(%d): %v", i, err)
		}
	}

	plans, err := store.ListPlans(ctx, "acme/prod", 3)
	if err != nil {
		t.Fatalf("ListPlans: %v", err)
	}
	if len(plans) != 3 {
		t.Fatalf("len(plans) = %d, want 3", len(plans))
	}

	// Verify descending order
	if plans[0].ID != "plan-004" {
		t.Errorf("plans[0].ID = %q, want plan-004", plans[0].ID)
	}
}

func TestSQLiteStore_Dependencies(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	dep := platform.DependencyRef{
		SourceContext:  "acme/prod",
		SourceResource: "shared-postgres",
		TargetContext:  "acme/prod/api",
		TargetResource: "api-service",
		Type:           "hard",
	}

	if err := store.AddDependency(ctx, dep); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	// Add another dependency on the same source
	dep2 := platform.DependencyRef{
		SourceContext:  "acme/prod",
		SourceResource: "shared-postgres",
		TargetContext:  "acme/prod/worker",
		TargetResource: "worker-service",
		Type:           "soft",
	}
	if err := store.AddDependency(ctx, dep2); err != nil {
		t.Fatalf("AddDependency(2): %v", err)
	}

	deps, err := store.Dependencies(ctx, "acme/prod", "shared-postgres")
	if err != nil {
		t.Fatalf("Dependencies: %v", err)
	}
	if len(deps) != 2 {
		t.Fatalf("len(deps) = %d, want 2", len(deps))
	}

	// Verify the dependency data round-trips
	found := false
	for _, d := range deps {
		if d.TargetResource == "api-service" && d.Type == "hard" {
			found = true
		}
	}
	if !found {
		t.Error("expected to find hard dependency on api-service")
	}
}

func TestSQLiteStore_Dependencies_Upsert(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	dep := platform.DependencyRef{
		SourceContext:  "acme/prod",
		SourceResource: "shared-postgres",
		TargetContext:  "acme/prod/api",
		TargetResource: "api-service",
		Type:           "hard",
	}
	if err := store.AddDependency(ctx, dep); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	// Update type to soft
	dep.Type = "soft"
	if err := store.AddDependency(ctx, dep); err != nil {
		t.Fatalf("AddDependency (upsert): %v", err)
	}

	deps, err := store.Dependencies(ctx, "acme/prod", "shared-postgres")
	if err != nil {
		t.Fatalf("Dependencies: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("len(deps) = %d, want 1", len(deps))
	}
	if deps[0].Type != "soft" {
		t.Errorf("Type = %q, want soft", deps[0].Type)
	}
}

func TestSQLiteStore_DriftReports(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	report := &DriftReport{
		ContextPath:  "acme/prod",
		ResourceName: "api-service",
		ResourceType: "aws.ecs_service",
		Tier:         platform.TierApplication,
		DriftType:    "changed",
		Expected:     map[string]any{"replicas": float64(3)},
		Actual:       map[string]any{"replicas": float64(2)},
		Diffs: []platform.DiffEntry{
			{Path: "replicas", OldValue: float64(3), NewValue: float64(2)},
		},
		DetectedAt: now,
	}

	if err := store.SaveDriftReport(ctx, report); err != nil {
		t.Fatalf("SaveDriftReport: %v", err)
	}
	if report.ID == 0 {
		t.Error("expected report.ID to be set after save")
	}

	reports, err := store.ListDriftReports(ctx, "acme/prod", 10)
	if err != nil {
		t.Fatalf("ListDriftReports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("len(reports) = %d, want 1", len(reports))
	}

	got := reports[0]
	if got.ResourceName != "api-service" {
		t.Errorf("ResourceName = %q, want api-service", got.ResourceName)
	}
	if got.DriftType != "changed" {
		t.Errorf("DriftType = %q, want changed", got.DriftType)
	}
	if got.Expected["replicas"] != float64(3) {
		t.Errorf("Expected[replicas] = %v, want 3", got.Expected["replicas"])
	}
	if got.Actual["replicas"] != float64(2) {
		t.Errorf("Actual[replicas] = %v, want 2", got.Actual["replicas"])
	}
	if len(got.Diffs) != 1 {
		t.Fatalf("len(Diffs) = %d, want 1", len(got.Diffs))
	}
}

func TestSQLiteStore_Lock(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	// Acquire lock
	handle, err := store.Lock(ctx, "acme/prod", 5*time.Minute)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	// Release lock
	if err := handle.Unlock(ctx); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	// Should be able to re-acquire
	handle2, err := store.Lock(ctx, "acme/prod", 5*time.Minute)
	if err != nil {
		t.Fatalf("Lock (re-acquire): %v", err)
	}
	if err := handle2.Unlock(ctx); err != nil {
		t.Fatalf("Unlock (re-acquire): %v", err)
	}
}

func TestSQLiteStore_Lock_Refresh(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	handle, err := store.Lock(ctx, "acme/prod", 1*time.Minute)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}
	defer handle.Unlock(ctx)

	// Refresh to extend TTL
	if err := handle.Refresh(ctx, 10*time.Minute); err != nil {
		t.Fatalf("Refresh: %v", err)
	}
}

func TestSQLiteStore_ConcurrentAccess(t *testing.T) {
	t.Parallel()
	store := newTestSQLiteStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Second)
	var wg sync.WaitGroup

	// Concurrently save resources
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res := &platform.ResourceOutput{
				Name:       fmt.Sprintf("res-%d", idx),
				Type:       "container_runtime",
				Status:     platform.ResourceStatusActive,
				Properties: map[string]any{"index": float64(idx)},
				LastSynced: now,
			}
			if err := store.SaveResource(ctx, "acme/concurrent", res); err != nil {
				t.Errorf("SaveResource(%d): %v", idx, err)
			}
		}(i)
	}
	wg.Wait()

	// Verify all were saved
	resources, err := store.ListResources(ctx, "acme/concurrent")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 20 {
		t.Errorf("len(resources) = %d, want 20", len(resources))
	}
}

// ensure fmt is used
var _ = fmt.Sprintf
