package module

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func newTestPersistenceStore(t *testing.T) *PersistenceStore {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	ps := NewPersistenceStore("test-persistence", "database")
	ps.SetDB(db)

	if err := ps.migrate(); err != nil {
		t.Fatalf("migration failed: %v", err)
	}
	return ps
}

func TestPersistence_MigrationCreatesAllTables(t *testing.T) {
	ps := newTestPersistenceStore(t)

	tables := []string{"workflow_instances", "resources", "users"}
	for _, table := range tables {
		var name string
		err := ps.db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table).Scan(&name)
		if err != nil {
			t.Errorf("expected table %q to exist: %v", table, err)
		}
	}
}

func TestPersistence_WorkflowInstance_Roundtrip(t *testing.T) {
	ps := newTestPersistenceStore(t)

	now := time.Now().Truncate(time.Millisecond)
	instance := &WorkflowInstance{
		ID:            "wf-1",
		WorkflowType:  "order-workflow",
		CurrentState:  "processing",
		PreviousState: "new",
		Data:          map[string]interface{}{"customer": "alice"},
		StartTime:     now,
		LastUpdated:   now,
		Completed:     false,
		Error:         "",
	}

	if err := ps.SaveWorkflowInstance(instance); err != nil {
		t.Fatalf("SaveWorkflowInstance failed: %v", err)
	}

	loaded, err := ps.LoadWorkflowInstances("order-workflow")
	if err != nil {
		t.Fatalf("LoadWorkflowInstances failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(loaded))
	}

	got := loaded[0]
	if got.ID != "wf-1" {
		t.Errorf("expected ID 'wf-1', got '%s'", got.ID)
	}
	if got.WorkflowType != "order-workflow" {
		t.Errorf("expected type 'order-workflow', got '%s'", got.WorkflowType)
	}
	if got.CurrentState != "processing" {
		t.Errorf("expected state 'processing', got '%s'", got.CurrentState)
	}
	if got.PreviousState != "new" {
		t.Errorf("expected previous state 'new', got '%s'", got.PreviousState)
	}
	if got.Data["customer"] != "alice" {
		t.Errorf("expected data customer 'alice', got '%v'", got.Data["customer"])
	}
	if got.Completed {
		t.Error("expected Completed=false")
	}
}

func TestPersistence_WorkflowInstance_Upsert(t *testing.T) {
	ps := newTestPersistenceStore(t)

	now := time.Now().Truncate(time.Millisecond)
	instance := &WorkflowInstance{
		ID:           "wf-1",
		WorkflowType: "order-workflow",
		CurrentState: "new",
		Data:         map[string]interface{}{},
		StartTime:    now,
		LastUpdated:  now,
	}

	if err := ps.SaveWorkflowInstance(instance); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	// Update the instance
	instance.CurrentState = "processing"
	instance.PreviousState = "new"
	instance.LastUpdated = now.Add(time.Minute)

	if err := ps.SaveWorkflowInstance(instance); err != nil {
		t.Fatalf("second save (upsert) failed: %v", err)
	}

	loaded, err := ps.LoadWorkflowInstances("order-workflow")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 instance after upsert, got %d", len(loaded))
	}
	if loaded[0].CurrentState != "processing" {
		t.Errorf("expected updated state 'processing', got '%s'", loaded[0].CurrentState)
	}
}

func TestPersistence_Resource_Roundtrip(t *testing.T) {
	ps := newTestPersistenceStore(t)

	data := map[string]interface{}{
		"name":  "Widget",
		"price": float64(9.99),
		"state": "active",
	}

	if err := ps.SaveResource("products", "prod-1", data); err != nil {
		t.Fatalf("SaveResource failed: %v", err)
	}

	loaded, err := ps.LoadResources("products")
	if err != nil {
		t.Fatalf("LoadResources failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 resource, got %d", len(loaded))
	}

	got, ok := loaded["prod-1"]
	if !ok {
		t.Fatal("expected resource 'prod-1' to exist")
	}
	if got["name"] != "Widget" {
		t.Errorf("expected name 'Widget', got '%v'", got["name"])
	}
	if got["price"] != float64(9.99) {
		t.Errorf("expected price 9.99, got '%v'", got["price"])
	}
}

