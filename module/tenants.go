package module

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"time"

	lru "github.com/hashicorp/golang-lru"

	"github.com/GoCodeAlone/workflow/interfaces"
)

// TenantSchemaConfig allows operators to customise the underlying table name
// and column names, supporting legacy schemas or multi-schema deployments.
type TenantSchemaConfig struct {
	TableName    string // default: "tenants"
	ColID        string // default: "id"
	ColSlug      string // default: "slug"
	ColName      string // default: "name"
	ColDomains   string // default: "domains"
	ColMetadata  string // default: "metadata"
	ColIsActive  string // default: "is_active"
	ColCreatedAt string // default: "created_at"
	ColUpdatedAt string // default: "updated_at"
}

func (c *TenantSchemaConfig) table() string {
	if c.TableName != "" {
		return c.TableName
	}
	return "tenants"
}
func (c *TenantSchemaConfig) col(override, def string) string {
	if override != "" {
		return override
	}
	return def
}

// SQLTenantRegistry is a PostgreSQL-backed implementation of interfaces.TenantRegistry.
// It maintains a bounded LRU cache in front of the database to reduce hot-path latency.
type SQLTenantRegistry struct {
	db       *sql.DB
	schema   TenantSchemaConfig
	cache    *lru.Cache
	cacheTTL time.Duration
}

// SQLTenantRegistryConfig is the constructor configuration.
type SQLTenantRegistryConfig struct {
	DB        *sql.DB
	Schema    TenantSchemaConfig
	CacheSize int           // 0 = disable cache; default 256
	CacheTTL  time.Duration // 0 = use default 60s
}

// NewSQLTenantRegistry creates a new SQLTenantRegistry.
func NewSQLTenantRegistry(cfg SQLTenantRegistryConfig) (*SQLTenantRegistry, error) {
	size := cfg.CacheSize
	if size == 0 {
		size = 256
	}
	ttl := cfg.CacheTTL
	if ttl == 0 {
		ttl = 60 * time.Second
	}
	cache, err := lru.New(size)
	if err != nil {
		return nil, fmt.Errorf("create lru cache: %w", err)
	}
	return &SQLTenantRegistry{
		db:       cfg.DB,
		schema:   cfg.Schema,
		cache:    cache,
		cacheTTL: ttl,
	}, nil
}

// cacheEntry wraps a Tenant with an expiry time.
type cacheEntry struct {
	tenant    interfaces.Tenant
	expiresAt time.Time
}

func (r *SQLTenantRegistry) fromCache(key string) (interfaces.Tenant, bool) {
	v, ok := r.cache.Get(key)
	if !ok {
		return interfaces.Tenant{}, false
	}
	e := v.(cacheEntry)
	if time.Now().After(e.expiresAt) {
		r.cache.Remove(key)
		return interfaces.Tenant{}, false
	}
	return e.tenant, true
}

func (r *SQLTenantRegistry) toCache(key string, t interfaces.Tenant) {
	r.cache.Add(key, cacheEntry{tenant: t, expiresAt: time.Now().Add(r.cacheTTL)})
}

func (r *SQLTenantRegistry) invalidate(t interfaces.Tenant) {
	r.cache.Remove("id:" + t.ID)
	r.cache.Remove("slug:" + t.Slug)
	for _, d := range t.Domains {
		r.cache.Remove("domain:" + d)
	}
}

// ProvidesMigrations returns the embedded migration FS.
// Implements interfaces.MigrationProvider.
func (r *SQLTenantRegistry) ProvidesMigrations() (fs.FS, error) {
	return TenantsMigrationsFS()
}

// MigrationsDependencies returns no dependencies (tenants is a root module).
func (r *SQLTenantRegistry) MigrationsDependencies() []string {
	return nil
}

// cols returns the resolved column name.
func (r *SQLTenantRegistry) c(override, def string) string {
	return r.schema.col(override, def)
}

