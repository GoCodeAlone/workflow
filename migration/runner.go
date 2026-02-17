package migration

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

// AppliedMigration records a migration that has already been applied.
type AppliedMigration struct {
	SchemaName string
	Version    int
	Checksum   string
	AppliedAt  time.Time
}

// MigrationStore persists records of applied migrations.
type MigrationStore interface {
	// Applied returns all applied migrations for the given schema, ordered by version.
	Applied(ctx context.Context, schemaName string) ([]AppliedMigration, error)
	// Record stores an applied migration record.
	Record(ctx context.Context, schemaName string, version int, checksum string) error
}

// MigrationRunner coordinates schema migrations across multiple providers.
type MigrationRunner struct {
	store  MigrationStore
	locker DistributedLock
	logger *slog.Logger
}

// NewMigrationRunner creates a new MigrationRunner.
func NewMigrationRunner(store MigrationStore, locker DistributedLock, logger *slog.Logger) *MigrationRunner {
	if logger == nil {
		logger = slog.Default()
	}
	return &MigrationRunner{
		store:  store,
		locker: locker,
		logger: logger,
	}
}

// PendingMigration describes a migration that has not yet been applied.
type PendingMigration struct {
	SchemaName  string
	FromVersion int
	ToVersion   int
	UpSQL       string
}

// Pending returns all pending migrations for the given providers without applying them.
func (r *MigrationRunner) Pending(ctx context.Context, providers ...SchemaProvider) ([]PendingMigration, error) {
	var pending []PendingMigration

	for _, p := range providers {
		applied, err := r.store.Applied(ctx, p.SchemaName())
		if err != nil {
			return nil, fmt.Errorf("query applied for %s: %w", p.SchemaName(), err)
		}

		currentVersion := 0
		for _, a := range applied {
			if a.Version > currentVersion {
				currentVersion = a.Version
			}
		}

		diffs := p.SchemaDiffs()
		sort.Slice(diffs, func(i, j int) bool {
			return diffs[i].ToVersion < diffs[j].ToVersion
		})

		for _, d := range diffs {
			if d.FromVersion >= currentVersion && d.ToVersion > currentVersion {
				pending = append(pending, PendingMigration{
					SchemaName:  p.SchemaName(),
					FromVersion: d.FromVersion,
					ToVersion:   d.ToVersion,
					UpSQL:       d.UpSQL,
				})
				currentVersion = d.ToVersion
			}
		}
	}

	return pending, nil
}

// Status returns the applied migrations for each provider.
func (r *MigrationRunner) Status(ctx context.Context, providers ...SchemaProvider) (map[string][]AppliedMigration, error) {
	result := make(map[string][]AppliedMigration, len(providers))
	for _, p := range providers {
		applied, err := r.store.Applied(ctx, p.SchemaName())
		if err != nil {
			return nil, fmt.Errorf("query applied for %s: %w", p.SchemaName(), err)
		}
		result[p.SchemaName()] = applied
	}
	return result, nil
}

// Run applies all pending migrations for the given providers.
// It acquires a distributed lock, queries the store for applied versions,
// compares against providers' current versions, runs pending diffs in order,
// and records each applied migration.
func (r *MigrationRunner) Run(ctx context.Context, db *sql.DB, providers ...SchemaProvider) error {
	// 1. Acquire distributed lock.
	release, err := r.locker.Acquire(ctx, "migration_runner")
	if err != nil {
		return fmt.Errorf("acquire migration lock: %w", err)
	}
	defer release()

	for _, p := range providers {
		if err := r.runProvider(ctx, db, p); err != nil {
			return fmt.Errorf("migrate %s: %w", p.SchemaName(), err)
		}
	}

	return nil
}

