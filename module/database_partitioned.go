package module

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/CrisisTextLine/modular"
)

// validPartitionValue matches safe LIST partition values (alphanumeric, hyphens, underscores, dots).
var validPartitionValue = regexp.MustCompile(`^[a-zA-Z0-9_.\-]+$`)

// Partition types supported by PostgreSQL.
const (
	PartitionTypeList  = "list"
	PartitionTypeRange = "range"
)

// PartitionKeyProvider is optionally implemented by database modules that support
// partitioning. Steps can use PartitionKey() to determine the column name
// for automatic tenant scoping, and PartitionTableName() to resolve
// tenant-specific partition table names at query time.
type PartitionKeyProvider interface {
	DBProvider
	PartitionKey() string
	// PartitionTableName resolves the partition table name for a given parent
	// table and tenant value, using the configured partitionNameFormat.
	// Returns the parent table name unchanged when no format is configured.
	PartitionTableName(parentTable, tenantValue string) string
}

// PartitionManager is optionally implemented by database modules that support
// runtime creation of partitions. The EnsurePartition method is idempotent —
// if the partition already exists the call succeeds without error.
type PartitionManager interface {
	PartitionKeyProvider
	EnsurePartition(ctx context.Context, tenantValue string) error
	// SyncPartitionsFromSource queries the configured sourceTable for all
	// distinct tenant values and ensures that partitions exist for each one.
	// No-ops if sourceTable is not configured.
	SyncPartitionsFromSource(ctx context.Context) error
}

// MultiPartitionManager extends PartitionManager for databases that can have
// more than one partition key configuration (e.g. tenant-partitioned tables
// AND api-version-partitioned tables in the same database). It is implemented
// by PartitionedDatabase; the additional methods are primarily meaningful when
// multiple partition configs are configured.
type MultiPartitionManager interface {
	PartitionManager
	// PartitionConfigs returns all configured partition groups.
	PartitionConfigs() []PartitionConfig
	// EnsurePartitionForKey creates partitions for the specified partition key
	// and value on all tables that belong to that partition config. Returns an
	// error if no config with that partitionKey is registered.
	EnsurePartitionForKey(ctx context.Context, partitionKey, value string) error
	// SyncPartitionsForKey syncs partitions for the specified partition key's
	// configured source table. No-ops if no sourceTable is configured for that
	// key. Returns an error if no config with that partitionKey is registered.
	SyncPartitionsForKey(ctx context.Context, partitionKey string) error
}

// PartitionConfig holds per-partition-key configuration within a
// database.partitioned module. Multiple PartitionConfig entries allow a single
// module to manage tables that are partitioned by different columns or with
// different partition types.
type PartitionConfig struct {
	// PartitionKey is the column name used for partitioning (e.g. tenant_id).
	PartitionKey string `json:"partitionKey" yaml:"partitionKey"`
	// Tables lists the tables that are partitioned by this key.
	Tables []string `json:"tables" yaml:"tables"`
	// PartitionType is "list" (default) or "range".
	PartitionType string `json:"partitionType" yaml:"partitionType"`
	// PartitionNameFormat is a template for generating partition table names.
	// Supports {table} and {tenant} placeholders. Default: "{table}_{tenant}".
	PartitionNameFormat string `json:"partitionNameFormat" yaml:"partitionNameFormat"`
	// SourceTable is the table queried by SyncPartitionsFromSource for this key.
	SourceTable string `json:"sourceTable" yaml:"sourceTable"`
	// SourceColumn overrides the column queried in SourceTable. Defaults to PartitionKey.
	SourceColumn string `json:"sourceColumn" yaml:"sourceColumn"`
}

