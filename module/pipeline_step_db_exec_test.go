package module

import (
	"context"
	"database/sql"
	"testing"

	_ "modernc.org/sqlite"
)

func TestDBExecStep_Insert(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	app := mockAppWithDB("test-db", db)
	factory := NewDBExecStepFactory()
	step, err := factory("insert-item", map[string]any{
		"database": "test-db",
		"query":    "INSERT INTO items (id, name) VALUES (?, ?)",
		"params":   []any{"item-1", "Widget"},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	affected, _ := result.Output["affected_rows"].(int64)
	if affected != 1 {
		t.Errorf("expected affected_rows=1, got %v", result.Output["affected_rows"])
	}

	// Verify the insert
	var name string
	err = db.QueryRow("SELECT name FROM items WHERE id = ?", "item-1").Scan(&name)
	if err != nil {
		t.Fatalf("verify select: %v", err)
	}
	if name != "Widget" {
		t.Errorf("expected name='Widget', got %q", name)
	}
}

func TestDBExecStep_Update(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT NOT NULL);
		INSERT INTO items (id, name) VALUES ('i1', 'Old Name');
	`)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	app := mockAppWithDB("test-db", db)
	factory := NewDBExecStepFactory()
	step, err := factory("update-item", map[string]any{
		"database": "test-db",
		"query":    "UPDATE items SET name = ? WHERE id = ?",
		"params":   []any{"New Name", "i1"},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	affected, _ := result.Output["affected_rows"].(int64)
	if affected != 1 {
		t.Errorf("expected affected_rows=1, got %v", result.Output["affected_rows"])
	}
}

func TestDBExecStep_Delete(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT NOT NULL);
		INSERT INTO items (id, name) VALUES ('i1', 'To Delete');
		INSERT INTO items (id, name) VALUES ('i2', 'Keep');
	`)
	if err != nil {
		t.Fatalf("setup: %v", err)
	}

	app := mockAppWithDB("test-db", db)
	factory := NewDBExecStepFactory()
	step, err := factory("delete-item", map[string]any{
		"database": "test-db",
		"query":    "DELETE FROM items WHERE id = ?",
		"params":   []any{"i1"},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	affected, _ := result.Output["affected_rows"].(int64)
	if affected != 1 {
		t.Errorf("expected affected_rows=1, got %v", result.Output["affected_rows"])
	}

	// Verify deletion
	var count int
	db.QueryRow("SELECT COUNT(*) FROM items").Scan(&count)
	if count != 1 {
		t.Errorf("expected 1 remaining item, got %d", count)
	}
}

func TestDBExecStep_TemplateParams(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE items (id TEXT PRIMARY KEY, name TEXT NOT NULL)`)
	if err != nil {
		t.Fatalf("create table: %v", err)
	}

	app := mockAppWithDB("test-db", db)
	factory := NewDBExecStepFactory()
	step, err := factory("insert-templated", map[string]any{
		"database": "test-db",
		"query":    "INSERT INTO items (id, name) VALUES (?, ?)",
		"params":   []any{"{{ .steps.prepare.id }}", "{{index .steps \"parse-request\" \"body\" \"name\"}}"},
	}, app)
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	pc := NewPipelineContext(nil, nil)
	pc.MergeStepOutput("prepare", map[string]any{"id": "gen-123"})
	pc.MergeStepOutput("parse-request", map[string]any{
		"body": map[string]any{"name": "New Item"},
	})

	result, err := step.Execute(context.Background(), pc)
	if err != nil {
		t.Fatalf("execute error: %v", err)
	}

	affected, _ := result.Output["affected_rows"].(int64)
	if affected != 1 {
		t.Errorf("expected affected_rows=1, got %v", result.Output["affected_rows"])
	}

	// Verify insert used resolved params
	var name string
	db.QueryRow("SELECT name FROM items WHERE id = ?", "gen-123").Scan(&name)
	if name != "New Item" {
		t.Errorf("expected name='New Item', got %q", name)
	}
}

func TestDBExecStep_RejectsTemplateInQuery(t *testing.T) {
	factory := NewDBExecStepFactory()
	_, err := factory("bad-exec", map[string]any{
		"database": "test-db",
		"query":    "DELETE FROM items WHERE id = '{{ .id }}'",
	}, nil)
	if err == nil {
		t.Fatal("expected error for template in query")
	}
}

func TestDBExecStep_MissingDatabase(t *testing.T) {
	factory := NewDBExecStepFactory()
	_, err := factory("no-db", map[string]any{
		"query": "INSERT INTO x VALUES (?)",
	}, nil)
	if err == nil {
		t.Fatal("expected error for missing database")
	}
}
