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

// PartitionedDatabaseConfig holds configuration for the database.partitioned module.
type PartitionedDatabaseConfig struct {
	Driver       string   `json:"driver" yaml:"driver"`
	DSN          string   `json:"dsn" yaml:"dsn"`
	MaxOpenConns int      `json:"maxOpenConns" yaml:"maxOpenConns"`
	MaxIdleConns int      `json:"maxIdleConns" yaml:"maxIdleConns"`
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
}

// PartitionedDatabase wraps WorkflowDatabase and adds PostgreSQL partition
// management. It satisfies DBProvider, DBDriverProvider, PartitionKeyProvider,
// and PartitionManager.
type PartitionedDatabase struct {
	name   string
	config PartitionedDatabaseConfig
	base   *WorkflowDatabase
	mu     sync.RWMutex
}

// NewPartitionedDatabase creates a new PartitionedDatabase module.
func NewPartitionedDatabase(name string, cfg PartitionedDatabaseConfig) *PartitionedDatabase {
	dbConfig := DatabaseConfig{
		Driver:       cfg.Driver,
		DSN:          cfg.DSN,
		MaxOpenConns: cfg.MaxOpenConns,
		MaxIdleConns: cfg.MaxIdleConns,
	}
	if cfg.PartitionType == "" {
		cfg.PartitionType = PartitionTypeList
	}
	if cfg.PartitionNameFormat == "" {
		cfg.PartitionNameFormat = "{table}_{tenant}"
	}
	return &PartitionedDatabase{
		name:   name,
		config: cfg,
		base:   NewWorkflowDatabase(name+"._base", dbConfig),
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
func (p *PartitionedDatabase) PartitionKey() string {
	return p.config.PartitionKey
}

// PartitionType returns the partition type ("list" or "range").
func (p *PartitionedDatabase) PartitionType() string {
	return p.config.PartitionType
}

// PartitionNameFormat returns the configured partition name format template.
func (p *PartitionedDatabase) PartitionNameFormat() string {
	return p.config.PartitionNameFormat
}

// PartitionTableName resolves the partition table name for a given parent
// table and tenant value using the configured partitionNameFormat.
func (p *PartitionedDatabase) PartitionTableName(parentTable, tenantValue string) string {
	suffix := sanitizePartitionSuffix(tenantValue)
	name := p.config.PartitionNameFormat
	name = strings.ReplaceAll(name, "{table}", parentTable)
	name = strings.ReplaceAll(name, "{tenant}", suffix)
	return name
}

// Tables returns the list of tables managed by this partitioned database.
func (p *PartitionedDatabase) Tables() []string {
	result := make([]string, len(p.config.Tables))
	copy(result, p.config.Tables)
	return result
}

// EnsurePartition creates a partition for the given tenant value on all
// configured tables. The operation is idempotent — IF NOT EXISTS prevents errors
// when the partition already exists.
//
// For LIST partitions: CREATE TABLE IF NOT EXISTS <name> PARTITION OF <table> FOR VALUES IN ('<value>')
// For RANGE partitions: CREATE TABLE IF NOT EXISTS <name> PARTITION OF <table> FOR VALUES FROM ('<value>') TO ('<value>\x00')
//
// Only PostgreSQL (pgx, pgx/v5, postgres) is supported. The method validates
// the tenant value and table/column names to prevent SQL injection.
func (p *PartitionedDatabase) EnsurePartition(ctx context.Context, tenantValue string) error {
	if !validPartitionValue.MatchString(tenantValue) {
		return fmt.Errorf("partitioned database %q: invalid tenant value %q (must match [a-zA-Z0-9_.\\-]+)", p.name, tenantValue)
	}

	if !isSupportedPartitionDriver(p.config.Driver) {
		return fmt.Errorf("partitioned database %q: driver %q does not support partitioning (use pgx, pgx/v5, or postgres)", p.name, p.config.Driver)
	}

	if err := validateIdentifier(p.config.PartitionKey); err != nil {
		return fmt.Errorf("partitioned database %q: invalid partition_key: %w", p.name, err)
	}

	db := p.base.DB()
	if db == nil {
		return fmt.Errorf("partitioned database %q: database connection is nil", p.name)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	for _, table := range p.config.Tables {
		if err := validateIdentifier(table); err != nil {
			return fmt.Errorf("partitioned database %q: invalid table name: %w", p.name, err)
		}

		partitionName := p.PartitionTableName(table, tenantValue)

		// Validate the computed partition name is a safe identifier.
		if err := validateIdentifier(partitionName); err != nil {
			return fmt.Errorf("partitioned database %q: invalid partition name %q: %w", p.name, partitionName, err)
		}

		var ddl string
		// We have already validated tenantValue against validPartitionValue so
		// it cannot contain single-quote characters.
		safeValue := strings.ReplaceAll(tenantValue, "'", "")

		switch p.config.PartitionType {
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
				p.name, p.config.PartitionType, PartitionTypeList, PartitionTypeRange)
		}

		if _, err := db.ExecContext(ctx, ddl); err != nil {
			return fmt.Errorf("partitioned database %q: failed to create partition %q for table %q: %w",
				p.name, partitionName, table, err)
		}
	}

	return nil
}

// SyncPartitionsFromSource queries the configured sourceTable for all distinct
// tenant values and ensures that partitions exist for each one.
// This enables automatic partition creation when new tenants are added to a
// source table (e.g., a "tenants" table).
//
// No-ops if sourceTable is not configured.
func (p *PartitionedDatabase) SyncPartitionsFromSource(ctx context.Context) error {
	if p.config.SourceTable == "" {
		return nil
	}

	if err := validateIdentifier(p.config.SourceTable); err != nil {
		return fmt.Errorf("partitioned database %q: invalid source table: %w", p.name, err)
	}

	srcCol := p.config.SourceColumn
	if srcCol == "" {
		srcCol = p.config.PartitionKey
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
		srcCol, p.config.SourceTable, srcCol)

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return fmt.Errorf("partitioned database %q: failed to query source table %q: %w",
			p.name, p.config.SourceTable, err)
	}
	defer rows.Close()

	var tenants []string
	for rows.Next() {
		var val string
		if err := rows.Scan(&val); err != nil {
			return fmt.Errorf("partitioned database %q: failed to scan tenant value: %w", p.name, err)
		}
		tenants = append(tenants, val)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("partitioned database %q: row iteration error: %w", p.name, err)
	}

	for _, tenant := range tenants {
		if err := p.EnsurePartition(ctx, tenant); err != nil {
			return err
		}
	}

	return nil
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
