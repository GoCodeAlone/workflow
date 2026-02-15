package infra

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
}

func TestNewProvisioner(t *testing.T) {
	p := NewProvisioner(testLogger())
	if p == nil {
		t.Fatal("expected non-nil provisioner")
	}
	status := p.Status()
	if len(status) != 0 {
		t.Errorf("expected empty status, got %d resources", len(status))
	}
}

func TestNewProvisionerNilLogger(t *testing.T) {
	p := NewProvisioner(nil)
	if p == nil {
		t.Fatal("expected non-nil provisioner with nil logger")
	}
}

func TestPlanCreate(t *testing.T) {
	p := NewProvisioner(testLogger())

	desired := InfraConfig{
		Resources: []ResourceConfig{
			{Name: "main-db", Type: "database", Provider: "sqlite", Config: map[string]any{"path": "/tmp/db.sqlite"}},
			{Name: "app-cache", Type: "cache", Provider: "memory"},
		},
	}

	plan, err := p.Plan(desired)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	if len(plan.Create) != 2 {
		t.Errorf("expected 2 creates, got %d", len(plan.Create))
	}
	if len(plan.Update) != 0 {
		t.Errorf("expected 0 updates, got %d", len(plan.Update))
	}
	if len(plan.Delete) != 0 {
		t.Errorf("expected 0 deletes, got %d", len(plan.Delete))
	}
	if len(plan.Current) != 0 {
		t.Errorf("expected 0 current, got %d", len(plan.Current))
	}
}

func TestPlanUpdate(t *testing.T) {
	p := NewProvisioner(testLogger())

	// Provision initial resource.
	initial := InfraConfig{
		Resources: []ResourceConfig{
			{Name: "main-db", Type: "database", Provider: "sqlite", Config: map[string]any{"path": "/tmp/old.db"}},
		},
	}
	plan, err := p.Plan(initial)
	if err != nil {
		t.Fatalf("initial plan: %v", err)
	}
	if err := p.Apply(context.Background(), plan); err != nil {
		t.Fatalf("initial apply: %v", err)
	}

	// Now plan with a changed config.
	updated := InfraConfig{
		Resources: []ResourceConfig{
			{Name: "main-db", Type: "database", Provider: "sqlite", Config: map[string]any{"path": "/tmp/new.db"}},
		},
	}
	plan, err = p.Plan(updated)
	if err != nil {
		t.Fatalf("update plan: %v", err)
	}
	if len(plan.Create) != 0 {
		t.Errorf("expected 0 creates, got %d", len(plan.Create))
	}
	if len(plan.Update) != 1 {
		t.Errorf("expected 1 update, got %d", len(plan.Update))
	}
	if len(plan.Delete) != 0 {
		t.Errorf("expected 0 deletes, got %d", len(plan.Delete))
	}
}

func TestPlanDelete(t *testing.T) {
	p := NewProvisioner(testLogger())

	// Provision initial resources.
	initial := InfraConfig{
		Resources: []ResourceConfig{
			{Name: "main-db", Type: "database", Provider: "sqlite"},
			{Name: "old-cache", Type: "cache", Provider: "memory"},
		},
	}
	plan, _ := p.Plan(initial)
	_ = p.Apply(context.Background(), plan)

	// Plan with one resource removed.
	desired := InfraConfig{
		Resources: []ResourceConfig{
			{Name: "main-db", Type: "database", Provider: "sqlite"},
		},
	}
	plan, err := p.Plan(desired)
	if err != nil {
		t.Fatalf("delete plan: %v", err)
	}
	if len(plan.Create) != 0 {
		t.Errorf("expected 0 creates, got %d", len(plan.Create))
	}
	if len(plan.Update) != 0 {
		t.Errorf("expected 0 updates, got %d", len(plan.Update))
	}
	if len(plan.Delete) != 1 {
		t.Errorf("expected 1 delete, got %d", len(plan.Delete))
	}
	if plan.Delete[0].Name != "old-cache" {
		t.Errorf("expected old-cache to be deleted, got %s", plan.Delete[0].Name)
	}
}

