package module

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

// testDBProvider wraps a *sql.DB to satisfy DBProvider
type testDBProvider struct {
	db *sql.DB
}

func (p *testDBProvider) DB() *sql.DB { return p.db }

// setupTestDB creates an in-memory SQLite database with test data
func setupTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	_, err = db.Exec(`
		CREATE TABLE companies (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			slug TEXT NOT NULL,
			owner_id TEXT NOT NULL DEFAULT '',
			parent_id TEXT,
			is_system INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		);
		INSERT INTO companies (id, name, slug, owner_id, created_at, updated_at)
		VALUES ('c1', 'Acme Corp', 'acme', 'u1', '2024-01-01T00:00:00Z', '2024-01-01T00:00:00Z');
		INSERT INTO companies (id, name, slug, owner_id, created_at, updated_at)
		VALUES ('c2', 'Beta Inc', 'beta', 'u2', '2024-01-02T00:00:00Z', '2024-01-02T00:00:00Z');
		INSERT INTO companies (id, name, slug, owner_id, parent_id, created_at, updated_at)
		VALUES ('o1', 'Acme Org', 'acme-org', 'u1', 'c1', '2024-01-03T00:00:00Z', '2024-01-03T00:00:00Z');
	`)
	if err != nil {
		t.Fatalf("setup db: %v", err)
	}
	return db
}

// mockAppWithDB creates a MockApplication with a database service registered
func mockAppWithDB(name string, db *sql.DB) *MockApplication {
	app := NewMockApplication()
	app.Services[name] = &testDBProvider{db: db}
	return app
}

func TestDBQueryStep_ListMode(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryStepFactory()
	step, err := factory("list-companies", map[string]any{
		"database": "test-db",
		"query":    "SELECT id, name, slug FROM companies WHERE parent_id IS NULL ORDER BY name",
		"mode":     "list",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	rows, ok := result.Output["rows"].([]map[string]any)
	if !ok {
		t.Fatal("expected rows in output")
	}
	count, ok := result.Output["count"].(int)
	if !ok {
		t.Fatal("expected count in output")
	}
	if count != 2 {
		t.Errorf("expected count=2, got %d", count)
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows, got %d", len(rows))
	}
	if rows[0]["name"] != "Acme Corp" {
		t.Errorf("expected first row name='Acme Corp', got %v", rows[0]["name"])
	}
}

func TestDBQueryStep_SingleMode_Found(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryStepFactory()
	step, err := factory("get-company", map[string]any{
		"database": "test-db",
		"query":    "SELECT id, name, slug FROM companies WHERE id = ?",
		"params":   []any{"c1"},
		"mode":     "single",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	found, _ := result.Output["found"].(bool)
	if !found {
		t.Error("expected found=true")
	}
	row, ok := result.Output["row"].(map[string]any)
	if !ok {
		t.Fatal("expected row in output")
	}
	if row["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp', got %v", row["name"])
	}
}

func TestDBQueryStep_SingleMode_NotFound(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryStepFactory()
	step, err := factory("get-missing", map[string]any{
		"database": "test-db",
		"query":    "SELECT id, name FROM companies WHERE id = ?",
		"params":   []any{"nonexistent"},
		"mode":     "single",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	found, _ := result.Output["found"].(bool)
	if found {
		t.Error("expected found=false")
	}
}

func TestDBQueryStep_TemplateParams(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryStepFactory()
	step, err := factory("query-with-template", map[string]any{
		"database": "test-db",
		"query":    "SELECT id, name FROM companies WHERE id = ?",
		"params":   []any{"{{index .steps \"parse-request\" \"path_params\" \"id\"}}"},
		"mode":     "single",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse-request", map[string]any{
		"path_params": map[string]any{"id": "c1"},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	found, _ := result.Output["found"].(bool)
	if !found {
		t.Error("expected found=true")
	}
	row, _ := result.Output["row"].(map[string]any)
	if row["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp', got %v", row["name"])
	}
}

func TestDBQueryStep_RejectsTemplateInQuery(t *testing.T) {
	factory := NewDBQueryStepFactory()
	_, err := factory("bad-query", map[string]any{
		"database": "test-db",
		"query":    "SELECT * FROM companies WHERE id = '{{ .id }}'",
		"mode":     "list",
	}, nil)
	if err == nil {
		t.Fatal("expected error for template in query")
	}
}

func TestDBQueryStep_MissingDatabase(t *testing.T) {
	factory := NewDBQueryStepFactory()
	_, err := factory("no-db", map[string]any{
		"query": "SELECT 1",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing database")
	}
}

func TestDBQueryStep_EmptyResult(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryStepFactory()
	step, err := factory("empty-list", map[string]any{
		"database": "test-db",
		"query":    "SELECT id, name FROM companies WHERE id = 'nonexistent'",
		"mode":     "list",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	rows, ok := result.Output["rows"].([]map[string]any)
	if !ok {
		t.Fatal("expected rows in output")
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
	count, _ := result.Output["count"].(int)
	if count != 0 {
		t.Errorf("expected count=0, got %d", count)
	}
}
