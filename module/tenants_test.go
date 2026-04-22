package module

import (
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
	_ "github.com/jackc/pgx/v5/stdlib"
)

// ---------------------------------------------------------------------------
// Minimal database/sql/driver mock for unit tests that need a *sql.DB but
// do not have access to a real PostgreSQL instance.  Tests that use the mock
// push pre-programmed responses via tenantMockPushQuery / tenantMockPushExec
// before exercising the registry methods.
// ---------------------------------------------------------------------------

func init() {
	sql.Register("workflow_tenant_mock", &tenantMockDriver{})
}

// tenantQueryResp is a pre-programmed response for a Query (SELECT / INSERT RETURNING / UPDATE RETURNING).
type tenantQueryResp struct {
	err  error            // non-nil → returned as query error (surfaces via row.Scan)
	cols []string         // column names
	rows [][]driver.Value // nil or empty → io.EOF → sql.ErrNoRows
}

// tenantExecResp is a pre-programmed response for an Exec (UPDATE / DELETE without RETURNING).
type tenantExecResp struct {
	err error
	n   int64 // RowsAffected
}

var (
	tenantMockMu         sync.Mutex
	tenantMockQueryQueue []*tenantQueryResp
	tenantMockExecQueue  []*tenantExecResp
)

func tenantMockPushQuery(r *tenantQueryResp) {
	tenantMockMu.Lock()
	tenantMockQueryQueue = append(tenantMockQueryQueue, r)
	tenantMockMu.Unlock()
}

func tenantMockPushExec(r *tenantExecResp) {
	tenantMockMu.Lock()
	tenantMockExecQueue = append(tenantMockExecQueue, r)
	tenantMockMu.Unlock()
}

func tenantMockPopQuery() *tenantQueryResp {
	tenantMockMu.Lock()
	defer tenantMockMu.Unlock()
	if len(tenantMockQueryQueue) == 0 {
		return &tenantQueryResp{} // empty rows → sql.ErrNoRows
	}
	r := tenantMockQueryQueue[0]
	tenantMockQueryQueue = tenantMockQueryQueue[1:]
	return r
}

func tenantMockPopExec() *tenantExecResp {
	tenantMockMu.Lock()
	defer tenantMockMu.Unlock()
	if len(tenantMockExecQueue) == 0 {
		return &tenantExecResp{n: 0}
	}
	r := tenantMockExecQueue[0]
	tenantMockExecQueue = tenantMockExecQueue[1:]
	return r
}

func tenantMockClear() {
	tenantMockMu.Lock()
	tenantMockQueryQueue = nil
	tenantMockExecQueue = nil
	tenantMockMu.Unlock()
}