func (r *SQLTenantRegistry) scanTenant(row interface{ Scan(...any) error }) (interfaces.Tenant, error) {
	var (
		t        interfaces.Tenant
		domains  []byte
		metadata []byte
	)
	if err := row.Scan(
		&t.ID, &t.Slug, &t.Name,
		&domains, &metadata, &t.IsActive,
	); err != nil {
		return interfaces.Tenant{}, err
	}
	if err := json.Unmarshal(domains, &t.Domains); err != nil {
		t.Domains = nil
	}
	if err := json.Unmarshal(metadata, &t.Metadata); err != nil {
		t.Metadata = nil
	}
	return t, nil
}

func (r *SQLTenantRegistry) selectSQL() string {
	tbl := r.schema.table()
	return fmt.Sprintf(
		"SELECT %s,%s,%s,%s,%s,%s FROM %s",
		r.c(r.schema.ColID, "id"),
		r.c(r.schema.ColSlug, "slug"),
		r.c(r.schema.ColName, "name"),
		r.c(r.schema.ColDomains, "domains"),
		r.c(r.schema.ColMetadata, "metadata"),
		r.c(r.schema.ColIsActive, "is_active"),
		tbl,
	)
}

// Ensure creates the tenant if it doesn't exist, or returns the existing one.
func (r *SQLTenantRegistry) Ensure(spec interfaces.TenantSpec) (interfaces.Tenant, error) {
	if err := spec.Validate(); err != nil {
		return interfaces.Tenant{}, err
	}

	// Check if it already exists.
	existing, err := r.GetBySlug(spec.Slug)
	if err == nil && !existing.IsZero() {
		return existing, nil
	}

	domainsJSON, _ := json.Marshal(spec.Domains)
	metadataJSON, _ := json.Marshal(spec.Metadata)
	if spec.Metadata == nil {
		metadataJSON = []byte("{}")
	}
	if spec.Domains == nil {
		domainsJSON = []byte("[]")
	}

	tbl := r.schema.table()
	q := fmt.Sprintf( //nolint:gosec // table/column names come from internal config, not user input
		`INSERT INTO %s (%s,%s,%s,%s) VALUES ($1,$2,$3::jsonb,$4::jsonb) RETURNING %s,%s,%s,%s,%s,%s`,
		tbl,
		r.c(r.schema.ColSlug, "slug"),
		r.c(r.schema.ColName, "name"),
		r.c(r.schema.ColDomains, "domains"),
		r.c(r.schema.ColMetadata, "metadata"),
		r.c(r.schema.ColID, "id"),
		r.c(r.schema.ColSlug, "slug"),
		r.c(r.schema.ColName, "name"),
		r.c(r.schema.ColDomains, "domains"),
		r.c(r.schema.ColMetadata, "metadata"),
		r.c(r.schema.ColIsActive, "is_active"),
	)

	row := r.db.QueryRowContext(context.Background(), q,
		spec.Slug, spec.Name, string(domainsJSON), string(metadataJSON),
	)
	t, err := r.scanTenant(row)
	if err != nil {
		return interfaces.Tenant{}, fmt.Errorf("ensure tenant %q: %w", spec.Slug, err)
	}
	r.toCache("id:"+t.ID, t)
	r.toCache("slug:"+t.Slug, t)
	return t, nil
}

// GetByID looks up a tenant by ID.
func (r *SQLTenantRegistry) GetByID(id string) (interfaces.Tenant, error) {
	if t, ok := r.fromCache("id:" + id); ok {
		return t, nil
	}
	q := r.selectSQL() + " WHERE " + r.c(r.schema.ColID, "id") + " = $1" //nolint:gosec
	row := r.db.QueryRowContext(context.Background(), q, id)
	t, err := r.scanTenant(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return interfaces.Tenant{}, interfaces.ErrResourceNotFound
		}
		return interfaces.Tenant{}, fmt.Errorf("get tenant by id %q: %w", id, err)
	}
	r.toCache("id:"+t.ID, t)
	return t, nil
}

