package migration

import (
	"context"
	"database/sql"
	"log/slog"
	"testing"

	_ "modernc.org/sqlite"
)

// testProvider is a mock SchemaProvider for testing.
type testProvider struct {
	name    string
	version int
	ddl     string
	diffs   []SchemaDiff
}

func (p *testProvider) SchemaName() string        { return p.name }
func (p *testProvider) SchemaVersion() int        { return p.version }
func (p *testProvider) SchemaSQL() string         { return p.ddl }
func (p *testProvider) SchemaDiffs() []SchemaDiff { return p.diffs }

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func newTestRunner(t *testing.T, db *sql.DB) (*MigrationRunner, *SQLiteMigrationStore) {
	t.Helper()
	store, err := NewSQLiteMigrationStore(db)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	lock := NewSQLiteLock(db)
	runner := NewMigrationRunner(store, lock, slog.Default())
	return runner, store
}

func TestMigrationRunner_FullSchema(t *testing.T) {
	db := newTestDB(t)
	runner, store := newTestRunner(t, db)
	ctx := context.Background()

	provider := &testProvider{
		name:    "test_schema",
		version: 1,
		ddl:     `CREATE TABLE IF NOT EXISTS test_items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`,
	}

	if err := runner.Run(ctx, db, provider); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Verify table was created.
	_, err := db.ExecContext(ctx, `INSERT INTO test_items (id, name) VALUES (1, 'hello')`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	// Verify migration was recorded.
	applied, err := store.Applied(ctx, "test_schema")
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied, got %d", len(applied))
	}
	if applied[0].Version != 1 {
		t.Errorf("expected version 1, got %d", applied[0].Version)
	}
}

func TestMigrationRunner_Diffs(t *testing.T) {
	db := newTestDB(t)
	runner, store := newTestRunner(t, db)
	ctx := context.Background()

	provider := &testProvider{
		name:    "test_schema",
		version: 3,
		ddl:     `CREATE TABLE IF NOT EXISTS test_items (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT, age INTEGER);`,
		diffs: []SchemaDiff{
			{FromVersion: 0, ToVersion: 1, UpSQL: `CREATE TABLE IF NOT EXISTS test_items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`,
				DownSQL: `DROP TABLE IF EXISTS test_items;`},
			{FromVersion: 1, ToVersion: 2, UpSQL: `ALTER TABLE test_items ADD COLUMN email TEXT;`,
				DownSQL: `ALTER TABLE test_items DROP COLUMN email;`},
			{FromVersion: 2, ToVersion: 3, UpSQL: `ALTER TABLE test_items ADD COLUMN age INTEGER;`,
				DownSQL: `ALTER TABLE test_items DROP COLUMN age;`},
		},
	}

	if err := runner.Run(ctx, db, provider); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Verify all columns exist.
	_, err := db.ExecContext(ctx, `INSERT INTO test_items (id, name, email, age) VALUES (1, 'alice', 'alice@example.com', 30)`)
	if err != nil {
		t.Fatalf("insert: %v", err)
	}

	applied, err := store.Applied(ctx, "test_schema")
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if len(applied) != 3 {
		t.Fatalf("expected 3 applied, got %d", len(applied))
	}
}

func TestMigrationRunner_Idempotent(t *testing.T) {
	db := newTestDB(t)
	runner, store := newTestRunner(t, db)
	ctx := context.Background()

	provider := &testProvider{
		name:    "test_schema",
		version: 1,
		ddl:     `CREATE TABLE IF NOT EXISTS test_items (id INTEGER PRIMARY KEY);`,
	}

	// Run twice.
	if err := runner.Run(ctx, db, provider); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if err := runner.Run(ctx, db, provider); err != nil {
		t.Fatalf("second run: %v", err)
	}

	applied, err := store.Applied(ctx, "test_schema")
	if err != nil {
		t.Fatalf("applied: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied, got %d", len(applied))
	}
}

