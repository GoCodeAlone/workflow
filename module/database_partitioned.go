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

// PartitionKeyProvider is optionally implemented by database modules that support
// LIST partitioning. Steps can use PartitionKey() to determine the column name
// for automatic tenant scoping.
type PartitionKeyProvider interface {
	DBProvider
	PartitionKey() string
}

// PartitionManager is optionally implemented by database modules that support
// runtime creation of LIST partitions. The EnsurePartition method is idempotent —
// if the partition already exists the call succeeds without error.
type PartitionManager interface {
	PartitionKeyProvider
	EnsurePartition(ctx context.Context, tenantValue string) error
}

// PartitionedDatabaseConfig holds configuration for the database.partitioned module.
type PartitionedDatabaseConfig struct {
	Driver       string   `json:"driver" yaml:"driver"`
	DSN          string   `json:"dsn" yaml:"dsn"`
	MaxOpenConns int      `json:"maxOpenConns" yaml:"maxOpenConns"`
	MaxIdleConns int      `json:"maxIdleConns" yaml:"maxIdleConns"`
	PartitionKey string   `json:"partitionKey" yaml:"partitionKey"`
	Tables       []string `json:"tables" yaml:"tables"`
}

// PartitionedDatabase wraps WorkflowDatabase and adds PostgreSQL LIST partition
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

// PartitionKey returns the column name used for LIST partitioning (satisfies PartitionKeyProvider).
func (p *PartitionedDatabase) PartitionKey() string {
	return p.config.PartitionKey
}

// Tables returns the list of tables managed by this partitioned database.
func (p *PartitionedDatabase) Tables() []string {
	result := make([]string, len(p.config.Tables))
	copy(result, p.config.Tables)
	return result
}

// EnsurePartition creates a LIST partition for the given tenant value on all
// configured tables. The operation is idempotent — IF NOT EXISTS prevents errors
// when the partition already exists.
//
// Only PostgreSQL (pgx, pgx/v5, postgres) is supported. The method validates
// the tenant value and table/column names to prevent SQL injection.
func (p *PartitionedDatabase) EnsurePartition(ctx context.Context, tenantValue string) error {
	if !validPartitionValue.MatchString(tenantValue) {
		return fmt.Errorf("partitioned database %q: invalid tenant value %q (must match [a-zA-Z0-9_.\\-]+)", p.name, tenantValue)
	}

	if !isSupportedPartitionDriver(p.config.Driver) {
		return fmt.Errorf("partitioned database %q: driver %q does not support LIST partitioning (use pgx, pgx/v5, or postgres)", p.name, p.config.Driver)
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

		// Sanitize the partition suffix: replace hyphens and dots with underscores.
		partitionSuffix := sanitizePartitionSuffix(tenantValue)
		partitionName := table + "_" + partitionSuffix

		// Use IF NOT EXISTS to make this idempotent.
		// The tenant value is embedded as a quoted literal (single-quoted).
		// We have already validated tenantValue against validPartitionValue so
		// it cannot contain single-quote characters.
		sql := fmt.Sprintf(
			"CREATE TABLE IF NOT EXISTS %s PARTITION OF %s FOR VALUES IN ('%s')",
			partitionName,
			table,
			strings.ReplaceAll(tenantValue, "'", ""),
		)

		if _, err := db.ExecContext(ctx, sql); err != nil {
			return fmt.Errorf("partitioned database %q: failed to create partition %q for table %q: %w",
				p.name, partitionName, table, err)
		}
	}

	return nil
}

// isSupportedPartitionDriver returns true for PostgreSQL-compatible drivers.
func isSupportedPartitionDriver(driver string) bool {
	switch driver {
	case "pgx", "pgx/v5", "postgres", "postgresql":
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