// PartitionedDatabaseConfig holds configuration for the database.partitioned module.
//
// Single-partition mode (backward-compatible): set PartitionKey, Tables, and
// optionally PartitionType, PartitionNameFormat, SourceTable, SourceColumn at
// the top level.
//
// Multi-partition mode: set Partitions to a list of PartitionConfig entries.
// Each entry is an independent partition group with its own key, tables, type,
// naming format and optional source. The top-level single-partition fields are
// ignored when Partitions is non-empty.
type PartitionedDatabaseConfig struct {
	Driver       string `json:"driver" yaml:"driver"`
	DSN          string `json:"dsn" yaml:"dsn"`
	MaxOpenConns int    `json:"maxOpenConns" yaml:"maxOpenConns"`
	MaxIdleConns int    `json:"maxIdleConns" yaml:"maxIdleConns"`

	// ── Single-partition fields (used when Partitions is empty) ──────────────
	PartitionKey string   `json:"partitionKey" yaml:"partitionKey"`
	Tables       []string `json:"tables" yaml:"tables"`
	// PartitionType is "list" (default) or "range".
	// LIST partitions are created with FOR VALUES IN ('value').
	// RANGE partitions are created with FOR VALUES FROM ('value') TO ('value_next').
	PartitionType string `json:"partitionType" yaml:"partitionType"`
	// PartitionNameFormat is a template for generating partition table names.
	// Supports {table} and {tenant} placeholders.
	// Default: "{table}_{tenant}" (e.g. forms_org_alpha).
	PartitionNameFormat string `json:"partitionNameFormat" yaml:"partitionNameFormat"`
	// SourceTable is the table that contains all tenant IDs.
	// When set, SyncPartitionsFromSource queries this table for all distinct
	// values in the partition key column and ensures partitions exist.
	// Example: "tenants" — will query "SELECT DISTINCT tenant_id FROM tenants".
	SourceTable string `json:"sourceTable" yaml:"sourceTable"`
	// SourceColumn overrides the column queried in sourceTable.
	// Defaults to PartitionKey if empty.
	SourceColumn string `json:"sourceColumn" yaml:"sourceColumn"`

	// ── Multi-partition mode ─────────────────────────────────────────────────
	// Partitions lists independent partition key configurations. When non-empty,
	// the single-partition fields above are ignored.
	Partitions []PartitionConfig `json:"partitions" yaml:"partitions"`
}

// PartitionedDatabase wraps WorkflowDatabase and adds PostgreSQL partition
// management. It satisfies DBProvider, DBDriverProvider, PartitionKeyProvider,
// PartitionManager, and MultiPartitionManager.
type PartitionedDatabase struct {
	name       string
	config     PartitionedDatabaseConfig
	partitions []PartitionConfig // normalized; always len >= 1 after construction
	base       *WorkflowDatabase
	mu         sync.RWMutex
}

// normalizePartitionConfig applies defaults to a PartitionConfig and returns the result.
func normalizePartitionConfig(p PartitionConfig) PartitionConfig {
	if p.PartitionType == "" {
		p.PartitionType = PartitionTypeList
	}
	if p.PartitionNameFormat == "" {
		p.PartitionNameFormat = "{table}_{tenant}"
	}
	return p
}

// NewPartitionedDatabase creates a new PartitionedDatabase module.
//
// When cfg.Partitions is non-empty the entries are used as-is (with defaults
// applied). Otherwise a single PartitionConfig is built from the top-level
// PartitionKey / Tables / … fields for backward compatibility.
func NewPartitionedDatabase(name string, cfg PartitionedDatabaseConfig) *PartitionedDatabase {
	dbConfig := DatabaseConfig{
		Driver:       cfg.Driver,
		DSN:          cfg.DSN,
		MaxOpenConns: cfg.MaxOpenConns,
		MaxIdleConns: cfg.MaxIdleConns,
	}

	var partitions []PartitionConfig
	if len(cfg.Partitions) > 0 {
		for _, p := range cfg.Partitions {
			partitions = append(partitions, normalizePartitionConfig(p))
		}
	} else {
		partitions = []PartitionConfig{normalizePartitionConfig(PartitionConfig{
			PartitionKey:        cfg.PartitionKey,
			Tables:              cfg.Tables,
			PartitionType:       cfg.PartitionType,
			PartitionNameFormat: cfg.PartitionNameFormat,
			SourceTable:         cfg.SourceTable,
			SourceColumn:        cfg.SourceColumn,
		})}
	}

	return &PartitionedDatabase{
		name:       name,
		config:     cfg,
		partitions: partitions,
		base:       NewWorkflowDatabase(name+"._base", dbConfig),
	}
}