func TestMigrationRunner_IncrementalDiffs(t *testing.T) {
	db := newTestDB(t)
	runner, store := newTestRunner(t, db)
	ctx := context.Background()

	// First run: version 1.
	v1 := &testProvider{
		name:    "test_schema",
		version: 1,
		ddl:     `CREATE TABLE IF NOT EXISTS test_items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`,
		diffs: []SchemaDiff{
			{FromVersion: 0, ToVersion: 1, UpSQL: `CREATE TABLE IF NOT EXISTS test_items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`,
				DownSQL: `DROP TABLE IF EXISTS test_items;`},
		},
	}

	if err := runner.Run(ctx, db, v1); err != nil {
		t.Fatalf("run v1: %v", err)
	}

	applied, err := store.Applied(ctx, "test_schema")
	if err != nil {
		t.Fatalf("applied after v1: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("expected 1 applied after v1, got %d", len(applied))
	}

	// Second run: add version 2.
	v2 := &testProvider{
		name:    "test_schema",
		version: 2,
		ddl:     `CREATE TABLE IF NOT EXISTS test_items (id INTEGER PRIMARY KEY, name TEXT NOT NULL, email TEXT);`,
		diffs: []SchemaDiff{
			{FromVersion: 0, ToVersion: 1, UpSQL: `CREATE TABLE IF NOT EXISTS test_items (id INTEGER PRIMARY KEY, name TEXT NOT NULL);`,
				DownSQL: `DROP TABLE IF EXISTS test_items;`},
			{FromVersion: 1, ToVersion: 2, UpSQL: `ALTER TABLE test_items ADD COLUMN email TEXT;`,
				DownSQL: `ALTER TABLE test_items DROP COLUMN email;`},
		},
	}

	if err := runner.Run(ctx, db, v2); err != nil {
		t.Fatalf("run v2: %v", err)
	}

	applied, err = store.Applied(ctx, "test_schema")
	if err != nil {
		t.Fatalf("applied after v2: %v", err)
	}
	if len(applied) != 2 {
		t.Fatalf("expected 2 applied after v2, got %d", len(applied))
	}
}

func TestMigrationRunner_Pending(t *testing.T) {
	db := newTestDB(t)
	runner, store := newTestRunner(t, db)
	ctx := context.Background()

	provider := &testProvider{
		name:    "test_schema",
		version: 2,
		diffs: []SchemaDiff{
			{FromVersion: 0, ToVersion: 1, UpSQL: `CREATE TABLE test_items (id INTEGER);`, DownSQL: `DROP TABLE test_items;`},
			{FromVersion: 1, ToVersion: 2, UpSQL: `ALTER TABLE test_items ADD COLUMN name TEXT;`, DownSQL: `ALTER TABLE test_items DROP COLUMN name;`},
		},
	}

	// All pending.
	pending, err := runner.Pending(ctx, provider)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected 2 pending, got %d", len(pending))
	}

	// Record version 1 as applied.
	if err := store.Record(ctx, "test_schema", 1, "abc"); err != nil {
		t.Fatalf("record: %v", err)
	}

	// Now only version 2 should be pending.
	pending, err = runner.Pending(ctx, provider)
	if err != nil {
		t.Fatalf("pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending, got %d", len(pending))
	}
	if pending[0].ToVersion != 2 {
		t.Errorf("expected pending version 2, got %d", pending[0].ToVersion)
	}
}

func TestMigrationRunner_Status(t *testing.T) {
	db := newTestDB(t)
	runner, store := newTestRunner(t, db)
	ctx := context.Background()

	if err := store.Record(ctx, "schema_a", 1, "aaa"); err != nil {
		t.Fatalf("record: %v", err)
	}
	if err := store.Record(ctx, "schema_a", 2, "bbb"); err != nil {
		t.Fatalf("record: %v", err)
	}

	providerA := &testProvider{name: "schema_a", version: 2}
	providerB := &testProvider{name: "schema_b", version: 1}

	status, err := runner.Status(ctx, providerA, providerB)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	if len(status["schema_a"]) != 2 {
		t.Errorf("expected 2 applied for schema_a, got %d", len(status["schema_a"]))
	}
	if len(status["schema_b"]) != 0 {
		t.Errorf("expected 0 applied for schema_b, got %d", len(status["schema_b"]))
	}
}

func TestMigrationRunner_MultipleProviders(t *testing.T) {
	db := newTestDB(t)
	runner, _ := newTestRunner(t, db)
	ctx := context.Background()

	providerA := &testProvider{
		name:    "schema_a",
		version: 1,
		ddl:     `CREATE TABLE IF NOT EXISTS table_a (id INTEGER PRIMARY KEY);`,
	}
	providerB := &testProvider{
		name:    "schema_b",
		version: 1,
		ddl:     `CREATE TABLE IF NOT EXISTS table_b (id INTEGER PRIMARY KEY);`,
	}

	if err := runner.Run(ctx, db, providerA, providerB); err != nil {
		t.Fatalf("run: %v", err)
	}

	// Both tables should exist.
	_, err := db.ExecContext(ctx, `INSERT INTO table_a (id) VALUES (1)`)
	if err != nil {
		t.Fatalf("insert table_a: %v", err)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO table_b (id) VALUES (1)`)
	if err != nil {
		t.Fatalf("insert table_b: %v", err)
	}
}

func TestChecksumSQL(t *testing.T) {
	s1 := checksumSQL("CREATE TABLE foo (id INT);")
	s2 := checksumSQL("CREATE TABLE foo (id INT);")
	s3 := checksumSQL("CREATE TABLE bar (id INT);")

	if s1 != s2 {
		t.Errorf("identical SQL should have same checksum")
	}
	if s1 == s3 {
		t.Errorf("different SQL should have different checksum")
	}
	if len(s1) != 16 {
		t.Errorf("expected 16 char hex, got %d", len(s1))
	}
}
