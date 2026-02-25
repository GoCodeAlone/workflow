package module

import (
	"context"
	"testing"
)

func TestMemoryNoSQL_PutGetDelete(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryNoSQL("test-store", MemoryNoSQLConfig{Collection: "items"})

	// Put an item
	if err := store.Put(ctx, "item-1", map[string]any{"name": "Widget", "price": 9.99}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}

	// Get it back
	item, err := store.Get(ctx, "item-1")
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}
	if item == nil {
		t.Fatal("Get returned nil for existing key")
	}
	if item["name"] != "Widget" {
		t.Errorf("expected name=Widget, got %v", item["name"])
	}

	// Get non-existent key returns nil, nil
	missing, err := store.Get(ctx, "does-not-exist")
	if err != nil {
		t.Fatalf("Get missing key returned error: %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing key, got %v", missing)
	}

	// Delete
	if err := store.Delete(ctx, "item-1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Confirm deleted
	deleted, err := store.Get(ctx, "item-1")
	if err != nil {
		t.Fatalf("Get after delete returned error: %v", err)
	}
	if deleted != nil {
		t.Error("expected nil after delete")
	}

	// Delete of non-existent key is not an error
	if err := store.Delete(ctx, "does-not-exist"); err != nil {
		t.Errorf("Delete of missing key should not error: %v", err)
	}
}

func TestMemoryNoSQL_PutEmptyKeyError(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryNoSQL("test-store", MemoryNoSQLConfig{})
	if err := store.Put(ctx, "", map[string]any{"x": 1}); err == nil {
		t.Error("expected error for empty key")
	}
}

func TestMemoryNoSQL_Query(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryNoSQL("q-store", MemoryNoSQLConfig{})

	_ = store.Put(ctx, "user:alice", map[string]any{"role": "admin"})
	_ = store.Put(ctx, "user:bob", map[string]any{"role": "viewer"})
	_ = store.Put(ctx, "order:1", map[string]any{"total": 100})

	// Query all
	all, err := store.Query(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Query all failed: %v", err)
	}
	if len(all) != 3 {
		t.Errorf("expected 3 items, got %d", len(all))
	}

	// Query with prefix filter
	users, err := store.Query(ctx, map[string]any{"prefix": "user:"})
	if err != nil {
		t.Fatalf("Query with prefix failed: %v", err)
	}
	if len(users) != 2 {
		t.Errorf("expected 2 user items, got %d", len(users))
	}

	// Query items include _key field
	for _, u := range users {
		if _, ok := u["_key"]; !ok {
			t.Error("query result missing _key field")
		}
	}

	// Query empty result
	none, err := store.Query(ctx, map[string]any{"prefix": "product:"})
	if err != nil {
		t.Fatalf("Query no-match failed: %v", err)
	}
	if len(none) != 0 {
		t.Errorf("expected 0 items, got %d", len(none))
	}
}

func TestMemoryNoSQL_IsolatedCopies(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryNoSQL("iso-store", MemoryNoSQLConfig{})

	original := map[string]any{"x": 1}
	_ = store.Put(ctx, "k", original)

	// Mutate original after put — stored copy should be unaffected
	original["x"] = 99
	got, _ := store.Get(ctx, "k")
	if got["x"] != 1 {
		t.Errorf("stored item was mutated externally: got x=%v", got["x"])
	}

	// Mutate returned item — stored copy should be unaffected
	got["x"] = 42
	got2, _ := store.Get(ctx, "k")
	if got2["x"] != 1 {
		t.Errorf("returned item shares storage: got x=%v", got2["x"])
	}
}