// Name returns the module name.
func (p *PartitionedDatabase) Name() string { return p.name }

// Init registers this module as a service.
func (p *PartitionedDatabase) Init(app modular.Application) error {
	return app.RegisterService(p.name, p)
}

// ProvidesServices declares the service this module provides.
func (p *PartitionedDatabase) ProvidesServices() []modular.ServiceProvider {
	return []modular.ServiceProvider{
		{
			Name:        p.name,
			Description: "Partitioned Database: " + p.name,
			Instance:    p,
		},
	}
}

// RequiresServices returns no dependencies.
func (p *PartitionedDatabase) RequiresServices() []modular.ServiceDependency {
	return nil
}

// Start opens the database connection during application startup.
func (p *PartitionedDatabase) Start(ctx context.Context) error {
	return p.base.Start(ctx)
}

// Stop closes the database connection during application shutdown.
func (p *PartitionedDatabase) Stop(ctx context.Context) error {
	return p.base.Stop(ctx)
}

// DB returns the underlying *sql.DB (satisfies DBProvider).
func (p *PartitionedDatabase) DB() *sql.DB {
	return p.base.DB()
}

// DriverName returns the configured database driver (satisfies DBDriverProvider).
func (p *PartitionedDatabase) DriverName() string {
	return p.config.Driver
}

// PartitionKey returns the column name used for partitioning (satisfies PartitionKeyProvider).
// When multiple partition configs are defined, it returns the first config's key.
func (p *PartitionedDatabase) PartitionKey() string {
	if len(p.partitions) > 0 {
		return p.partitions[0].PartitionKey
	}
	return ""
}

// PartitionType returns the partition type of the primary partition config ("list" or "range").
func (p *PartitionedDatabase) PartitionType() string {
	if len(p.partitions) > 0 {
		return p.partitions[0].PartitionType
	}
	return PartitionTypeList
}

// PartitionNameFormat returns the partition name format of the primary partition config.
func (p *PartitionedDatabase) PartitionNameFormat() string {
	if len(p.partitions) > 0 {
		return p.partitions[0].PartitionNameFormat
	}
	return "{table}_{tenant}"
}

// PartitionTableName resolves the partition table name for a given parent
// table and tenant value using the primary partition config's partitionNameFormat.
func (p *PartitionedDatabase) PartitionTableName(parentTable, tenantValue string) string {
	if len(p.partitions) == 0 {
		return parentTable
	}
	return applyPartitionNameFormat(p.partitions[0].PartitionNameFormat, parentTable, tenantValue)
}

// Tables returns the list of tables managed by the primary partition config.
func (p *PartitionedDatabase) Tables() []string {
	if len(p.partitions) == 0 {
		return nil
	}
	result := make([]string, len(p.partitions[0].Tables))
	copy(result, p.partitions[0].Tables)
	return result
}

// PartitionConfigs returns all configured partition groups (satisfies MultiPartitionManager).
// It returns a deep copy so callers cannot mutate the internal state.
func (p *PartitionedDatabase) PartitionConfigs() []PartitionConfig {
	result := make([]PartitionConfig, len(p.partitions))
	for i, cfg := range p.partitions {
		result[i] = cfg
		if cfg.Tables != nil {
			tablesCopy := make([]string, len(cfg.Tables))
			copy(tablesCopy, cfg.Tables)
			result[i].Tables = tablesCopy
		}
	}
	return result
}