func TestPlanMixedOperations(t *testing.T) {
	p := NewProvisioner(testLogger())

	// Provision initial resources.
	initial := InfraConfig{
		Resources: []ResourceConfig{
			{Name: "keep-db", Type: "database", Provider: "sqlite"},
			{Name: "change-cache", Type: "cache", Provider: "memory", Config: map[string]any{"size": 100}},
			{Name: "remove-queue", Type: "queue", Provider: "memory"},
		},
	}
	plan, _ := p.Plan(initial)
	_ = p.Apply(context.Background(), plan)

	// Desired: keep one, update one, delete one, create one.
	desired := InfraConfig{
		Resources: []ResourceConfig{
			{Name: "keep-db", Type: "database", Provider: "sqlite"},
			{Name: "change-cache", Type: "cache", Provider: "redis"},
			{Name: "new-storage", Type: "storage", Provider: "memory"},
		},
	}
	plan, err := p.Plan(desired)
	if err != nil {
		t.Fatalf("mixed plan: %v", err)
	}
	if len(plan.Create) != 1 {
		t.Errorf("expected 1 create, got %d", len(plan.Create))
	}
	if len(plan.Update) != 1 {
		t.Errorf("expected 1 update, got %d", len(plan.Update))
	}
	if len(plan.Delete) != 1 {
		t.Errorf("expected 1 delete, got %d", len(plan.Delete))
	}
	if len(plan.Current) != 3 {
		t.Errorf("expected 3 current, got %d", len(plan.Current))
	}
}

