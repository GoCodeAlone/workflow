package main

import (
	"context"
	"database/sql"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/migration"

	_ "modernc.org/sqlite"
)

// TODO(v0.21+): reclaim wfctl migrate namespace for app DB migrations
// via workflow-migrate integration. The current wfctl migrate handler
// moves permanently to `wfctl config migrate`.

// runConfig is the wfctl config command dispatcher. It groups
// engine-config-domain subcommands under a single namespace, starting with
// `wfctl config migrate` (formerly `wfctl migrate`).
func runConfig(args []string) error {
	if len(args) < 1 {
		fmt.Fprintf(flag.CommandLine.Output(), `Usage: wfctl config <subcommand> [options]

Manage engine configuration.

Subcommands:
  validate  Validate wfctl.yaml and .wfctl-lock.yaml project config files
  migrate   Manage engine config database schema migrations
            (replaces the deprecated wfctl migrate command)

`)
		return fmt.Errorf("missing or unknown subcommand")
	}
	switch args[0] {
	case "validate":
		return runConfigValidate(args[1:])
	case "migrate":
		return runConfigMigrate(args[1:])
	default:
		return fmt.Errorf("unknown wfctl config subcommand %q (available: validate, migrate)", args[0])
	}
}

// migrateDeprecationWriter is the io.Writer that receives the deprecation
// banner from runMigrateDeprecated. Defaults to os.Stderr; overridden in tests.
var (
	defaultMigrateDeprecationWriter io.Writer = os.Stderr
	migrateDeprecationWriter        io.Writer = defaultMigrateDeprecationWriter
)

// runMigrateDeprecated is the legacy wfctl migrate dispatcher. It prints a
// one-time deprecation notice then delegates to runConfigMigrate.
func runMigrateDeprecated(args []string) error {
	fmt.Fprintln(migrateDeprecationWriter,
		"wfctl migrate is being renamed to wfctl config migrate "+
			"(engine config migration is config-domain). "+
			"The old form is supported for one release; please update your scripts.")
	return runConfigMigrate(args)
}

// runConfigMigrate is the canonical wfctl config migrate handler.
// It manages engine config database schema migrations.
func runConfigMigrate(args []string) error {
	fs := flag.NewFlagSet("config migrate", flag.ContinueOnError)
	dbPath := fs.String("db", "workflow.db", "Path to SQLite database file")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl config migrate <subcommand> [options]

Manage engine config database schema migrations.

Subcommands:
  status    Show applied and pending migrations
  diff      Show pending migrations without applying them
  apply     Apply pending migrations
  plugins   Migrate requires.plugins[] from app.yaml → wfctl.yaml + .wfctl-lock.yaml
  repair-dirty
            Repair a known dirty golang-migrate metadata state via an IaC provider job

Examples:
  wfctl config migrate status --db workflow.db
  wfctl config migrate diff --db workflow.db
  wfctl config migrate apply --db workflow.db
  wfctl config migrate plugins --config workflow.yaml
  wfctl config migrate repair-dirty --config infra.yaml --env staging --database db --app app --job-image image --expected-dirty-version 20260426000005 --force-version 20260422000001 --confirm-force FORCE_MIGRATION_METADATA

Note: wfctl migrate is a deprecated alias for wfctl config migrate.

Options:
`)
		fs.PrintDefaults()
	}

	if len(args) == 0 {
		fs.Usage()
		return fmt.Errorf("subcommand required: status, diff, apply, plugins, or repair-dirty")
	}

	subcmd := args[0]

	// Handle non-DB subcommands before opening the database.
	if subcmd == "plugins" {
		return runMigratePlugins(args[1:])
	}
	if subcmd == "repair-dirty" {
		return runMigrateRepairDirty(args[1:])
	}

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

// runMigrate is a package-level alias for runConfigMigrate retained for
// backward compatibility with existing tests and internal callers.
// Production use of wfctl migrate routes through runMigrateDeprecated.
func runMigrate(args []string) error { return runConfigMigrate(args) }

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
