package module

import (
	"database/sql"
	"io/fs"
	"os"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// TestSQLTenantRegistry_Interface verifies compile-time conformance.
func TestSQLTenantRegistry_Interface(t *testing.T) {
	var _ interfaces.TenantRegistry = (*SQLTenantRegistry)(nil)
	var _ interfaces.MigrationProvider = (*SQLTenantRegistry)(nil)
}

// TestSQLTenantRegistry_SchemaConfig tests the schema config helpers.
func TestSQLTenantRegistry_SchemaConfig(t *testing.T) {
	cfg := TenantSchemaConfig{TableName: "my_tenants", ColID: "tenant_id"}
	r := &SQLTenantRegistry{schema: cfg}

	if r.schema.table() != "my_tenants" {
		t.Errorf("expected 'my_tenants', got %q", r.schema.table())
	}
	if r.c(cfg.ColID, "id") != "tenant_id" {
		t.Errorf("expected 'tenant_id', got %q", r.c(cfg.ColID, "id"))
	}
	// Default column when override is empty.
	if r.c("", "slug") != "slug" {
		t.Errorf("expected 'slug', got %q", r.c("", "slug"))
	}
}

// TestSQLTenantRegistry_DefaultSchema tests that zero-value config returns defaults.
func TestSQLTenantRegistry_DefaultSchema(t *testing.T) {
	r := &SQLTenantRegistry{}
	if r.schema.table() != "tenants" {
		t.Errorf("expected default 'tenants', got %q", r.schema.table())
	}
}

// TestTenantsMigrationsFS verifies the embedded migrations filesystem.
func TestTenantsMigrationsFS(t *testing.T) {
	mfs, err := TenantsMigrationsFS()
	if err != nil {
		t.Fatalf("TenantsMigrationsFS error: %v", err)
	}
	entries, err := fs.ReadDir(mfs, ".")
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) == 0 {
		t.Error("expected at least one migration file")
	}
	// Check both up and down files exist.
	fileNames := make(map[string]bool)
	for _, e := range entries {
		fileNames[e.Name()] = true
	}
	if !fileNames["20260422000001_tenants.up.sql"] {
		t.Error("missing up migration file")
	}
	if !fileNames["20260422000001_tenants.down.sql"] {
		t.Error("missing down migration file")
	}
}

// TestSQLTenantRegistry_MigrationsDependencies verifies the tenant registry
// has no migration dependencies.
func TestSQLTenantRegistry_MigrationsDependencies(t *testing.T) {
	r := &SQLTenantRegistry{}
	deps := r.MigrationsDependencies()
	if len(deps) != 0 {
		t.Errorf("expected no deps, got %v", deps)
	}
}

// TestSQLTenantRegistry_Integration runs real SQL against a Postgres database.
// Skipped unless POSTGRES_TEST_URL is set.
func TestSQLTenantRegistry_Integration(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_URL")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_URL not set; skipping integration test")
	}

	reg, db := newTestSQLRegistry(t, dsn)
	defer db.Close()

	// Ensure creates a new tenant.
	spec := interfaces.TenantSpec{Name: "Acme Corp", Slug: "acme", Domains: []string{"acme.example.com"}}
	tenant, err := reg.Ensure(spec)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if tenant.IsZero() {
		t.Error("Ensure returned zero tenant")
	}
	if tenant.Slug != "acme" {
		t.Errorf("slug: got %q, want acme", tenant.Slug)
	}

	// Ensure again → same tenant.
	tenant2, err := reg.Ensure(spec)
	if err != nil {
		t.Fatalf("Ensure idempotent: %v", err)
	}
	if tenant.ID != tenant2.ID {
		t.Errorf("Ensure should return same tenant on second call")
	}

	// GetBySlug.
	got, err := reg.GetBySlug("acme")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ID != tenant.ID {
		t.Error("GetBySlug returned wrong tenant")
	}

	// GetByID.
	got2, err := reg.GetByID(tenant.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got2.Name != "Acme Corp" {
		t.Errorf("GetByID name: got %q, want 'Acme Corp'", got2.Name)
	}

	// GetByDomain.
	got3, err := reg.GetByDomain("acme.example.com")
	if err != nil {
		t.Fatalf("GetByDomain: %v", err)
	}
	if got3.ID != tenant.ID {
		t.Error("GetByDomain returned wrong tenant")
	}

	// Update name.
	newName := "Acme Corporation"
	updated, err := reg.Update(tenant.ID, interfaces.TenantPatch{Name: &newName})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if updated.Name != newName {
		t.Errorf("Update name: got %q, want %q", updated.Name, newName)
	}

	// List active.
	all, err := reg.List(interfaces.TenantFilter{ActiveOnly: true})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(all) == 0 {
		t.Error("List should return at least the created tenant")
	}

	// Disable.
	if err := reg.Disable(tenant.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}
	// After disable, GetByID still works but is_active = false.
	disabled, err := reg.GetByID(tenant.ID)
	if err != nil {
		t.Fatalf("GetByID after disable: %v", err)
	}
	if disabled.IsActive {
		t.Error("expected is_active=false after Disable")
	}
}

// newTestSQLRegistry creates a SQLTenantRegistry for integration tests and creates
// the tenants table.
func newTestSQLRegistry(t *testing.T, dsn string) (*SQLTenantRegistry, interface{ Close() error }) {
	t.Helper()
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("sql.Open: %v", err)
	}
	if err := db.Ping(); err != nil {
		t.Fatalf("db.Ping: %v", err)
	}
	// Create the tenants table for this test run.
	upSQL, _ := fs.ReadFile(tenantsMigrationsFS, "tenants_migrations/20260422000001_tenants.up.sql")
	// Strip goose annotations for raw exec.
	query := stripGooseAnnotations(string(upSQL))
	if _, err := db.Exec(query); err != nil {
		t.Fatalf("create tenants table: %v", err)
	}
	t.Cleanup(func() {
		downSQL, _ := fs.ReadFile(tenantsMigrationsFS, "tenants_migrations/20260422000001_tenants.down.sql")
		query := stripGooseAnnotations(string(downSQL))
		_, _ = db.Exec(query)
	})

	reg, err := NewSQLTenantRegistry(SQLTenantRegistryConfig{DB: db})
	if err != nil {
		t.Fatalf("NewSQLTenantRegistry: %v", err)
	}
	return reg, db
}

// stripGooseAnnotations removes goose comment directives so SQL can be executed directly.
func stripGooseAnnotations(sql string) string {
	var lines []string
	for _, line := range splitLines(sql) {
		if len(line) > 0 && line[0] == '-' && containsGoose(line) {
			continue
		}
		lines = append(lines, line)
	}
	return joinLines(lines)
}

func splitLines(s string) []string {
	return splitByNewline(s)
}
func joinLines(lines []string) string {
	result := ""
	for _, l := range lines {
		result += l + "\n"
	}
	return result
}
func splitByNewline(s string) []string {
	var out []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
func containsGoose(s string) bool {
	return len(s) > 8 && s[3:8] == "goose"
}
