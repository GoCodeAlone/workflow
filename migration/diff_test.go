package migration

import (
	"strings"
	"testing"
)

func TestDiffSchemas_AddColumn(t *testing.T) {
	oldDDL := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`
	newDDL := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT);`

	upSQL, downSQL := DiffSchemas(oldDDL, newDDL)

	if !strings.Contains(upSQL, "ADD COLUMN email") {
		t.Errorf("up should add email column, got: %s", upSQL)
	}
	if !strings.Contains(downSQL, "DROP COLUMN email") {
		t.Errorf("down should drop email column, got: %s", downSQL)
	}
}

func TestDiffSchemas_DropColumn(t *testing.T) {
	oldDDL := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT);`
	newDDL := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`

	upSQL, downSQL := DiffSchemas(oldDDL, newDDL)

	if !strings.Contains(upSQL, "DROP COLUMN email") {
		t.Errorf("up should drop email column, got: %s", upSQL)
	}
	if !strings.Contains(downSQL, "ADD COLUMN email") {
		t.Errorf("down should add email column, got: %s", downSQL)
	}
}

func TestDiffSchemas_AddTable(t *testing.T) {
	oldDDL := `CREATE TABLE users (id INTEGER PRIMARY KEY);`
	newDDL := `CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE posts (id INTEGER PRIMARY KEY, user_id INTEGER);`

	upSQL, downSQL := DiffSchemas(oldDDL, newDDL)

	if !strings.Contains(upSQL, "CREATE TABLE") && !strings.Contains(upSQL, "posts") {
		t.Errorf("up should create posts table, got: %s", upSQL)
	}
	if !strings.Contains(downSQL, "DROP TABLE") {
		t.Errorf("down should drop posts table, got: %s", downSQL)
	}
}

func TestDiffSchemas_DropTable(t *testing.T) {
	oldDDL := `CREATE TABLE users (id INTEGER PRIMARY KEY);
CREATE TABLE posts (id INTEGER PRIMARY KEY);`
	newDDL := `CREATE TABLE users (id INTEGER PRIMARY KEY);`

	upSQL, downSQL := DiffSchemas(oldDDL, newDDL)

	if !strings.Contains(upSQL, "DROP TABLE IF EXISTS posts") {
		t.Errorf("up should drop posts table, got: %s", upSQL)
	}
	if !strings.Contains(downSQL, "CREATE TABLE") {
		t.Errorf("down should recreate posts table, got: %s", downSQL)
	}
}

func TestDiffSchemas_AddIndex(t *testing.T) {
	oldDDL := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`
	newDDL := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);
CREATE INDEX idx_users_name ON users (name);`

	upSQL, downSQL := DiffSchemas(oldDDL, newDDL)

	if !strings.Contains(upSQL, "CREATE INDEX") {
		t.Errorf("up should create index, got: %s", upSQL)
	}
	if !strings.Contains(downSQL, "DROP INDEX") {
		t.Errorf("down should drop index, got: %s", downSQL)
	}
}

func TestDiffSchemas_NoChange(t *testing.T) {
	ddl := `CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT);`

	upSQL, downSQL := DiffSchemas(ddl, ddl)

	if upSQL != "" {
		t.Errorf("expected empty up, got: %s", upSQL)
	}
	if downSQL != "" {
		t.Errorf("expected empty down, got: %s", downSQL)
	}
}

func TestParseTables(t *testing.T) {
	ddl := `CREATE TABLE users (
    id INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT
);
CREATE TABLE posts (
    id INTEGER PRIMARY KEY,
    title TEXT
);`

	tables := parseTables(ddl)
	if len(tables) != 2 {
		t.Fatalf("expected 2 tables, got %d", len(tables))
	}

	users, ok := tables["users"]
	if !ok {
		t.Fatal("missing users table")
	}
	if len(users.Columns) != 3 {
		t.Errorf("expected 3 columns in users, got %d", len(users.Columns))
	}
}

func TestParseIndexes(t *testing.T) {
	ddl := `CREATE INDEX idx_users_name ON users (name);
CREATE UNIQUE INDEX idx_users_email ON users (email);`

	indexes := parseIndexes(ddl)
	if len(indexes) != 2 {
		t.Fatalf("expected 2 indexes, got %d", len(indexes))
	}
	if _, ok := indexes["idx_users_name"]; !ok {
		t.Error("missing idx_users_name")
	}
	if _, ok := indexes["idx_users_email"]; !ok {
		t.Error("missing idx_users_email")
	}
}
