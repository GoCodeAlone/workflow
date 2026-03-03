package module

import (
	"context"
	"testing"
	"time"
)

func TestDBQueryCachedStep_CacheMiss(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("lookup-config", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = ?",
		"params":    []any{"c1"},
		"cache_key": "company:c1",
		"cache_ttl": "5m",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["cache_hit"] != false {
		t.Errorf("expected cache_hit=false on first call, got %v", result.Output["cache_hit"])
	}
	if result.Output["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp', got %v", result.Output["name"])
	}
}

func TestDBQueryCachedStep_CacheHit(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("lookup-config", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = ?",
		"params":    []any{"c1"},
		"cache_key": "company:c1",
		"cache_ttl": "5m",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)

	// First call — cache miss
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}

	// Second call — cache hit
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("second execute error: %v", err)
	}

	if result.Output["cache_hit"] != true {
		t.Errorf("expected cache_hit=true on second call, got %v", result.Output["cache_hit"])
	}
	if result.Output["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp' on cache hit, got %v", result.Output["name"])
	}
}

func TestDBQueryCachedStep_TTLExpiry(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("lookup-config", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = ?",
		"params":    []any{"c1"},
		"cache_key": "company:c1",
		"cache_ttl": "1ms",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)

	// First call — cache miss
	first, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}
	if first.Output["cache_hit"] != false {
		t.Errorf("expected cache_hit=false on first call")
	}

	// Wait for TTL to expire
	time.Sleep(5 * time.Millisecond)

	// Second call — should be cache miss again due to expiry
	second, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("second execute error: %v", err)
	}
	if second.Output["cache_hit"] != false {
		t.Errorf("expected cache_hit=false after TTL expiry, got %v", second.Output["cache_hit"])
	}
}