// GetByDomain looks up a tenant by domain.
func (r *SQLTenantRegistry) GetByDomain(domain string) (interfaces.Tenant, error) {
	cacheKey := "domain:" + domain
	if t, ok := r.fromCache(cacheKey); ok {
		return t, nil
	}
	// Use JSONB containment: domains @> '["acme.example.com"]'::jsonb
	domainJSON, _ := json.Marshal([]string{domain})
	tbl := r.schema.table()
	q := fmt.Sprintf( //nolint:gosec // table/column names come from internal config, not user input
		"SELECT %s,%s,%s,%s,%s,%s FROM %s WHERE %s @> $1::jsonb",
		r.c(r.schema.ColID, "id"),
		r.c(r.schema.ColSlug, "slug"),
		r.c(r.schema.ColName, "name"),
		r.c(r.schema.ColDomains, "domains"),
		r.c(r.schema.ColMetadata, "metadata"),
		r.c(r.schema.ColIsActive, "is_active"),
		tbl,
		r.c(r.schema.ColDomains, "domains"),
	)
	row := r.db.QueryRowContext(context.Background(), q, string(domainJSON))
	t, err := r.scanTenant(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return interfaces.Tenant{}, interfaces.ErrResourceNotFound
		}
		return interfaces.Tenant{}, fmt.Errorf("get tenant by domain %q: %w", domain, err)
	}
	r.toCache(cacheKey, t)
	r.toCache("id:"+t.ID, t)
	return t, nil
}

// GetBySlug looks up a tenant by slug.
func (r *SQLTenantRegistry) GetBySlug(slug string) (interfaces.Tenant, error) {
	cacheKey := "slug:" + slug
	if t, ok := r.fromCache(cacheKey); ok {
		return t, nil
	}
	q := r.selectSQL() + " WHERE " + r.c(r.schema.ColSlug, "slug") + " = $1" //nolint:gosec
	row := r.db.QueryRowContext(context.Background(), q, slug)
	t, err := r.scanTenant(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return interfaces.Tenant{}, interfaces.ErrResourceNotFound
		}
		return interfaces.Tenant{}, fmt.Errorf("get tenant by slug %q: %w", slug, err)
	}
	r.toCache(cacheKey, t)
	r.toCache("id:"+t.ID, t)
	return t, nil
}

// List returns tenants matching the filter.
func (r *SQLTenantRegistry) List(filter interfaces.TenantFilter) ([]interfaces.Tenant, error) {
	tbl := r.schema.table()
	var conditions []string
	var args []any
	argN := 1

	if filter.ActiveOnly {
		conditions = append(conditions, fmt.Sprintf("%s = $%d", r.c(r.schema.ColIsActive, "is_active"), argN))
		args = append(args, true)
		argN++
	}
	if filter.Slug != "" {
		conditions = append(conditions, fmt.Sprintf("%s = $%d", r.c(r.schema.ColSlug, "slug"), argN))
		args = append(args, filter.Slug)
		argN++
	}
	if filter.Domain != "" {
		// Use JSONB containment: domains @> '["acme.example.com"]'::jsonb
		domainJSON, _ := json.Marshal([]string{filter.Domain})
		conditions = append(conditions, fmt.Sprintf("%s @> $%d::jsonb", r.c(r.schema.ColDomains, "domains"), argN))
		args = append(args, string(domainJSON))
	}

	q := fmt.Sprintf( //nolint:gosec // table/column names come from internal config, not user input
		"SELECT %s,%s,%s,%s,%s,%s FROM %s",
		r.c(r.schema.ColID, "id"),
		r.c(r.schema.ColSlug, "slug"),
		r.c(r.schema.ColName, "name"),
		r.c(r.schema.ColDomains, "domains"),
		r.c(r.schema.ColMetadata, "metadata"),
		r.c(r.schema.ColIsActive, "is_active"),
		tbl,
	)
	if len(conditions) > 0 {
		q += " WHERE " + strings.Join(conditions, " AND ")
	}
	q += fmt.Sprintf(" ORDER BY %s", r.c(r.schema.ColName, "name"))
	if filter.Limit > 0 {
		q += fmt.Sprintf(" LIMIT %d", filter.Limit)
	}
	if filter.Offset > 0 {
		q += fmt.Sprintf(" OFFSET %d", filter.Offset)
	}

	rows, err := r.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		return nil, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()

	var tenants []interfaces.Tenant
	for rows.Next() {
		t, err := r.scanTenant(rows)
		if err != nil {
			return nil, fmt.Errorf("scan tenant row: %w", err)
		}
		tenants = append(tenants, t)
	}
	return tenants, rows.Err()
}