// EnsurePartition creates a partition for the given value on all tables managed
// by the primary partition config. The operation is idempotent — IF NOT EXISTS
// prevents errors when the partition already exists.
//
// For LIST partitions: CREATE TABLE IF NOT EXISTS <name> PARTITION OF <table> FOR VALUES IN ('<value>')
// For RANGE partitions: CREATE TABLE IF NOT EXISTS <name> PARTITION OF <table> FOR VALUES FROM ('<value>') TO ('<value>\x00')
//
// Only PostgreSQL (pgx, pgx/v5, postgres) is supported. The method validates
// the tenant value and table/column names to prevent SQL injection.
func (p *PartitionedDatabase) EnsurePartition(ctx context.Context, tenantValue string) error {
	if len(p.partitions) == 0 {
		return fmt.Errorf("partitioned database %q: no partition config defined", p.name)
	}
	return p.ensurePartitionForConfig(ctx, p.partitions[0], tenantValue)
}

// EnsurePartitionForKey creates partitions for the specified partition key and
// value on all tables that belong to that partition config (satisfies
// MultiPartitionManager). Returns an error if no config with that partitionKey
// is registered.
func (p *PartitionedDatabase) EnsurePartitionForKey(ctx context.Context, partitionKey, value string) error {
	cfg, ok := p.partitionConfigByKey(partitionKey)
	if !ok {
		return fmt.Errorf("partitioned database %q: no partition config found for key %q", p.name, partitionKey)
	}
	return p.ensurePartitionForConfig(ctx, cfg, value)
}

// ensurePartitionForConfig is the shared implementation for EnsurePartition and
// EnsurePartitionForKey. It validates inputs and executes the DDL for each table.
func (p *PartitionedDatabase) ensurePartitionForConfig(ctx context.Context, cfg PartitionConfig, tenantValue string) error {
	if !validPartitionValue.MatchString(tenantValue) {
		return fmt.Errorf("partitioned database %q: invalid tenant value %q (must match [a-zA-Z0-9_.\\-]+)", p.name, tenantValue)
	}

	if !isSupportedPartitionDriver(p.config.Driver) {
		return fmt.Errorf("partitioned database %q: driver %q does not support partitioning (use pgx, pgx/v5, or postgres)", p.name, p.config.Driver)
	}

	if err := validateIdentifier(cfg.PartitionKey); err != nil {
		return fmt.Errorf("partitioned database %q: invalid partition_key: %w", p.name, err)
	}

	db := p.base.DB()
	if db == nil {
		return fmt.Errorf("partitioned database %q: database connection is nil", p.name)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, table := range cfg.Tables {
		if err := validateIdentifier(table); err != nil {
			return fmt.Errorf("partitioned database %q: invalid table name: %w", p.name, err)
		}

		partitionName := applyPartitionNameFormat(cfg.PartitionNameFormat, table, tenantValue)

		// Validate the computed partition name is a safe identifier.
		if err := validateIdentifier(partitionName); err != nil {
			return fmt.Errorf("partitioned database %q: invalid partition name %q: %w", p.name, partitionName, err)
		}

		var ddl string
		// We have already validated tenantValue against validPartitionValue so
		// it cannot contain single-quote characters.
		safeValue := strings.ReplaceAll(tenantValue, "'", "")

		switch cfg.PartitionType {
		case PartitionTypeList:
			ddl = fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES IN ('%s')",
				partitionName, table, safeValue,
			)
		case PartitionTypeRange:
			// RANGE partition: from the tenant value (inclusive) to the same
			// value followed by a null byte (exclusive). This creates a
			// single-value range partition, which is the closest equivalent
			// to LIST semantics for RANGE-partitioned tables.
			ddl = fmt.Sprintf(
				"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES FROM ('%s') TO ('%s\\x00')",
				partitionName, table, safeValue, safeValue,
			)
		default:
			return fmt.Errorf("partitioned database %q: unsupported partition type %q (use %q or %q)",
				p.name, cfg.PartitionType, PartitionTypeList, PartitionTypeRange)
		}

		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("partitioned database %q: failed to create partition %q for table %q: %w",
				p.name, partitionName, table, err)
		}
	}

	return nil
}

