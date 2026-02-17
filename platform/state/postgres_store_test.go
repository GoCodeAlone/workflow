//go:build postgres_platform

package state

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/GoCodeAlone/workflow/platform"
)

func newTestPostgresStore(t *testing.T) *PostgresStore {
	t.Helper()
	dsn := os.Getenv("PLATFORM_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("PLATFORM_POSTGRES_DSN not set, skipping PostgreSQL tests")
	}

	store, err := NewPostgresStore(dsn)
	if err != nil {
		t.Fatalf("NewPostgresStore: %v", err)
	}
	t.Cleanup(func() {
		// Clean up test data
		ctx := context.Background()
		store.db.ExecContext(ctx, `DELETE FROM platform_drift_reports`)
		store.db.ExecContext(ctx, `DELETE FROM platform_dependencies`)
		store.db.ExecContext(ctx, `DELETE FROM platform_plans`)
		store.db.ExecContext(ctx, `DELETE FROM platform_resources`)
		store.db.ExecContext(ctx, `DELETE FROM platform_locks`)
		store.Close()
	})
	return store
}

func TestPostgresStore_SaveAndGetResource(t *testing.T) {
	t.Parallel()
	store := newTestPostgresStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	res := &platform.ResourceOutput{
		Name:          "pg-test-db",
		Type:          "database",
		ProviderType:  "aws.rds",
		Endpoint:      "pg-test.cluster.amazonaws.com:5432",
		ConnectionStr: "postgres://user:pass@pg-test:5432/mydb",
		CredentialRef: "vault:aws/rds/pg-test",
		Properties:    map[string]any{"engine": "postgresql", "version": "15"},
		Status:        platform.ResourceStatusActive,
		LastSynced:    now,
	}

	if err := store.SaveResource(ctx, "test/prod", res); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}

	got, err := store.GetResource(ctx, "test/prod", "pg-test-db")
	if err != nil {
		t.Fatalf("GetResource: %v", err)
	}

	if got.Name != res.Name {
		t.Errorf("Name = %q, want %q", got.Name, res.Name)
	}
	if got.Status != res.Status {
		t.Errorf("Status = %q, want %q", got.Status, res.Status)
	}
}

func TestPostgresStore_ListResources(t *testing.T) {
	t.Parallel()
	store := newTestPostgresStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	for _, name := range []string{"pg-alpha", "pg-beta", "pg-gamma"} {
		res := &platform.ResourceOutput{
			Name:       name,
			Type:       "container_runtime",
			Status:     platform.ResourceStatusActive,
			Properties: map[string]any{},
			LastSynced: now,
		}
		if err := store.SaveResource(ctx, "test/pg-list", res); err != nil {
			t.Fatalf("SaveResource(%s): %v", name, err)
		}
	}

	resources, err := store.ListResources(ctx, "test/pg-list")
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	if len(resources) != 3 {
		t.Fatalf("len = %d, want 3", len(resources))
	}
}

func TestPostgresStore_DeleteResource(t *testing.T) {
	t.Parallel()
	store := newTestPostgresStore(t)
	ctx := context.Background()

	res := &platform.ResourceOutput{
		Name:       "pg-to-delete",
		Type:       "container_runtime",
		Status:     platform.ResourceStatusActive,
		Properties: map[string]any{},
		LastSynced: time.Now().UTC(),
	}

	if err := store.SaveResource(ctx, "test/pg-del", res); err != nil {
		t.Fatalf("SaveResource: %v", err)
	}

	if err := store.DeleteResource(ctx, "test/pg-del", "pg-to-delete"); err != nil {
		t.Fatalf("DeleteResource: %v", err)
	}

	_, err := store.GetResource(ctx, "test/pg-del", "pg-to-delete")
	if err == nil {
		t.Fatal("expected error after delete")
	}
}

func TestPostgresStore_SaveAndGetPlan(t *testing.T) {
	t.Parallel()
	store := newTestPostgresStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	plan := &platform.Plan{
		ID:      "pg-plan-001",
		Tier:    platform.TierApplication,
		Context: "test/pg-plan",
		Actions: []platform.PlanAction{
			{Action: "create", ResourceName: "svc", ResourceType: "aws.ecs", Provider: "aws"},
		},
		CreatedAt: now,
		Status:    "pending",
		Provider:  "aws",
	}

	if err := store.SavePlan(ctx, plan); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}

	got, err := store.GetPlan(ctx, "pg-plan-001")
	if err != nil {
		t.Fatalf("GetPlan: %v", err)
	}

	if got.ID != plan.ID {
		t.Errorf("ID = %q, want %q", got.ID, plan.ID)
	}
	if len(got.Actions) != 1 {
		t.Fatalf("len(Actions) = %d, want 1", len(got.Actions))
	}
}

func TestPostgresStore_Dependencies(t *testing.T) {
	t.Parallel()
	store := newTestPostgresStore(t)
	ctx := context.Background()

	dep := platform.DependencyRef{
		SourceContext:  "test/pg-dep",
		SourceResource: "shared-postgres",
		TargetContext:  "test/pg-dep/api",
		TargetResource: "api-service",
		Type:           "hard",
	}

	if err := store.AddDependency(ctx, dep); err != nil {
		t.Fatalf("AddDependency: %v", err)
	}

	deps, err := store.Dependencies(ctx, "test/pg-dep", "shared-postgres")
	if err != nil {
		t.Fatalf("Dependencies: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("len(deps) = %d, want 1", len(deps))
	}
	if deps[0].Type != "hard" {
		t.Errorf("Type = %q, want hard", deps[0].Type)
	}
}

func TestPostgresStore_DriftReports(t *testing.T) {
	t.Parallel()
	store := newTestPostgresStore(t)
	ctx := context.Background()

	now := time.Now().UTC().Truncate(time.Microsecond)
	report := &DriftReport{
		ContextPath:  "test/pg-drift",
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
		t.Error("expected report.ID to be set")
	}

	reports, err := store.ListDriftReports(ctx, "test/pg-drift", 10)
	if err != nil {
		t.Fatalf("ListDriftReports: %v", err)
	}
	if len(reports) != 1 {
		t.Fatalf("len(reports) = %d, want 1", len(reports))
	}
}

func TestPostgresStore_Lock(t *testing.T) {
	t.Parallel()
	store := newTestPostgresStore(t)
	ctx := context.Background()

	handle, err := store.Lock(ctx, "test/pg-lock", 5*time.Minute)
	if err != nil {
		t.Fatalf("Lock: %v", err)
	}

	if err := handle.Unlock(ctx); err != nil {
		t.Fatalf("Unlock: %v", err)
	}

	// Re-acquire should succeed
	handle2, err := store.Lock(ctx, "test/pg-lock", 5*time.Minute)
	if err != nil {
		t.Fatalf("Lock (re-acquire): %v", err)
	}
	handle2.Unlock(ctx)
}