func (r *MigrationRunner) runProvider(ctx context.Context, db *sql.DB, p SchemaProvider) error {
	schemaName := p.SchemaName()

	// 2. Query migration store for applied versions.
	applied, err := r.store.Applied(ctx, schemaName)
	if err != nil {
		return fmt.Errorf("query applied: %w", err)
	}

	appliedVersions := make(map[int]bool, len(applied))
	currentVersion := 0
	for _, a := range applied {
		appliedVersions[a.Version] = true
		if a.Version > currentVersion {
			currentVersion = a.Version
		}
	}

	targetVersion := p.SchemaVersion()
	if currentVersion >= targetVersion {
		r.logger.Info("schema up to date",
			"schema", schemaName,
			"version", currentVersion)
		return nil
	}

	// 3. Compare against provider's current version and get pending diffs.
	diffs := p.SchemaDiffs()
	sort.Slice(diffs, func(i, j int) bool {
		return diffs[i].ToVersion < diffs[j].ToVersion
	})

	// If no applied versions and no diffs, apply the full schema.
	if currentVersion == 0 && len(diffs) == 0 {
		fullSQL := p.SchemaSQL()
		if fullSQL == "" {
			return nil
		}

		r.logger.Info("applying full schema",
			"schema", schemaName,
			"version", targetVersion)

		tx, err := db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tx: %w", err)
		}

		if _, err := tx.ExecContext(ctx, fullSQL); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("execute full schema: %w", err)
		}

		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit full schema: %w", err)
		}

		checksum := checksumSQL(fullSQL)
		if err := r.store.Record(ctx, schemaName, targetVersion, checksum); err != nil {
			return fmt.Errorf("record migration: %w", err)
		}

		return nil
	}

	// 4. Run pending diffs in order.
	for _, d := range diffs {
		if d.FromVersion >= currentVersion && d.ToVersion > currentVersion {
			r.logger.Info("applying migration",
				"schema", schemaName,
				"from", d.FromVersion,
				"to", d.ToVersion)

			tx, err := db.BeginTx(ctx, nil)
			if err != nil {
				return fmt.Errorf("begin tx for v%d->v%d: %w", d.FromVersion, d.ToVersion, err)
			}

			if _, err := tx.ExecContext(ctx, d.UpSQL); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("execute v%d->v%d: %w", d.FromVersion, d.ToVersion, err)
			}

			if err := tx.Commit(); err != nil {
				return fmt.Errorf("commit v%d->v%d: %w", d.FromVersion, d.ToVersion, err)
			}

			// 5. Record each applied migration.
			checksum := checksumSQL(d.UpSQL)
			if err := r.store.Record(ctx, schemaName, d.ToVersion, checksum); err != nil {
				return fmt.Errorf("record v%d: %w", d.ToVersion, err)
			}

			currentVersion = d.ToVersion
		}
	}

	return nil
}

// checksumSQL computes a sha256 checksum for a SQL string.
func checksumSQL(sql string) string {
	h := sha256.Sum256([]byte(sql))
	return fmt.Sprintf("%x", h[:8])
}

// SQLiteMigrationStore implements MigrationStore using SQLite.
type SQLiteMigrationStore struct {
	db *sql.DB
}

// NewSQLiteMigrationStore creates a new SQLiteMigrationStore and ensures the
// _migrations table exists.
func NewSQLiteMigrationStore(db *sql.DB) (*SQLiteMigrationStore, error) {
	_, err := db.Exec(`CREATE TABLE IF NOT EXISTS _migrations (
		schema_name TEXT NOT NULL,
		version     INTEGER NOT NULL,
		checksum    TEXT NOT NULL,
		applied_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (schema_name, version)
	)`)
	if err != nil {
		return nil, fmt.Errorf("create _migrations table: %w", err)
	}
	return &SQLiteMigrationStore{db: db}, nil
}

// Applied returns all applied migrations for the given schema, ordered by version.
func (s *SQLiteMigrationStore) Applied(ctx context.Context, schemaName string) ([]AppliedMigration, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT schema_name, version, checksum, applied_at FROM _migrations WHERE schema_name = ? ORDER BY version`,
		schemaName)
	if err != nil {
		return nil, fmt.Errorf("query _migrations: %w", err)
	}
	defer rows.Close()

	var result []AppliedMigration
	for rows.Next() {
		var m AppliedMigration
		if err := rows.Scan(&m.SchemaName, &m.Version, &m.Checksum, &m.AppliedAt); err != nil {
			return nil, fmt.Errorf("scan migration: %w", err)
		}
		result = append(result, m)
	}
	return result, rows.Err()
}

// Record stores an applied migration record.
func (s *SQLiteMigrationStore) Record(ctx context.Context, schemaName string, version int, checksum string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO _migrations (schema_name, version, checksum) VALUES (?, ?, ?)`,
		schemaName, version, checksum)
	if err != nil {
		return fmt.Errorf("insert _migrations: %w", err)
	}
	return nil
}