// Update applies a partial patch to an existing tenant.
func (r *SQLTenantRegistry) Update(id string, patch interfaces.TenantPatch) (interfaces.Tenant, error) {
	tbl := r.schema.table()
	var setClauses []string
	var args []any
	argN := 1

	if patch.Name != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s=$%d", r.c(r.schema.ColName, "name"), argN))
		args = append(args, *patch.Name)
		argN++
	}
	if patch.Domains != nil {
		domainsJSON, _ := json.Marshal(patch.Domains)
		setClauses = append(setClauses, fmt.Sprintf("%s=$%d::jsonb", r.c(r.schema.ColDomains, "domains"), argN))
		args = append(args, string(domainsJSON))
		argN++
	}
	if patch.Metadata != nil {
		metadataJSON, _ := json.Marshal(patch.Metadata)
		setClauses = append(setClauses, fmt.Sprintf("%s=$%d::jsonb", r.c(r.schema.ColMetadata, "metadata"), argN))
		args = append(args, string(metadataJSON))
		argN++
	}
	if patch.IsActive != nil {
		setClauses = append(setClauses, fmt.Sprintf("%s=$%d", r.c(r.schema.ColIsActive, "is_active"), argN))
		args = append(args, *patch.IsActive)
		argN++
	}
	if len(setClauses) == 0 {
		return r.GetByID(id)
	}

	// Always bump updated_at.
	setClauses = append(setClauses, fmt.Sprintf("%s=NOW()", r.c(r.schema.ColUpdatedAt, "updated_at")))

	q := fmt.Sprintf( //nolint:gosec // table/column names come from internal config, not user input
		"UPDATE %s SET %s WHERE %s=$%d RETURNING %s,%s,%s,%s,%s,%s",
		tbl,
		strings.Join(setClauses, ","),
		r.c(r.schema.ColID, "id"), argN,
		r.c(r.schema.ColID, "id"),
		r.c(r.schema.ColSlug, "slug"),
		r.c(r.schema.ColName, "name"),
		r.c(r.schema.ColDomains, "domains"),
		r.c(r.schema.ColMetadata, "metadata"),
		r.c(r.schema.ColIsActive, "is_active"),
	)
	args = append(args, id)

	row := r.db.QueryRowContext(context.Background(), q, args...)
	t, err := r.scanTenant(row)
	if err != nil {
		if err == sql.ErrNoRows {
			return interfaces.Tenant{}, interfaces.ErrResourceNotFound
		}
		return interfaces.Tenant{}, fmt.Errorf("update tenant %q: %w", id, err)
	}
	r.invalidate(t)
	r.toCache("id:"+t.ID, t)
	r.toCache("slug:"+t.Slug, t)
	return t, nil
}

// Disable soft-deletes a tenant by setting is_active = false.
func (r *SQLTenantRegistry) Disable(id string) error {
	// Fetch first so we can invalidate slug and domain cache keys after the update.
	existing, err := r.GetByID(id)
	if err != nil {
		return err
	}

	tbl := r.schema.table()
	q := fmt.Sprintf( //nolint:gosec // table/column names come from internal config, not user input
		"UPDATE %s SET %s=FALSE,%s=NOW() WHERE %s=$1",
		tbl,
		r.c(r.schema.ColIsActive, "is_active"),
		r.c(r.schema.ColUpdatedAt, "updated_at"),
		r.c(r.schema.ColID, "id"),
	)
	result, err := r.db.ExecContext(context.Background(), q, id)
	if err != nil {
		return fmt.Errorf("disable tenant %q: %w", id, err)
	}
	n, _ := result.RowsAffected()
	if n == 0 {
		return interfaces.ErrResourceNotFound
	}
	// Invalidate all cache keys (id, slug, and every domain) so stale IsActive=true
	// entries cannot be served after a tenant is disabled.
	r.invalidate(existing)
	return nil
}
