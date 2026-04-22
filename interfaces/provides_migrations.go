package interfaces

import "io/fs"

// MigrationProvider is optionally implemented by modules that embed their own
// SQL migration files (e.g., the tenants module, authz module).
//
// The workflow-plugin-migrations runner walks all registered modules in the
// modular service registry, calls ProvidesMigrations on those that implement
// this interface, collects the returned filesystems, orders them by declared
// dependency, and applies them before the application's own migrations.
type MigrationProvider interface {
	// ProvidesMigrations returns an fs.FS rooted at the migrations directory.
	// The FS must contain SQL files named in the golang-migrate convention:
	// <version>_<description>.up.sql and <version>_<description>.down.sql.
	ProvidesMigrations() (fs.FS, error)

	// MigrationsDependencies returns the names of other MigrationProvider
	// modules whose migrations must be applied before this module's.
	// For example, the authz module might return ["workflow.tenants"].
	// Returning nil or an empty slice means no dependencies.
	MigrationsDependencies() []string
}