func TestPlanValidation(t *testing.T) {
	p := NewProvisioner(testLogger())

	tests := []struct {
		name    string
		config  InfraConfig
		wantErr bool
	}{
		{
			name: "empty name",
			config: InfraConfig{Resources: []ResourceConfig{
				{Name: "", Type: "database", Provider: "sqlite"},
			}},
			wantErr: true,
		},
		{
			name: "invalid type",
			config: InfraConfig{Resources: []ResourceConfig{
				{Name: "r1", Type: "invalid", Provider: "sqlite"},
			}},
			wantErr: true,
		},
		{
			name: "invalid provider",
			config: InfraConfig{Resources: []ResourceConfig{
				{Name: "r1", Type: "database", Provider: "oracle"},
			}},
			wantErr: true,
		},
		{
			name: "duplicate name",
			config: InfraConfig{Resources: []ResourceConfig{
				{Name: "r1", Type: "database", Provider: "sqlite"},
				{Name: "r1", Type: "cache", Provider: "memory"},
			}},
			wantErr: true,
		},
		{
			name:    "empty resources is valid",
			config:  InfraConfig{Resources: []ResourceConfig{}},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := p.Plan(tt.config)
			if (err != nil) != tt.wantErr {
				t.Errorf("Plan() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestApply(t *testing.T) {
	p := NewProvisioner(testLogger())
	ctx := context.Background()

	desired := InfraConfig{
		Resources: []ResourceConfig{
			{Name: "test-db", Type: "database", Provider: "sqlite"},
			{Name: "test-cache", Type: "cache", Provider: "memory"},
		},
	}

	plan, err := p.Plan(desired)
	if err != nil {
		t.Fatalf("plan: %v", err)
	}

	if err := p.Apply(ctx, plan); err != nil {
		t.Fatalf("apply: %v", err)
	}

	status := p.Status()
	if len(status) != 2 {
		t.Errorf("expected 2 resources, got %d", len(status))
	}
	if status["test-db"].Status != "provisioned" {
		t.Errorf("expected provisioned, got %s", status["test-db"].Status)
	}
	if status["test-cache"].Status != "provisioned" {
		t.Errorf("expected provisioned, got %s", status["test-cache"].Status)
	}
}

func TestApplyNilPlan(t *testing.T) {
	p := NewProvisioner(testLogger())
	if err := p.Apply(context.Background(), nil); err == nil {
		t.Error("expected error for nil plan")
	}
}

func TestApplyCancelledContext(t *testing.T) {
	p := NewProvisioner(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately.

	plan := &ProvisionPlan{
		Create: []ResourceConfig{
			{Name: "db", Type: "database", Provider: "sqlite"},
		},
	}

	err := p.Apply(ctx, plan)
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestDestroy(t *testing.T) {
	p := NewProvisioner(testLogger())
	ctx := context.Background()

	// Provision a resource.
	desired := InfraConfig{
		Resources: []ResourceConfig{
			{Name: "to-destroy", Type: "database", Provider: "sqlite"},
		},
	}
	plan, _ := p.Plan(desired)
	_ = p.Apply(ctx, plan)

	// Destroy it.
	if err := p.Destroy(ctx, "to-destroy"); err != nil {
		t.Fatalf("destroy: %v", err)
	}

	status := p.Status()
	if len(status) != 0 {
		t.Errorf("expected 0 resources after destroy, got %d", len(status))
	}
}

func TestDestroyNotFound(t *testing.T) {
	p := NewProvisioner(testLogger())
	err := p.Destroy(context.Background(), "nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent resource")
	}
}

func TestDestroyCancelledContext(t *testing.T) {
	p := NewProvisioner(testLogger())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := p.Destroy(ctx, "anything")
	if err == nil {
		t.Error("expected error for cancelled context")
	}
}

func TestStatus(t *testing.T) {
	p := NewProvisioner(testLogger())
	ctx := context.Background()

	desired := InfraConfig{
		Resources: []ResourceConfig{
			{Name: "db1", Type: "database", Provider: "sqlite"},
			{Name: "db2", Type: "database", Provider: "postgres"},
		},
	}
	plan, _ := p.Plan(desired)
	_ = p.Apply(ctx, plan)

	status := p.Status()
	if len(status) != 2 {
		t.Fatalf("expected 2, got %d", len(status))
	}

	// Verify it's a copy (modifying returned map doesn't affect internal state).
	delete(status, "db1")
	internalStatus := p.Status()
	if len(internalStatus) != 2 {
		t.Error("modifying returned status affected internal state")
	}
}

func TestParseConfig(t *testing.T) {
	raw := map[string]any{
		"resources": []any{
			map[string]any{
				"name":     "mydb",
				"type":     "database",
				"provider": "postgres",
				"config": map[string]any{
					"host": "localhost",
					"port": 5432,
				},
			},
			map[string]any{
				"name":     "mycache",
				"type":     "cache",
				"provider": "redis",
			},
		},
	}

	cfg, err := ParseConfig(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cfg.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(cfg.Resources))
	}
	if cfg.Resources[0].Name != "mydb" {
		t.Errorf("expected mydb, got %s", cfg.Resources[0].Name)
	}
	if cfg.Resources[0].Provider != "postgres" {
		t.Errorf("expected postgres, got %s", cfg.Resources[0].Provider)
	}
	if cfg.Resources[0].Config["host"] != "localhost" {
		t.Errorf("expected localhost, got %v", cfg.Resources[0].Config["host"])
	}
}

func TestParseConfigNil(t *testing.T) {
	_, err := ParseConfig(nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestParseConfigNoResources(t *testing.T) {
	cfg, err := ParseConfig(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Resources != nil {
		t.Errorf("expected nil resources, got %v", cfg.Resources)
	}
}

func TestParseConfigInvalidResources(t *testing.T) {
	_, err := ParseConfig(map[string]any{
		"resources": "not-a-list",
	})
	if err == nil {
		t.Error("expected error for non-list resources")
	}
}

func TestParseConfigMissingFields(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]any
	}{
		{
			name: "missing name",
			raw: map[string]any{
				"resources": []any{
					map[string]any{"type": "database", "provider": "sqlite"},
				},
			},
		},
		{
			name: "missing type",
			raw: map[string]any{
				"resources": []any{
					map[string]any{"name": "r1", "provider": "sqlite"},
				},
			},
		},
		{
			name: "missing provider",
			raw: map[string]any{
				"resources": []any{
					map[string]any{"name": "r1", "type": "database"},
				},
			},
		},
		{
			name: "non-map resource",
			raw: map[string]any{
				"resources": []any{"not-a-map"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConfig(tt.raw)
			if err == nil {
				t.Error("expected error")
			}
		})
	}
}

func TestConcurrentAccess(t *testing.T) {
	p := NewProvisioner(testLogger())
	ctx := context.Background()

	// Provision some initial resources.
	initial := InfraConfig{
		Resources: []ResourceConfig{
			{Name: "shared-db", Type: "database", Provider: "sqlite"},
		},
	}
	plan, _ := p.Plan(initial)
	_ = p.Apply(ctx, plan)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = p.Status()
		}()
	}
	wg.Wait()
}

func TestResourceDiffers(t *testing.T) {
	base := ResourceConfig{
		Name: "r1", Type: "database", Provider: "sqlite",
		Config: map[string]any{"path": "/tmp/db"},
	}

	same := ResourceConfig{
		Name: "r1", Type: "database", Provider: "sqlite",
		Config: map[string]any{"path": "/tmp/db"},
	}

	diffProvider := ResourceConfig{
		Name: "r1", Type: "database", Provider: "postgres",
		Config: map[string]any{"path": "/tmp/db"},
	}

	diffConfig := ResourceConfig{
		Name: "r1", Type: "database", Provider: "sqlite",
		Config: map[string]any{"path": "/tmp/other"},
	}

	if resourceDiffers(base, same) {
		t.Error("expected same configs to not differ")
	}
	if !resourceDiffers(base, diffProvider) {
		t.Error("expected different providers to differ")
	}
	if !resourceDiffers(base, diffConfig) {
		t.Error("expected different configs to differ")
	}
}