func TestPersistence_Resource_Upsert(t *testing.T) {
	ps := newTestPersistenceStore(t)

	if err := ps.SaveResource("products", "prod-1", map[string]interface{}{"name": "Old"}); err != nil {
		t.Fatalf("first save failed: %v", err)
	}
	if err := ps.SaveResource("products", "prod-1", map[string]interface{}{"name": "New"}); err != nil {
		t.Fatalf("second save failed: %v", err)
	}

	loaded, err := ps.LoadResources("products")
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 resource after upsert, got %d", len(loaded))
	}
	if loaded["prod-1"]["name"] != "New" {
		t.Errorf("expected updated name 'New', got '%v'", loaded["prod-1"]["name"])
	}
}

func TestPersistence_User_Roundtrip(t *testing.T) {
	ps := newTestPersistenceStore(t)

	user := UserRecord{
		ID:           "user-1",
		Email:        "alice@example.com",
		Name:         "Alice",
		PasswordHash: "$2a$10$fakehash",
		CreatedAt:    time.Now().Truncate(time.Millisecond),
	}

	if err := ps.SaveUser(user); err != nil {
		t.Fatalf("SaveUser failed: %v", err)
	}

	loaded, err := ps.LoadUsers()
	if err != nil {
		t.Fatalf("LoadUsers failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 user, got %d", len(loaded))
	}

	got := loaded[0]
	if got.ID != "user-1" {
		t.Errorf("expected ID 'user-1', got '%s'", got.ID)
	}
	if got.Email != "alice@example.com" {
		t.Errorf("expected email 'alice@example.com', got '%s'", got.Email)
	}
	if got.Name != "Alice" {
		t.Errorf("expected name 'Alice', got '%s'", got.Name)
	}
	if got.PasswordHash != "$2a$10$fakehash" {
		t.Errorf("expected password hash to match, got '%s'", got.PasswordHash)
	}
}

func TestPersistence_User_Upsert(t *testing.T) {
	ps := newTestPersistenceStore(t)

	user := UserRecord{
		ID:           "user-1",
		Email:        "alice@example.com",
		Name:         "Alice",
		PasswordHash: "$2a$10$hash1",
		CreatedAt:    time.Now().Truncate(time.Millisecond),
	}
	if err := ps.SaveUser(user); err != nil {
		t.Fatalf("first save failed: %v", err)
	}

	// Update
	user.Name = "Alice Updated"
	user.PasswordHash = "$2a$10$hash2"
	if err := ps.SaveUser(user); err != nil {
		t.Fatalf("second save failed: %v", err)
	}

	loaded, err := ps.LoadUsers()
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(loaded) != 1 {
		t.Fatalf("expected 1 user after upsert, got %d", len(loaded))
	}
	if loaded[0].Name != "Alice Updated" {
		t.Errorf("expected updated name 'Alice Updated', got '%s'", loaded[0].Name)
	}
}

func TestPersistence_EmptyDatabase(t *testing.T) {
	ps := newTestPersistenceStore(t)

	instances, err := ps.LoadWorkflowInstances("nonexistent")
	if err != nil {
		t.Fatalf("LoadWorkflowInstances on empty db failed: %v", err)
	}
	if len(instances) != 0 {
		t.Errorf("expected 0 instances, got %d", len(instances))
	}

	resources, err := ps.LoadResources("nonexistent")
	if err != nil {
		t.Fatalf("LoadResources on empty db failed: %v", err)
	}
	if len(resources) != 0 {
		t.Errorf("expected 0 resources, got %d", len(resources))
	}

	users, err := ps.LoadUsers()
	if err != nil {
		t.Fatalf("LoadUsers on empty db failed: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("expected 0 users, got %d", len(users))
	}
}

func TestPersistence_ModuleInterface(t *testing.T) {
	ps := NewPersistenceStore("test-ps", "my-db")

	if ps.Name() != "test-ps" {
		t.Errorf("expected name 'test-ps', got '%s'", ps.Name())
	}

	services := ps.ProvidesServices()
	if len(services) != 1 || services[0].Name != "test-ps" {
		t.Errorf("unexpected ProvidesServices: %v", services)
	}

	deps := ps.RequiresServices()
	if len(deps) != 1 || deps[0].Name != "my-db" {
		t.Errorf("unexpected RequiresServices: %v", deps)
	}
}
