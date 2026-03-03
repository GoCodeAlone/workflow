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
	row, ok := result.Output["row"].(map[string]any)
	if !ok {
		t.Fatal("expected row in output")
	}
	if row["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp', got %v", row["name"])
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
	row, ok := result.Output["row"].(map[string]any)
	if !ok {
		t.Fatal("expected row in output on cache hit")
	}
	if row["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp' on cache hit, got %v", row["name"])
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

	row, ok := result.Output["row"].(map[string]any)
	if !ok {
		t.Fatal("expected row in output")
	}
	if row["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp', got %v", row["name"])
	}
	// id and slug should not be in output since scan_fields only has "name"
	if _, ok := row["id"]; ok {
		t.Errorf("expected 'id' to be excluded from row when not in scan_fields")
	}
	if _, ok := row["slug"]; ok {
		t.Errorf("expected 'slug' to be excluded from row when not in scan_fields")
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

	row, ok := result.Output["row"].(map[string]any)
	if !ok {
		t.Fatal("expected row in output")
	}
	if row["name"] != "Beta Inc" {
		t.Errorf("expected name='Beta Inc', got %v", row["name"])
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

	// No rows in single mode means row={}, found=false, cache_hit=false
	if result.Output["cache_hit"] != false {
		t.Errorf("expected cache_hit=false, got %v", result.Output["cache_hit"])
	}
	found, _ := result.Output["found"].(bool)
	if found {
		t.Errorf("expected found=false when no rows returned")
	}
	row, ok := result.Output["row"].(map[string]any)
	if !ok {
		t.Fatal("expected row in output even when no rows found")
	}
	if len(row) != 0 {
		t.Errorf("expected empty row map, got %v", row)
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

	row, ok := result.Output["row"].(map[string]any)
	if !ok {
		t.Fatal("expected row in output after $1→? normalization")
	}
	if row["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp' after $1→? normalization, got %v", row["name"])
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
	secondRow, ok := second.Output["row"].(map[string]any)
	if !ok {
		t.Fatal("expected row in cached output")
	}
	if secondRow["name"] != "Acme Corp" {
		t.Errorf("expected name='Acme Corp' from cache, got %v", secondRow["name"])
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

// TestDBQueryCachedStep_InvalidModeRejected verifies that an unknown mode is rejected.
func TestDBQueryCachedStep_InvalidModeRejected(t *testing.T) {
	factory := NewDBQueryCachedStepFactory()
	_, err := factory("bad", map[string]any{
		"database":  "db",
		"query":     "SELECT 1",
		"cache_key": "k",
		"mode":      "bulk",
	}, nil)
	if err == nil {
		t.Fatal("expected error for invalid mode")
	}
}

// TestDBQueryCachedStep_ListMode verifies that mode: list returns rows/count format.
func TestDBQueryCachedStep_ListMode(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("list-companies", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name, slug FROM companies WHERE parent_id IS NULL ORDER BY name",
		"mode":      "list",
		"cache_key": "companies:list",
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
	rows, ok := result.Output["rows"].([]map[string]any)
	if !ok {
		t.Fatal("expected rows in output for list mode")
	}
	count, ok := result.Output["count"].(int)
	if !ok {
		t.Fatal("expected count in output for list mode")
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

// TestDBQueryCachedStep_ListModeCacheHit verifies that list mode results are cached and returned correctly.
func TestDBQueryCachedStep_ListModeCacheHit(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("list-companies", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE parent_id IS NULL ORDER BY name",
		"mode":      "list",
		"cache_key": "companies:list",
		"cache_ttl": "5m",
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

	// Second call — cache hit
	second, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("second execute error: %v", err)
	}
	if second.Output["cache_hit"] != true {
		t.Errorf("expected cache_hit=true on second call, got %v", second.Output["cache_hit"])
	}
	rows, ok := second.Output["rows"].([]map[string]any)
	if !ok {
		t.Fatal("expected rows in cached output")
	}
	if len(rows) != 2 {
		t.Errorf("expected 2 rows from cache, got %d", len(rows))
	}
}

// TestDBQueryCachedStep_ListModeEmpty verifies that list mode returns an empty rows slice when no rows match.
func TestDBQueryCachedStep_ListModeEmpty(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("list-empty", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = ?",
		"params":    []any{"nonexistent"},
		"mode":      "list",
		"cache_key": "companies:empty",
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
		t.Fatal("expected rows in output for list mode even when empty")
	}
	if len(rows) != 0 {
		t.Errorf("expected 0 rows, got %d", len(rows))
	}
	count, _ := result.Output["count"].(int)
	if count != 0 {
		t.Errorf("expected count=0, got %d", count)
	}
}

// TestDBQueryCachedStep_SingleModeFound verifies that mode: single returns row/found format when a row is found.
func TestDBQueryCachedStep_SingleModeFound(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("get-company", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = ?",
		"params":    []any{"c1"},
		"mode":      "single",
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

// TestDBQueryCachedStep_SingleModeNotFound verifies that mode: single returns row={}/found=false when no row matches.
func TestDBQueryCachedStep_SingleModeNotFound(t *testing.T) {
	db := setupTestDB(t)
	app := mockAppWithDB("test-db", db)

	factory := NewDBQueryCachedStepFactory()
	step, err := factory("get-missing", map[string]any{
		"database":  "test-db",
		"query":     "SELECT id, name FROM companies WHERE id = ?",
		"params":    []any{"nonexistent"},
		"mode":      "single",
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

	found, _ := result.Output["found"].(bool)
	if found {
		t.Error("expected found=false")
	}
	row, ok := result.Output["row"].(map[string]any)
	if !ok {
		t.Fatal("expected row in output even when not found")
	}
	if len(row) != 0 {
		t.Errorf("expected empty row map, got %v", row)
	}
}
