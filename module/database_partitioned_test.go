package module

import (
	"context"
	"database/sql"
	"testing"
)

func TestPartitionedDatabase_PartitionKey(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		DSN:          "postgres://localhost/test",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms", "submissions"},
	}
	pd := NewPartitionedDatabase("db", cfg)

	if pd.PartitionKey() != "tenant_id" {
		t.Errorf("expected tenant_id, got %q", pd.PartitionKey())
	}
	if pd.Name() != "db" {
		t.Errorf("expected name 'db', got %q", pd.Name())
	}
	tables := pd.Tables()
	if len(tables) != 2 {
		t.Errorf("expected 2 tables, got %d", len(tables))
	}
}

func TestPartitionedDatabase_EnsurePartition_InvalidDriver(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:       "sqlite3",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.EnsurePartition(context.Background(), "org-alpha")
	if err == nil {
		t.Fatal("expected error for non-postgres driver")
	}
}

func TestPartitionedDatabase_EnsurePartition_InvalidTenantValue(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "tenant_id",
		Tables:       []string{"forms"},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.EnsurePartition(context.Background(), "org'; DROP TABLE forms; --")
	if err == nil {
		t.Fatal("expected error for invalid tenant value")
	}
}

func TestPartitionedDatabase_EnsurePartition_InvalidPartitionKey(t *testing.T) {
	cfg := PartitionedDatabaseConfig{
		Driver:       "pgx",
		PartitionKey: "bad column name!",
		Tables:       []string{"forms"},
	}
	pd := NewPartitionedDatabase("db", cfg)

	err := pd.EnsurePartition(context.Background(), "org-alpha")
	if err == nil {
		t.Fatal("expected error for invalid partition key")
	}
}

func TestSanitizePartitionSuffix(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"org-alpha", "org_alpha"},
		{"org.beta", "org_beta"},
		{"tenant_1", "tenant_1"},
		{"org-my.tenant", "org_my_tenant"},
	}

	for _, tc := range tests {
		got := sanitizePartitionSuffix(tc.input)
		if got != tc.expected {
			t.Errorf("sanitizePartitionSuffix(%q) = %q, want %q", tc.input, got, tc.expected)
		}
	}
}

func TestIsSupportedPartitionDriver(t *testing.T) {
	supported := []string{"pgx", "pgx/v5", "postgres", "postgresql"}
	for _, d := range supported {
		if !isSupportedPartitionDriver(d) {
			t.Errorf("expected %q to be supported", d)
		}
	}

	unsupported := []string{"sqlite3", "sqlite", "mysql", ""}
	for _, d := range unsupported {
		if isSupportedPartitionDriver(d) {
			t.Errorf("expected %q to be unsupported", d)
		}
	}
}

// testPartitionManager is a mock PartitionManager for testing step.db_create_partition.
type testPartitionManager struct {
	partitionKey string
	partitions   map[string]bool
}

func (p *testPartitionManager) DB() *sql.DB          { return nil }
func (p *testPartitionManager) PartitionKey() string { return p.partitionKey }
func (p *testPartitionManager) EnsurePartition(_ context.Context, tenantValue string) error {
	if p.partitions == nil {
		p.partitions = make(map[string]bool)
	}
	p.partitions[tenantValue] = true
	return nil
}

func TestDBCreatePartitionStep_Execute(t *testing.T) {
	mgr := &testPartitionManager{partitionKey: "tenant_id"}
	app := NewMockApplication()
	app.Services["part-db"] = mgr

	factory := NewDBCreatePartitionStepFactory()
	step, err := factory("create-part", map[string]any{
		"database":  "part-db",
		"tenantKey": "steps.body.tenant_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("body", map[string]any{"tenant_id": "new-org"})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["tenant"] != "new-org" {
		t.Errorf("expected tenant='new-org', got %v", result.Output["tenant"])
	}
	if !mgr.partitions["new-org"] {
		t.Error("expected partition to be created for new-org")
	}
}

func TestDBCreatePartitionStep_MissingDatabase(t *testing.T) {
	factory := NewDBCreatePartitionStepFactory()
	_, err := factory("create-part", map[string]any{
		"tenantKey": "body.tenant_id",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing database")
	}
}

func TestDBCreatePartitionStep_MissingTenantKey(t *testing.T) {
	factory := NewDBCreatePartitionStepFactory()
	_, err := factory("create-part", map[string]any{
		"database": "part-db",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing tenantKey")
	}
}

func TestDBCreatePartitionStep_NotPartitionManager(t *testing.T) {
	db := setupTenantTestDB(t)
	app := mockAppWithDB("plain-db", db)

	factory := NewDBCreatePartitionStepFactory()
	step, err := factory("create-part", map[string]any{
		"database":  "plain-db",
		"tenantKey": "body.tenant_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("body", map[string]any{"tenant_id": "new-org"})

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when service does not implement PartitionManager")
	}
}

func TestDBCreatePartitionStep_NilTenantValue(t *testing.T) {
	mgr := &testPartitionManager{partitionKey: "tenant_id"}
	app := NewMockApplication()
	app.Services["part-db"] = mgr

	factory := NewDBCreatePartitionStepFactory()
	step, err := factory("create-part", map[string]any{
		"database":  "part-db",
		"tenantKey": "steps.body.tenant_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// No tenant_id in context
	pc := NewPipelineContext(nil, nil)

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when tenant value is nil")
	}
}