// openMockDB opens a *sql.DB backed by the mock driver and registers cleanup.
func openMockDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("workflow_tenant_mock", "")
	if err != nil {
		t.Fatalf("sql.Open mock: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// tenantColNames are the columns that scanTenant expects (must match selectSQL order).
var tenantColNames = []string{"id", "slug", "name", "domains", "metadata", "is_active"}

// tenantDriverRow builds a driver.Value slice for scanTenant.
func tenantDriverRow(ten interfaces.Tenant) []driver.Value {
	domains, _ := json.Marshal(ten.Domains)
	metadata, _ := json.Marshal(ten.Metadata)
	if ten.Metadata == nil {
		metadata = []byte("{}")
	}
	if ten.Domains == nil {
		domains = []byte("[]")
	}
	return []driver.Value{ten.ID, ten.Slug, ten.Name, domains, metadata, ten.IsActive}
}

// --- driver.Driver ---

type tenantMockDriver struct{}

func (d *tenantMockDriver) Open(string) (driver.Conn, error) { return &tenantMockConn{}, nil }

// --- driver.Conn ---

type tenantMockConn struct{}

func (c *tenantMockConn) Prepare(query string) (driver.Stmt, error) {
	return &tenantMockStmt{}, nil
}
func (c *tenantMockConn) Close() error              { return nil }
func (c *tenantMockConn) Begin() (driver.Tx, error) { return &tenantMockTx{}, nil }

// --- driver.Tx ---

type tenantMockTx struct{}

func (t *tenantMockTx) Commit() error   { return nil }
func (t *tenantMockTx) Rollback() error { return nil }

// --- driver.Stmt ---

type tenantMockStmt struct{}

func (s *tenantMockStmt) Close() error  { return nil }
func (s *tenantMockStmt) NumInput() int { return -1 } // variadic

func (s *tenantMockStmt) Query(args []driver.Value) (driver.Rows, error) {
	r := tenantMockPopQuery()
	if r.err != nil {
		return nil, r.err
	}
	return &tenantMockRows{cols: r.cols, data: r.rows}, nil
}

func (s *tenantMockStmt) Exec(args []driver.Value) (driver.Result, error) {
	r := tenantMockPopExec()
	if r.err != nil {
		return nil, r.err
	}
	return &tenantMockExecResult{n: r.n}, nil
}

// --- driver.Rows ---

type tenantMockRows struct {
	cols []string
	data [][]driver.Value
	pos  int
}

func (r *tenantMockRows) Columns() []string { return r.cols }
func (r *tenantMockRows) Close() error      { return nil }
func (r *tenantMockRows) Next(dest []driver.Value) error {
	if r.pos >= len(r.data) {
		return io.EOF
	}
	copy(dest, r.data[r.pos])
	r.pos++
	return nil
}

// --- driver.Result ---

type tenantMockExecResult struct{ n int64 }

func (r *tenantMockExecResult) LastInsertId() (int64, error) { return 0, nil }
func (r *tenantMockExecResult) RowsAffected() (int64, error) { return r.n, nil }

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
	var sb strings.Builder
	for _, l := range lines {
		sb.WriteString(l)
		sb.WriteByte('\n')
	}
	return sb.String()
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
	return strings.Contains(s, "+goose")
}

// ---------------------------------------------------------------------------
// Unit tests using the mock driver
// ---------------------------------------------------------------------------

// TestNewSQLTenantRegistry_NilDB asserts that NewSQLTenantRegistry returns a
// non-nil error containing "cfg.DB is required" when DB is nil.
func TestNewSQLTenantRegistry_NilDB(t *testing.T) {
	_, err := NewSQLTenantRegistry(SQLTenantRegistryConfig{})
	if err == nil {
		t.Fatal("expected error for nil DB")
	}
	if !strings.Contains(err.Error(), "cfg.DB is required") {
		t.Errorf("error should mention 'cfg.DB is required', got: %v", err)
	}
}

// TestNewSQLTenantRegistry_CacheDisabled verifies that CacheSize=-1 produces a
// registry with a nil cache, and that all cache-adjacent code paths (Ensure →
// GetBySlug → Disable → GetBySlug) do not panic when the cache is absent.
func TestNewSQLTenantRegistry_CacheDisabled(t *testing.T) {
	tenantMockClear()

	ten := interfaces.Tenant{
		ID:       "t-nocache-1",
		Slug:     "nocache",
		Name:     "No Cache Test",
		Domains:  []string{"nocache.example.com"},
		IsActive: true,
	}
	disabledTen := ten
	disabledTen.IsActive = false

	// Ensure → GetBySlug (empty, not found).
	tenantMockPushQuery(&tenantQueryResp{cols: tenantColNames, rows: nil})
	// Ensure → INSERT RETURNING.
	tenantMockPushQuery(&tenantQueryResp{cols: tenantColNames, rows: [][]driver.Value{tenantDriverRow(ten)}})
	// GetBySlug.
	tenantMockPushQuery(&tenantQueryResp{cols: tenantColNames, rows: [][]driver.Value{tenantDriverRow(ten)}})
	// Disable → GetByID.
	tenantMockPushQuery(&tenantQueryResp{cols: tenantColNames, rows: [][]driver.Value{tenantDriverRow(ten)}})
	// Disable → ExecContext (UPDATE SET is_active=FALSE).
	tenantMockPushExec(&tenantExecResp{n: 1})
	// GetBySlug after disable.
	tenantMockPushQuery(&tenantQueryResp{cols: tenantColNames, rows: [][]driver.Value{tenantDriverRow(disabledTen)}})

	db := openMockDB(t)
	reg, err := NewSQLTenantRegistry(SQLTenantRegistryConfig{DB: db, CacheSize: -1})
	if err != nil {
		t.Fatalf("NewSQLTenantRegistry: %v", err)
	}

	// Cache must be nil for CacheSize=-1.
	if reg.cache != nil {
		t.Error("expected nil cache for CacheSize=-1")
	}

	// Exercise the sequence — must not panic with nil cache.
	spec := interfaces.TenantSpec{Name: ten.Name, Slug: ten.Slug, Domains: ten.Domains}
	got, err := reg.Ensure(spec)
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got.ID != ten.ID {
		t.Errorf("Ensure: unexpected ID %q", got.ID)
	}

	got2, err := reg.GetBySlug(ten.Slug)
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got2.Slug != ten.Slug {
		t.Errorf("GetBySlug: unexpected slug %q", got2.Slug)
	}

	if err := reg.Disable(got.ID); err != nil {
		t.Fatalf("Disable: %v", err)
	}

	got3, err := reg.GetBySlug(ten.Slug)
	if err != nil {
		t.Fatalf("GetBySlug after Disable: %v", err)
	}
	if got3.IsActive {
		t.Error("expected is_active=false after Disable")
	}
}

// TestEnsure_PropagatesNonNotFoundError verifies that a transient DB error
// (neither sql.ErrNoRows nor ErrResourceNotFound) is propagated by Ensure
// rather than treated as a "not found" and triggering an INSERT.
func TestEnsure_PropagatesNonNotFoundError(t *testing.T) {
	tenantMockClear()

	transientErr := errors.New("transient: connection reset by peer")

	// GetBySlug (inside Ensure) returns a transient error.
	tenantMockPushQuery(&tenantQueryResp{err: transientErr})

	db := openMockDB(t)
	reg, err := NewSQLTenantRegistry(SQLTenantRegistryConfig{DB: db})
	if err != nil {
		t.Fatalf("NewSQLTenantRegistry: %v", err)
	}

	spec := interfaces.TenantSpec{Name: "Acme", Slug: "acme"}
	_, err = reg.Ensure(spec)
	if err == nil {
		t.Fatal("expected error from Ensure on transient DB error")
	}
	if !errors.Is(err, transientErr) {
		t.Errorf("Ensure error should wrap transient error via %%w; got: %v", err)
	}
}

// TestUpdate_InvalidatesStaleDomainCache verifies that after calling Update with
// changed domains, the stale "old.example.com" domain cache entry is evicted so
// that a subsequent GetByDomain goes to the DB (not a stale cache hit).
func TestUpdate_InvalidatesStaleDomainCache(t *testing.T) {
	tenantMockClear()

	oldTen := interfaces.Tenant{
		ID:       "t-cache-1",
		Slug:     "acme-cache",
		Name:     "Acme Cache Test",
		Domains:  []string{"old.example.com"},
		IsActive: true,
	}
	updatedTen := interfaces.Tenant{
		ID:       "t-cache-1",
		Slug:     "acme-cache",
		Name:     "Acme Cache Test",
		Domains:  []string{"new.example.com"},
		IsActive: true,
	}

	// GetByDomain("old.example.com") → old tenant.
	tenantMockPushQuery(&tenantQueryResp{cols: tenantColNames, rows: [][]driver.Value{tenantDriverRow(oldTen)}})
	// Update → GetByID hits cache (no extra mock needed) → UPDATE RETURNING.
	tenantMockPushQuery(&tenantQueryResp{cols: tenantColNames, rows: [][]driver.Value{tenantDriverRow(updatedTen)}})
	// GetByDomain("old.example.com") after update → empty (not found).
	tenantMockPushQuery(&tenantQueryResp{cols: tenantColNames, rows: nil})

	db := openMockDB(t)
	reg, err := NewSQLTenantRegistry(SQLTenantRegistryConfig{DB: db, CacheSize: 32})
	if err != nil {
		t.Fatalf("NewSQLTenantRegistry: %v", err)
	}

	// Prime cache via GetByDomain.
	got, err := reg.GetByDomain("old.example.com")
	if err != nil {
		t.Fatalf("GetByDomain: %v", err)
	}
	if got.ID != oldTen.ID {
		t.Fatalf("GetByDomain returned wrong tenant: %+v", got)
	}

	// GetByDomain also caches "id:t-cache-1"; Update.GetByID will hit cache.
	newDomains := []string{"new.example.com"}
	updated, err := reg.Update(oldTen.ID, interfaces.TenantPatch{Domains: newDomains})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(updated.Domains) == 0 || updated.Domains[0] != "new.example.com" {
		t.Errorf("Update returned wrong domains: %v", updated.Domains)
	}

	// Old domain must now be a cache miss → DB path → ErrResourceNotFound.
	_, err = reg.GetByDomain("old.example.com")
	if !errors.Is(err, interfaces.ErrResourceNotFound) {
		t.Errorf("GetByDomain after Update: want ErrResourceNotFound, got %v", err)
	}
}
