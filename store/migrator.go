package store

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migrator applies SQL migrations from the embedded filesystem.
type Migrator struct {
	pool *pgxpool.Pool
}

// NewMigrator creates a new Migrator.
func NewMigrator(pool *pgxpool.Pool) *Migrator {
	return &Migrator{pool: pool}
}

// Migrate applies all pending migrations in order. It uses an advisory lock
// to prevent concurrent migration runs and tracks applied migrations in a
// schema_migrations table.
func (m *Migrator) Migrate(ctx context.Context) error {
	// Acquire advisory lock to prevent concurrent migrations.
	_, err := m.pool.Exec(ctx, `SELECT pg_advisory_lock(1)`)
	if err != nil {
		return fmt.Errorf("acquire advisory lock: %w", err)
	}
	defer func() {
		_, _ = m.pool.Exec(ctx, `SELECT pg_advisory_unlock(1)`)
	}()

	// Ensure migration tracking table exists.
	_, err = m.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version TEXT PRIMARY KEY,
			applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		)`)
	if err != nil {
		return fmt.Errorf("create schema_migrations table: %w", err)
	}

	// Get already-applied migrations.
	applied := map[string]bool{}
	rows, err := m.pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("query applied migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("scan migration version: %w", err)
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate applied migrations: %w", err)
	}

	// Read migration files.
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}

	// Sort by name to ensure order.
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}

		version := strings.TrimSuffix(entry.Name(), ".sql")
		if applied[version] {
			continue
		}

		content, err := fs.ReadFile(migrationsFS, "migrations/"+entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", entry.Name(), err)
		}

		tx, err := m.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin tx for %s: %w", entry.Name(), err)
		}

		if _, err := tx.Exec(ctx, string(content)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("execute migration %s: %w", entry.Name(), err)
		}

		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", entry.Name(), err)
		}

		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", entry.Name(), err)
		}
	}

	return nil
}