// SyncPartitionsFromSource queries the configured sourceTable for all distinct
// tenant values and ensures that partitions exist for each one. When multiple
// partition configs are defined, all configs with a sourceTable are synced.
//
// No-ops if no sourceTable is configured in any partition config.
func (p *PartitionedDatabase) SyncPartitionsFromSource(ctx context.Context) error {
	for _, cfg := range p.partitions {
		if err := p.syncPartitionConfigFromSource(ctx, cfg); err != nil {
			return err
		}
	}
	return nil
}

// SyncPartitionsForKey syncs partitions for the specified partition key's
// configured source table (satisfies MultiPartitionManager). No-ops if no
// sourceTable is configured for that key. Returns an error if no config with
// that partitionKey is registered.
func (p *PartitionedDatabase) SyncPartitionsForKey(ctx context.Context, partitionKey string) error {
	cfg, ok := p.partitionConfigByKey(partitionKey)
	if !ok {
		return fmt.Errorf("partitioned database %q: no partition config found for key %q", p.name, partitionKey)
	}
	return p.syncPartitionConfigFromSource(ctx, cfg)
}

// syncPartitionConfigFromSource is the shared implementation for
// SyncPartitionsFromSource and SyncPartitionsForKey.
func (p *PartitionedDatabase) syncPartitionConfigFromSource(ctx context.Context, cfg PartitionConfig) error {
	if cfg.SourceTable == "" {
		return nil
	}

	if err := validateIdentifier(cfg.SourceTable); err != nil {
		return fmt.Errorf("partitioned database %q: invalid source table: %w", p.name, err)
	}

	srcCol := cfg.SourceColumn
	if srcCol == "" {
		srcCol = cfg.PartitionKey
	}
	if err := validateIdentifier(srcCol); err != nil {
		return fmt.Errorf("partitioned database %q: invalid source column: %w", p.name, err)
	}

	db := p.base.DB()
	if db == nil {
		return fmt.Errorf("partitioned database %q: database connection is nil", p.name)
	}

	// All identifiers (srcCol, SourceTable) have been validated by validateIdentifier above.
	query := fmt.Sprintf("SELECT DISTINCT %s FROM %s WHERE %s IS NOT NULL", //nolint:gosec // G201: identifiers validated above
		srcCol, cfg.SourceTable, srcCol)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("partitioned database %q: failed to query source table %q: %w",
			p.name, cfg.SourceTable, err)
	}
	defer rows.Close()

	var values []string
	for rows.Next() {
		var val string
		if err := rows.Scan(&val); err != nil {
			return fmt.Errorf("partitioned database %q: failed to scan partition value: %w", p.name, err)
		}
		values = append(values, val)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("partitioned database %q: row iteration error: %w", p.name, err)
	}

	for _, val := range values {
		if err := p.ensurePartitionForConfig(ctx, cfg, val); err != nil {
			return err
		}
	}

	return nil
}

// partitionConfigByKey returns the PartitionConfig for the given partition key, if any.
func (p *PartitionedDatabase) partitionConfigByKey(partitionKey string) (PartitionConfig, bool) {
	for _, cfg := range p.partitions {
		if cfg.PartitionKey == partitionKey {
			return cfg, true
		}
	}
	return PartitionConfig{}, false
}

// isSupportedPartitionDriver returns true for PostgreSQL-compatible drivers.
func isSupportedPartitionDriver(driver string) bool {
	switch driver {
	case "pgx", "pgx/v5", "postgres":
		return true
	}
	return false
}

// sanitizePartitionSuffix converts a tenant value to a safe PostgreSQL identifier suffix.
// Hyphens and dots are replaced with underscores.
func sanitizePartitionSuffix(tenantValue string) string {
	r := strings.NewReplacer("-", "_", ".", "_")
	return r.Replace(tenantValue)
}

// applyPartitionNameFormat applies a partition name format template to a table
// name and tenant value. Supports {table} and {tenant} placeholders.
func applyPartitionNameFormat(format, parentTable, tenantValue string) string {
	suffix := sanitizePartitionSuffix(tenantValue)
	name := strings.ReplaceAll(format, "{table}", parentTable)
	name = strings.ReplaceAll(name, "{tenant}", suffix)
	return name
}