func TestDBQueryCachedStep_ScanFields(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("lookup-config", map[string]any{
		"database":    "test-db",
		"query":       "SELECT id, name, slug FROM companies WHERE id = ?",
		"params":      []any{"c1"},
		"cache_key":   "company:c1",
		"scan_fields": []any{"name"},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp', got %v", result.Output["name"])
	}
	// id and slug should not be in output since scan_fields only has "name"
	if _, ok := result.Output["id"]; ok {
		t.Errorf("expected 'id' to be excluded from output when not in scan_fields")
	}
	if _, ok := result.Output["slug"]; ok {
		t.Errorf("expected 'slug' to be excluded from output when not in scan_fields")
	}
}

func TestDBQueryCachedStep_TemplateParams(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("lookup-config", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = ?",
		"params":    []any{"{{index .steps \"parse\" \"path_params\" \"id\"}}"},
		"cache_key": "company:{{index .steps \"parse\" \"path_params\" \"id\"}}",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("parse", map[string]any{
		"path_params": map[string]any{"id": "c2"},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["name"] != "Beta Inc" {
		t.Errorf("expected name='Beta Inc', got %v", result.Output["name"])
	}
}

func TestDBQueryCachedStep_DefaultTTL(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	// No cache_ttl specified — should default to 5m
	factory := NewDBQueryCachedStepFactory()
	step, err := factory("lookup-config", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id FROM companies WHERE id = ?",
		"params":    []any{"c1"},
		"cache_key": "company:c1",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}
	if result.Output["cache_hit"] != false {
		t.Errorf("expected cache_hit=false on first call")
	}

	// Immediately call again — should be a hit
	result2, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("second execute error: %v", err)
	}
	if result2.Output["cache_hit"] != true {
		t.Errorf("expected cache_hit=true on second call with default TTL")
	}
}

func TestDBQueryCachedStep_MissingDatabase(t *testing.T) {
	factory := NewDBQueryCachedStepFactory()
	_, err := factory("bad", map[string]any{
		"query":     "SELECT 1",
		"cache_key": "k",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing database")
	}
}

func TestDBQueryCachedStep_MissingQuery(t *testing.T) {
	factory := NewDBQueryCachedStepFactory()
	_, err := factory("bad", map[string]any{
		"database":  "db",
		"cache_key": "k",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing query")
	}
}

func TestDBQueryCachedStep_MissingCacheKey(t *testing.T) {
	factory := NewDBQueryCachedStepFactory()
	_, err := factory("bad", map[string]any{
		"database": "db",
		"query":    "SELECT 1",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing cache_key")
	}
}

func TestDBQueryCachedStep_InvalidTTL(t *testing.T) {
	factory := NewDBQueryCachedStepFactory()
	_, err := factory("bad", map[string]any{
		"database":  "db",
		"query":     "SELECT 1",
		"cache_key": "k",
		"cache_ttl": "not-a-duration",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid cache_ttl")
	}
}

func TestDBQueryCachedStep_RejectsTemplateInQuery(t *testing.T) {
	factory := NewDBQueryCachedStepFactory()
	_, err := factory("bad", map[string]any{
		"database":  "db",
		"query":     "SELECT * FROM t WHERE id = '{{.id}}'",
		"cache_key": "k",
	}, nil)
	if err == nil {
		t.Fatal("expected error for template in query")
	}
}

func TestDBQueryCachedStep_NoRows(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("lookup-missing", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = ?",
		"params":    []any{"nonexistent"},
		"cache_key": "company:nonexistent",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// No rows means empty output (no id/name keys), cache_hit=false
	if result.Output["cache_hit"] != false {
		t.Errorf("expected cache_hit=false, got %v", result.Output["cache_hit"])
	}
	if _, ok := result.Output["id"]; ok {
		t.Errorf("expected no 'id' field when no rows returned")
	}
}

// TestDBQueryCachedStep_PostgresPlaceholderNormalization verifies that $1-style
// placeholders are converted to ? for SQLite (driver "sqlite" triggers normalization).
func TestDBQueryCachedStep_PostgresPlaceholderNormalization(t *testing.T) {
	db := setupTestDB(t)
	// Register with SQLite driver name so normalizePlaceholders converts $1 → ?
	app := mockAppWithDBDriver("test-db", db, "sqlite")

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("lookup-by-dollar", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = $1",
		"params":    []any{"c1"},
		"cache_key": "company:c1",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error (placeholder normalization may have failed): %v", err)
	}

	if result.Output["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp' after $1→? normalization, got %v", result.Output["name"])
	}
}

// TestDBQueryCachedStep_CacheHitSkipsDB verifies that a cache hit does not re-query
// the database. After the first call succeeds, we close the DB; a second call should
// still succeed from the cache without hitting the closed connection.
func TestDBQueryCachedStep_CacheHitSkipsDB(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("lookup-config", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = ?",
		"params":    []any{"c1"},
		"cache_key": "company:c1",
		"cache_ttl": "5m",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)

	// First call — populates the cache
	first, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}
	if first.Output["cache_hit"] != false {
		t.Errorf("expected cache_hit=false on first call")
	}

	// Close the DB — any DB access from here will fail
	if err := db.Close(); err != nil {
		t.Fatalf("failed to close db: %v", err)
	}

	// Second call — must be served from cache, not from the (now-closed) DB
	second, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("second execute should have hit cache, got error: %v", err)
	}
	if second.Output["cache_hit"] != true {
		t.Errorf("expected cache_hit=true on second call after DB closed")
	}
	if second.Output["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp' from cache, got %v", second.Output["name"])
	}
}

// TestDBQueryCachedStep_ZeroTTLRejected verifies that cache_ttl=0 is rejected.
func TestDBQueryCachedStep_ZeroTTLRejected(t *testing.T) {
	factory := NewDBQueryCachedStepFactory()
	_, err := factory("bad", map[string]any{
		"database":  "db",
		"query":     "SELECT 1",
		"cache_key": "k",
		"cache_ttl": "0s",
	}, nil)
	if err == nil {
		t.Fatal("expected error for zero cache_ttl")
	}
}

// TestDBQueryCachedStep_NegativeTTLRejected verifies that a negative cache_ttl is rejected.
func TestDBQueryCachedStep_NegativeTTLRejected(t *testing.T) {
	factory := NewDBQueryCachedStepFactory()
	_, err := factory("bad", map[string]any{
		"database":  "db",
		"query":     "SELECT 1",
		"cache_key": "k",
		"cache_ttl": "-1s",
	}, nil)
	if err == nil {
		t.Fatal("expected error for negative cache_ttl")
	}
}

func TestDBQueryCachedStep_ListMode(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("list-companies", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE parent_id IS NULL ORDER BY name",
		"cache_key": "companies:all",
		"cache_ttl": "5m",
		"mode":      "list",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	if result.Output["cache_hit"] != false {
		t.Errorf("expected cache_hit=false on first call, got %v", result.Output["cache_hit"])
	}

	rows, ok := result.Output["rows"].([]map[string]any)
	if !ok {
		t.Fatalf("expected rows to be []map[string]any, got %T", result.Output["rows"])
	}
	count, ok := result.Output["count"].(int)
	if !ok {
		t.Fatalf("expected count to be int, got %T", result.Output["count"])
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

func TestDBQueryCachedStep_ListModeCacheHit(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("list-companies", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE parent_id IS NULL ORDER BY name",
		"cache_key": "companies:list-hit",
		"cache_ttl": "5m",
		"mode":      "list",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)

	// First call — cache miss
	_, err = step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("first execute error: %v", err)
	}

	// Second call — cache hit
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("second execute error: %v", err)
	}

	if result.Output["cache_hit"] != true {
		t.Errorf("expected cache_hit=true on second call, got %v", result.Output["cache_hit"])
	}
	count, ok := result.Output["count"].(int)
	if !ok {
		t.Fatalf("expected count to be int, got %T", result.Output["count"])
	}
	if count != 2 {
		t.Errorf("expected count=2 on cache hit, got %d", count)
	}
}

func TestDBQueryCachedStep_ListModeNoRows(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("list-empty", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = ?",
		"params":    []any{"nonexistent"},
		"cache_key": "companies:empty",
		"mode":      "list",
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
		t.Fatalf("expected rows to be []map[string]any, got %T", result.Output["rows"])
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
	count, _ := result.Output["count"].(int)
	if count != 0 {
		t.Errorf("expected count=0, got %d", count)
	}
}

func TestDBQueryCachedStep_InvalidMode(t *testing.T) {
	factory := NewDBQueryCachedStepFactory()
	_, err := factory("bad-mode", map[string]any{
		"database":  "db",
		"query":     "SELECT 1",
		"cache_key": "k",
		"mode":      "invalid",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

func TestDBQueryCachedStep_DefaultModeIsSingle(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("default-mode", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = ?",
		"params":    []any{"c1"},
		"cache_key": "company:default-mode",
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	// Default mode is single — expect flat columns, not rows/count
	if result.Output["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp' in single mode, got %v", result.Output["name"])
	}
	if _, ok := result.Output["rows"]; ok {
		t.Error("expected no 'rows' key in single mode output")
	}
}
