package module

import (
	"embed"
	"io/fs"
)

//go:embed tenants_migrations/*.sql
var tenantsMigrationsFS embed.FS

// TenantsMigrationsFS returns the embedded SQL migration files for the tenants table.
// It implements the ProvidesMigrations contract from interfaces.MigrationProvider.
func TenantsMigrationsFS() (fs.FS, error) {
	return fs.Sub(tenantsMigrationsFS, "tenants_migrations")
}
