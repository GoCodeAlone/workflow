package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/migration"

	_ "modernc.org/sqlite"
)

func runMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ContinueOnError)
	dbPath := fs.String("db", "workflow.db", "Path to SQLite database file")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl migrate <subcommand> [options]

Manage database schema migrations.

Subcommands:
  status    Show applied and pending migrations
  diff      Show pending migrations without applying them
  apply     Apply pending migrations

Examples:
  wfctl migrate status --db workflow.db
  wfctl migrate diff --db workflow.db
  wfctl migrate apply --db workflow.db

Options:
`)
		fs.PrintDefaults()
	}

	if len(args) == 0 {
		fs.Usage()
		return fmt.Errorf("subcommand required: status, diff, or apply")
	}

	subcmd := args[0]
	if err := fs.Parse(args[1:]); err != nil {
		return err
	}

	db, err := sql.Open("sqlite", *dbPath)
	if err != nil {
		return fmt.Errorf("open database %s: %w", *dbPath, err)
	}
	defer db.Close()

	store, err := migration.NewSQLiteMigrationStore(db)
	if err != nil {
		return fmt.Errorf("init migration store: %w", err)
	}

	lock := migration.NewSQLiteLock(db)
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	runner := migration.NewMigrationRunner(store, lock, logger)

	// Discover providers — in a real setup these come from the engine's registered modules.
	// For now we use an empty set. When modules implement SchemaProvider, they'll be
	// collected here via engine discovery.
	providers := discoverSchemaProviders()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	switch subcmd {
	case "status":
		return migrateStatus(ctx, runner, providers)
	case "diff":
		return migrateDiff(ctx, runner, providers)
	case "apply":
		return migrateApply(ctx, runner, db, providers)
	default:
		fs.Usage()
		return fmt.Errorf("unknown subcommand: %s", subcmd)
	}
}

func migrateStatus(ctx context.Context, runner *migration.MigrationRunner, providers []migration.SchemaProvider) error {
	if len(providers) == 0 {
		fmt.Println("No schema providers registered.")
		return nil
	}

	status, err := runner.Status(ctx, providers...)
	if err != nil {
		return err
	}

	for _, p := range providers {
		name := p.SchemaName()
		applied := status[name]
		fmt.Printf("\nSchema: %s (target: v%d)\n", name, p.SchemaVersion())
		if len(applied) == 0 {
			fmt.Println("  No migrations applied.")
		} else {
			fmt.Println("  Applied:")
			for _, a := range applied {
				fmt.Printf("    v%d  checksum=%s  applied_at=%s\n",
					a.Version, a.Checksum, a.AppliedAt.Format(time.RFC3339))
			}
		}
	}

	// Show pending.
	pending, err := runner.Pending(ctx, providers...)
	if err != nil {
		return err
	}
	if len(pending) > 0 {
		fmt.Printf("\nPending: %d migration(s)\n", len(pending))
		for _, p := range pending {
			fmt.Printf("  %s: v%d -> v%d\n", p.SchemaName, p.FromVersion, p.ToVersion)
		}
	} else {
		fmt.Println("\nAll schemas up to date.")
	}

	return nil
}

func migrateDiff(ctx context.Context, runner *migration.MigrationRunner, providers []migration.SchemaProvider) error {
	if len(providers) == 0 {
		fmt.Println("No schema providers registered.")
		return nil
	}

	pending, err := runner.Pending(ctx, providers...)
	if err != nil {
		return err
	}

	if len(pending) == 0 {
		fmt.Println("No pending migrations.")
		return nil
	}

	fmt.Printf("%d pending migration(s):\n", len(pending))
	for _, p := range pending {
		fmt.Printf("\n-- %s: v%d -> v%d\n", p.SchemaName, p.FromVersion, p.ToVersion)
		fmt.Println(strings.TrimSpace(p.UpSQL))
		fmt.Println()
	}

	return nil
}

func migrateApply(ctx context.Context, runner *migration.MigrationRunner, db *sql.DB, providers []migration.SchemaProvider) error {
	if len(providers) == 0 {
		fmt.Println("No schema providers registered.")
		return nil
	}

	// Show what will be applied.
	pending, err := runner.Pending(ctx, providers...)
	if err != nil {
		return err
	}

	if len(pending) == 0 {
		fmt.Println("No pending migrations.")
		return nil
	}

	fmt.Printf("Applying %d migration(s)...\n", len(pending))
	if err := runner.Run(ctx, db, providers...); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}

	fmt.Println("Migrations applied successfully.")
	return nil
}

// discoverSchemaProviders returns all SchemaProvider instances from registered modules.
// This is a placeholder that will be populated when modules implement SchemaProvider.
func discoverSchemaProviders() []migration.SchemaProvider {
	return nil
}