func TestDynamoDBNoSQL_LocalMode(t *testing.T) {
	ctx := context.Background()
	m := NewDynamoDBNoSQL("dynamo-local", DynamoDBNoSQLConfig{
		TableName: "items",
		Endpoint:  "local",
	})
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := m.Put(ctx, "k1", map[string]any{"v": "hello"}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	item, err := m.Get(ctx, "k1")
	if err != nil || item == nil || item["v"] != "hello" {
		t.Errorf("Get after Put failed: item=%v err=%v", item, err)
	}
	if err := m.Delete(ctx, "k1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	gone, _ := m.Get(ctx, "k1")
	if gone != nil {
		t.Error("expected nil after delete")
	}

	// Query
	_ = m.Put(ctx, "a", map[string]any{"x": 1})
	_ = m.Put(ctx, "b", map[string]any{"x": 2})
	results, err := m.Query(ctx, map[string]any{})
	if err != nil {
		t.Fatalf("Query failed: %v", err)
	}
	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestMongoDBNoSQL_MemoryMode(t *testing.T) {
	ctx := context.Background()
	m := NewMongoDBNoSQL("mongo-mem", MongoDBNoSQLConfig{
		URI:        "memory://",
		Database:   "testdb",
		Collection: "docs",
	})
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := m.Put(ctx, "doc-1", map[string]any{"title": "Hello"}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	item, _ := m.Get(ctx, "doc-1")
	if item == nil || item["title"] != "Hello" {
		t.Errorf("Get returned unexpected item: %v", item)
	}
	results, _ := m.Query(ctx, map[string]any{})
	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
	if err := m.Delete(ctx, "doc-1"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
}

func TestRedisNoSQL_MemoryMode(t *testing.T) {
	ctx := context.Background()
	m := NewRedisNoSQL("redis-mem", RedisNoSQLConfig{
		Addr: "memory://",
	})
	if err := m.Init(nil); err != nil {
		t.Fatalf("Init failed: %v", err)
	}

	if err := m.Put(ctx, "session:abc", map[string]any{"user": "alice"}); err != nil {
		t.Fatalf("Put failed: %v", err)
	}
	item, _ := m.Get(ctx, "session:abc")
	if item == nil || item["user"] != "alice" {
		t.Errorf("Get returned unexpected: %v", item)
	}
	if err := m.Delete(ctx, "session:abc"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	gone, _ := m.Get(ctx, "session:abc")
	if gone != nil {
		t.Error("expected nil after delete")
	}
}

func TestNoSQLGetStep(t *testing.T) {
	ctx := context.Background()

	store := NewMemoryNoSQL("items", MemoryNoSQLConfig{})
	_ = store.Put(ctx, "item-42", map[string]any{"id": "item-42", "name": "Widget"})

	app := NewMockApplication()
	app.Services["items"] = NoSQLStore(store)

	factory := NewNoSQLGetStepFactory()
	step, err := factory("get-item", map[string]any{
		"store":  "items",
		"key":    "item-42",
		"output": "result",
	}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Output["found"] != true {
		t.Errorf("expected found=true, got %v", result.Output["found"])
	}
	item, ok := result.Output["result"].(map[string]any)
	if !ok || item["name"] != "Widget" {
		t.Errorf("unexpected result: %v", result.Output["result"])
	}
}

func TestNoSQLGetStep_Miss(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryNoSQL("items", MemoryNoSQLConfig{})
	app := NewMockApplication()
	app.Services["items"] = NoSQLStore(store)

	factory := NewNoSQLGetStepFactory()
	step, _ := factory("get-missing", map[string]any{
		"store":   "items",
		"key":     "no-such-key",
		"miss_ok": true,
	}, app)

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("miss_ok=true should not error: %v", err)
	}
	if result.Output["found"] != false {
		t.Errorf("expected found=false")
	}
}

func TestNoSQLPutStep(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryNoSQL("items", MemoryNoSQLConfig{})
	app := NewMockApplication()
	app.Services["items"] = NoSQLStore(store)

	factory := NewNoSQLPutStepFactory()
	step, err := factory("put-item", map[string]any{
		"store": "items",
		"key":   "item-99",
		"item":  "body",
	}, app)
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}

	pc := NewPipelineContext(map[string]any{
		"body": map[string]any{"name": "Gadget", "price": 29.99},
	}, nil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Output["stored"] != true {
		t.Error("expected stored=true")
	}

	// Verify stored in the actual store
	item, _ := store.Get(ctx, "item-99")
	if item == nil || item["name"] != "Gadget" {
		t.Errorf("stored item mismatch: %v", item)
	}
}

func TestNoSQLDeleteStep(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryNoSQL("items", MemoryNoSQLConfig{})
	_ = store.Put(ctx, "del-me", map[string]any{"x": 1})
	app := NewMockApplication()
	app.Services["items"] = NoSQLStore(store)

	factory := NewNoSQLDeleteStepFactory()
	step, _ := factory("del-step", map[string]any{
		"store": "items",
		"key":   "del-me",
	}, app)

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Output["deleted"] != true {
		t.Error("expected deleted=true")
	}
	gone, _ := store.Get(ctx, "del-me")
	if gone != nil {
		t.Error("item still exists after delete step")
	}
}

func TestNoSQLQueryStep(t *testing.T) {
	ctx := context.Background()
	store := NewMemoryNoSQL("items", MemoryNoSQLConfig{})
	_ = store.Put(ctx, "prod:1", map[string]any{"name": "A"})
	_ = store.Put(ctx, "prod:2", map[string]any{"name": "B"})
	_ = store.Put(ctx, "order:1", map[string]any{"total": 50})
	app := NewMockApplication()
	app.Services["items"] = NoSQLStore(store)

	factory := NewNoSQLQueryStepFactory()
	step, _ := factory("query-step", map[string]any{
		"store":  "items",
		"prefix": "prod:",
		"output": "products",
	}, app)

	pc := NewPipelineContext(map[string]any{}, nil)
	result, err := step.Execute(ctx, pc)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Output["count"] != 2 {
		t.Errorf("expected count=2, got %v", result.Output["count"])
	}
}

func TestNoSQLGetStep_StoreRequired(t *testing.T) {
	factory := NewNoSQLGetStepFactory()
	_, err := factory("bad", map[string]any{"key": "k"}, nil)
	if err == nil {
		t.Error("expected error when store is missing")
	}
}

func TestNoSQLGetStep_KeyRequired(t *testing.T) {
	factory := NewNoSQLGetStepFactory()
	_, err := factory("bad", map[string]any{"store": "s"}, nil)
	if err == nil {
		t.Error("expected error when key is missing")
	}
}
