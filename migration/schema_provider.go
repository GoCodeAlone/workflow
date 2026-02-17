// Package migration provides a migration management system with schema providers,
// distributed locking, and a migration runner for applying database schema changes.
package migration

// SchemaProvider defines the interface for components that own database schemas.
// Each provider declares its schema name, current version, full DDL, and ordered diffs.
type SchemaProvider interface {
	// SchemaName returns a unique identifier for this schema (e.g., "workflow_executions").
	SchemaName() string
	// SchemaVersion returns the current version of the schema.
	SchemaVersion() int
	// SchemaSQL returns the full DDL for the current schema version.
	SchemaSQL() string
	// SchemaDiffs returns ordered diffs for migrating between versions.
	SchemaDiffs() []SchemaDiff
}

// SchemaDiff represents a single migration step between two schema versions.
type SchemaDiff struct {
	FromVersion int
	ToVersion   int
	UpSQL       string
	DownSQL     string
}
