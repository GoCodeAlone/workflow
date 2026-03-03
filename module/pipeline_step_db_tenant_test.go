package module

import (
	"context"
	"database/sql"
	"testing"
)

// testPartitionKeyProvider wraps a *sql.DB to satisfy PartitionKeyProvider.
type testPartitionKeyProvider struct {
	db           *sql.DB
	partitionKey string
}

func (p *testPartitionKeyProvider) DB() *sql.DB          { return p.db }
func (p *testPartitionKeyProvider) PartitionKey() string { return p.partitionKey }

// mockAppWithPartitionDB creates a MockApplication with a PartitionKeyProvider service.
func mockAppWithPartitionDB(name string, db *sql.DB, partitionKey string) *MockApplication {
	app := NewMockApplication()
	app.Services[name] = &testPartitionKeyProvider{db: db, partitionKey: partitionKey}
	return app
}

// setupTenantTestDB creates an in-memory SQLite database with tenant-scoped test data.
func setupTenantTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE forms (
			id TEXT PRIMARY KEY,
			tenant_id TEXT NOT NULL,
			slug TEXT NOT NULL
		);
		INSERT INTO forms (id, tenant_id, slug) VALUES ('f1', 'org-alpha', 'contact');
		INSERT INTO forms (id, tenant_id, slug) VALUES ('f2', 'org-alpha', 'feedback');
		INSERT INTO forms (id, tenant_id, slug) VALUES ('f3', 'org-beta', 'signup');
	`)
	if err != nil {
		t.Fatalf("setup tenant db: %v", err)
	}
	return db
}

func TestDBQueryStep_TenantKey_AutoFilter(t *testing.T) {
	db := setupTenantTestDB(t)
	app := mockAppWithPartitionDB("part-db", db, "tenant_id")

	factory := NewDBQueryStepFactory()
	step, err := factory("list-forms", map[string]any{
		"database":  "part-db",
		"query":     "SELECT id, slug FROM forms",
		"tenantKey": "steps.auth.tenant_id",
		"mode":      "list",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("auth", map[string]any{"tenant_id": "org-alpha"})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	rows, ok := result.Output["rows"].([]map[string]any)
	if !ok {
		t.Fatal("expected rows in output")
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows for org-alpha, got %d", len(rows))
	}
}

func TestDBQueryStep_TenantKey_NoPartitionKeyProvider(t *testing.T) {
	db := setupTenantTestDB(t)
	// Use a plain DBProvider (no PartitionKeyProvider)
	app := mockAppWithDB("plain-db", db)

	factory := NewDBQueryStepFactory()
	step, err := factory("list-forms", map[string]any{
		"database":  "plain-db",
		"query":     "SELECT id FROM forms",
		"tenantKey": "steps.auth.tenant_id",
		"mode":      "list",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("auth", map[string]any{"tenant_id": "org-alpha"})

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when database does not implement PartitionKeyProvider")
	}
}

func TestDBQueryStep_TenantKey_NilTenantValue(t *testing.T) {
	db := setupTenantTestDB(t)
	app := mockAppWithPartitionDB("part-db", db, "tenant_id")

	factory := NewDBQueryStepFactory()
	step, err := factory("list-forms", map[string]any{
		"database":  "part-db",
		"query":     "SELECT id FROM forms",
		"tenantKey": "steps.auth.tenant_id",
		"mode":      "list",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	// Pipeline context without auth.tenant_id set
	pc := NewPipelineContext(nil, nil)

	_, err = step.Execute(context.Background(), pc)
	if err == nil {
		t.Fatal("expected error when tenant value is nil")
	}
}

func TestDBExecStep_TenantKey_AutoFilter(t *testing.T) {
	db := setupTenantTestDB(t)
	app := mockAppWithPartitionDB("part-db", db, "tenant_id")

	factory := NewDBExecStepFactory()
	step, err := factory("update-form", map[string]any{
		"database":  "part-db",
		"query":     "UPDATE forms SET slug = $1 WHERE id = $2",
		"params":    []any{"new-slug", "f1"},
		"tenantKey": "steps.auth.tenant_id",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("auth", map[string]any{"tenant_id": "org-alpha"})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	affected, _ := result.Output["affected_rows"].(int64)
	if affected != 1 {
		t.Errorf("expected 1 affected row, got %d", affected)
	}
}

func TestAppendTenantFilter_WithWhereClause(t *testing.T) {
	query := "SELECT * FROM forms WHERE active = true"
	result := appendTenantFilter(query, "tenant_id", 1)
	expected := "SELECT * FROM forms WHERE active = true AND tenant_id = $1"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestAppendTenantFilter_WithoutWhereClause(t *testing.T) {
	query := "SELECT * FROM forms"
	result := appendTenantFilter(query, "tenant_id", 2)
	expected := "SELECT * FROM forms WHERE tenant_id = $2"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}

func TestAppendTenantFilter_TrailingWhitespace(t *testing.T) {
	query := "SELECT * FROM forms ORDER BY created_at  "
	result := appendTenantFilter(query, "tenant_id", 1)
	expected := "SELECT * FROM forms ORDER BY created_at WHERE tenant_id = $1"
	if result != expected {
		t.Errorf("expected %q, got %q", expected, result)
	}
}
