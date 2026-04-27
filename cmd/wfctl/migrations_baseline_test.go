package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestRunMigrationsValidateAppliesBaselineBeforeCandidate(t *testing.T) {
	cfgPath := writeMigrationBaselineConfig(t, true)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	var calls []string
	restore := stubMigrationBaselineHooks(t, &calls, []string{"migrations/202604270001_add_users.up.sql"}, "abc123")
	defer restore()

	err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci"})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"discover origin/main HEAD",
		"run lint migrations",
		"ephemeral app postgres://secret@example/db",
		"materialize origin/main migrations",
		"run test --keep-alive /tmp/baseline/migrations postgres://ephemeral/app",
		"cleanup /tmp/baseline/migrations",
		"materialize HEAD migrations",
		"run up /tmp/candidate/migrations postgres://ephemeral/app",
		"run status /tmp/candidate/migrations postgres://ephemeral/app",
		"cleanup /tmp/candidate/migrations",
		"cleanup ephemeral app",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestRunMigrationsValidateDetectsBaselineChangedMigrationSources(t *testing.T) {
	cfgPath := writeMigrationBaselineMultiSourceConfig(t)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	var calls []string
	restore := stubMigrationBaselineHooks(t, &calls, []string{
		"docs/readme.md",
		"migrations/auth/202604270001_roles.up.sql",
	}, "abc123")
	defer restore()

	err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci"})
	if err != nil {
		t.Fatal(err)
	}

	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "run up /tmp/candidate/migrations/auth") {
		t.Fatalf("auth source was not replayed:\n%s", joined)
	}
	if strings.Contains(joined, "migrations/billing") {
		t.Fatalf("unchanged billing source was replayed:\n%s", joined)
	}
}

func TestRunMigrationsValidateSkipsBaselineWhenDisabledOrUnchanged(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		cfgPath := writeMigrationBaselineConfig(t, false)
		t.Setenv("DATABASE_URL", "postgres://secret@example/db")

		var calls []string
		restore := stubMigrationBaselineHooks(t, &calls, []string{"migrations/202604270001_add_users.up.sql"}, "abc123")
		defer restore()

		if err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci"}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(strings.Join(calls, "\n"), "materialize") {
			t.Fatalf("baseline was materialized while disabled: %#v", calls)
		}
	})

	t.Run("unchanged", func(t *testing.T) {
		cfgPath := writeMigrationBaselineConfig(t, true)
		t.Setenv("DATABASE_URL", "postgres://secret@example/db")

		var calls []string
		restore := stubMigrationBaselineHooks(t, &calls, []string{"docs/readme.md"}, "abc123")
		defer restore()

		if err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci"}); err != nil {
			t.Fatal(err)
		}
		joined := strings.Join(calls, "\n")
		if !strings.Contains(joined, "run lint migrations") {
			t.Fatalf("lint was not run for unchanged source: %#v", calls)
		}
		if strings.Contains(joined, "materialize") || strings.Contains(joined, "run up") {
			t.Fatalf("baseline replay was not skipped for unchanged source: %#v", calls)
		}
	})

	t.Run("force", func(t *testing.T) {
		cfgPath := writeMigrationBaselineConfig(t, true)
		t.Setenv("DATABASE_URL", "postgres://secret@example/db")

		var calls []string
		restore := stubMigrationBaselineHooks(t, &calls, []string{"docs/readme.md"}, "abc123")
		defer restore()

		if err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci", "--force-baseline-candidate"}); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(strings.Join(calls, "\n"), "run up /tmp/candidate/migrations") {
			t.Fatalf("forced baseline replay did not run: %#v", calls)
		}
	})
}

func TestRunMigrationsValidateRecordsBaselineResultFileForCommit(t *testing.T) {
	cfgPath := writeMigrationBaselineConfig(t, true)
	resultPath := filepath.Join(t.TempDir(), "result.json")
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	var calls []string
	restore := stubMigrationBaselineHooks(t, &calls, []string{"migrations/202604270001_add_users.up.sql"}, "abc123")
	defer restore()

	err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci", "--result-file", resultPath})
	if err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var got migrationValidationResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode result file: %v\n%s", err, data)
	}
	if got.Commit != "abc123" || got.Decision != "pass" {
		t.Fatalf("unexpected result file: %+v", got)
	}
	if len(got.Migrations) != 1 || got.Migrations[0].BaselineCandidate != "pass" || got.Migrations[0].Dirty {
		t.Fatalf("baseline/candidate status not recorded: %+v", got.Migrations)
	}
	if strings.Contains(string(data), "postgres://secret@example/db") {
		t.Fatal("result file leaked DSN")
	}
}

func TestRunMigrationsValidateWritesFailureResultFile(t *testing.T) {
	cfgPath := writeMigrationBaselineConfig(t, true)
	resultPath := filepath.Join(t.TempDir(), "result.json")
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	var calls []string
	restore := stubMigrationBaselineHooks(t, &calls, []string{"migrations/202604270001_add_users.up.sql"}, "abc123")
	defer restore()
	migrationEphemeralDB = migrationEphemeralDatabaseOperations{
		Create: func(context.Context, string, string) (string, func(), error) {
			return "", nil, errors.New("ephemeral failed for postgres://secret@example/db")
		},
	}

	err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci", "--result-file", resultPath})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if strings.Contains(err.Error(), "postgres://secret@example/db") {
		t.Fatalf("returned error leaked DSN: %v", err)
	}
	data, readErr := os.ReadFile(resultPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(data), "postgres://secret@example/db") {
		t.Fatal("failure result file leaked DSN")
	}
	var got migrationValidationResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode result file: %v\n%s", err, data)
	}
	if got.Decision != "fail" || len(got.Migrations) != 1 || got.Migrations[0].BaselineCandidate != "fail" {
		t.Fatalf("unexpected failure result: %+v", got)
	}
	if got.Migrations[0].Error == "" {
		t.Fatalf("failure result missing sanitized error: %+v", got.Migrations[0])
	}
}

func TestParseMigrationStatusRejectsUnknownOutput(t *testing.T) {
	if _, err := parseMigrationStatus("unexpected status output"); err == nil {
		t.Fatal("expected unrecognized status error")
	}
	if _, err := parseMigrationStatus(`{"message":"plugin changed output"}`); err == nil {
		t.Fatal("expected unrecognized JSON status error")
	}
	if _, err := parseMigrationStatus(`{"dirty":false}`); err == nil {
		t.Fatal("expected incomplete JSON status error")
	}
	if _, err := parseMigrationStatus("Current: 202604270001\n"); err == nil {
		t.Fatal("expected incomplete text status error")
	}
	status, err := parseMigrationStatus("Current: 202604270001\nNo pending migrations.\nWARNING: database is in dirty state!\n")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Dirty {
		t.Fatal("expected plugin dirty warning to mark status dirty")
	}
}

func TestMigrationSourceChangedNormalizesDotSlashAndRoot(t *testing.T) {
	if !migrationSourceChanged("./migrations", []string{"migrations/202604270001_add_users.up.sql"}) {
		t.Fatal("expected ./migrations to match git diff path")
	}
	if !migrationSourceChanged(".", []string{"migrations/202604270001_add_users.up.sql"}) {
		t.Fatal("expected repo-root source to match any changed path")
	}
}

func TestRunMigrationsValidateFailsClosedOnDirtyBaselineStatus(t *testing.T) {
	cfgPath := writeMigrationBaselineConfigData(t, `
version: 1
ci:
  migrations:
    - name: app
      source_dir: migrations
      database:
        env: DATABASE_URL
      validation:
        baseline_candidate: true
`)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	var calls []string
	restore := stubMigrationBaselineHooks(t, &calls, []string{"migrations/202604270001_add_users.up.sql"}, "abc123")
	defer restore()
	oldRunner := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(_ context.Context, _ string, args []string, env map[string]string) (migrationCommandResult, error) {
				command := strings.Join(args[2:len(args)-4], " ")
				if command == "status" {
					return migrationCommandResult{Stdout: "Current: 202604270001\nDirty: true\nNo pending migrations.\n"}, nil
				}
				if command != "lint" && env["DATABASE_URL"] == "postgres://secret@example/db" {
					t.Fatalf("validation used configured DSN for %s", command)
				}
				return migrationCommandResult{}, nil
			},
		}
	}
	defer func() { newMigrationPluginRunner = oldRunner }()

	err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci"})
	if err == nil {
		t.Fatal("expected dirty validation to fail")
	}
	if !strings.Contains(err.Error(), "dirty") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMigrationsValidateWritesFailureResultFileForDiffErrors(t *testing.T) {
	cfgPath := writeMigrationBaselineConfig(t, true)
	resultPath := filepath.Join(t.TempDir(), "result.json")
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	var calls []string
	restore := stubMigrationBaselineHooks(t, &calls, []string{}, "abc123")
	defer restore()
	migrationGitOps.ChangedFiles = func(context.Context, string, string) ([]string, error) {
		return nil, errors.New("git diff failed for postgres://secret@example/db")
	}

	err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci", "--result-file", resultPath})
	if err == nil {
		t.Fatal("expected validation error")
	}
	if strings.Contains(err.Error(), "postgres://secret@example/db") {
		t.Fatalf("returned error leaked DSN: %v", err)
	}
	data, readErr := os.ReadFile(resultPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if strings.Contains(string(data), "postgres://secret@example/db") {
		t.Fatal("failure result file leaked DSN")
	}
	var got migrationValidationResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode result file: %v\n%s", err, data)
	}
	if got.Decision != "fail" || len(got.Migrations) != 1 || got.Migrations[0].BaselineCandidate != "fail" {
		t.Fatalf("unexpected failure result: %+v", got)
	}
}

func TestMaterializeBaselineSourceOnlyFallsBackWhenSourceMissing(t *testing.T) {
	missingOps := migrationGitOperations{MaterializeSource: func(context.Context, string, string) (string, func(), error) {
		return "", nil, errMigrationSourceMissing
	}}
	source, cleanup, err := materializeBaselineSource(context.Background(), missingOps, "origin/main", "migrations/new")
	if err != nil {
		t.Fatal(err)
	}
	if cleanup == nil {
		t.Fatal("expected cleanup for empty source")
	}
	defer cleanup()
	if !strings.HasSuffix(filepath.ToSlash(source), "migrations/new") {
		t.Fatalf("unexpected empty source path: %s", source)
	}

	failingOps := migrationGitOperations{MaterializeSource: func(context.Context, string, string) (string, func(), error) {
		return "", nil, errors.New("bad ref")
	}}
	if _, _, err := materializeBaselineSource(context.Background(), failingOps, "bad-ref", "migrations"); err == nil {
		t.Fatal("expected non-missing materialization error")
	}
}

func TestRunMigrationsValidateUsesEphemeralDSNForBaselineCandidate(t *testing.T) {
	cfgPath := writeMigrationBaselineConfig(t, true)
	t.Setenv("DATABASE_URL", "postgres://real-db.example/app")

	var calls []string
	restore := stubMigrationBaselineHooks(t, &calls, []string{"migrations/202604270001_add_users.up.sql"}, "abc123")
	defer restore()

	if err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci"}); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(calls, "\n")
	if strings.Contains(joined, "run up /tmp/candidate/migrations postgres://real-db.example/app") ||
		strings.Contains(joined, "run status /tmp/candidate/migrations postgres://real-db.example/app") {
		t.Fatalf("baseline/candidate replay used configured DSN:\n%s", joined)
	}
	if !strings.Contains(joined, "run up /tmp/candidate/migrations postgres://ephemeral/app") {
		t.Fatalf("candidate replay did not use ephemeral DSN:\n%s", joined)
	}
}

func stubMigrationBaselineHooks(t *testing.T, calls *[]string, changedFiles []string, commit string) func() {
	t.Helper()
	oldRunner := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(_ context.Context, _ string, args []string, env map[string]string) (migrationCommandResult, error) {
				command := strings.Join(args[2:len(args)-4], " ")
				sourceDir := argValue(args, "--source-dir")
				call := "run " + command + " " + sourceDir
				if command != "lint" {
					call += " " + env["DATABASE_URL"]
				}
				*calls = append(*calls, call)
				if strings.HasPrefix(command, "status") {
					return migrationCommandResult{Stdout: "Current: 202604270001\nNo pending migrations.\n"}, nil
				}
				return migrationCommandResult{}, nil
			},
		}
	}

	oldGit := migrationGitOps
	migrationGitOps = migrationGitOperations{
		ChangedFiles: func(_ context.Context, baselineRef, candidateRef string) ([]string, error) {
			*calls = append(*calls, "discover "+baselineRef+" "+candidateRef)
			return changedFiles, nil
		},
		MaterializeSource: func(_ context.Context, ref, sourceDir string) (string, func(), error) {
			*calls = append(*calls, "materialize "+ref+" "+sourceDir)
			root := "/tmp/candidate"
			if ref == "origin/main" {
				root = "/tmp/baseline"
			}
			materialized := filepath.Join(root, sourceDir)
			return materialized, func() {
				*calls = append(*calls, "cleanup "+materialized)
			}, nil
		},
		CurrentCommit: func(_ context.Context) (string, error) {
			return commit, nil
		},
	}
	oldEphemeral := migrationEphemeralDB
	migrationEphemeralDB = migrationEphemeralDatabaseOperations{
		Create: func(_ context.Context, name, baseDSN string) (string, func(), error) {
			*calls = append(*calls, "ephemeral "+name+" "+baseDSN)
			return "postgres://ephemeral/" + name, func() {
				*calls = append(*calls, "cleanup ephemeral "+name)
			}, nil
		},
	}

	return func() {
		newMigrationPluginRunner = oldRunner
		migrationGitOps = oldGit
		migrationEphemeralDB = oldEphemeral
	}
}

func writeMigrationBaselineConfig(t *testing.T, baselineCandidate bool) string {
	t.Helper()
	return writeMigrationBaselineConfigData(t, `
version: 1
ci:
  migrations:
    - name: app
      source_dir: migrations
      database:
        env: DATABASE_URL
      validation:
        lint: true
        baseline_candidate: `+boolYAML(baselineCandidate)+`
        forbid_dirty: true
`)
}

func writeMigrationBaselineMultiSourceConfig(t *testing.T) string {
	t.Helper()
	return writeMigrationBaselineConfigData(t, `
version: 1
ci:
  migrations:
    - name: auth
      source_dir: migrations/auth
      database:
        env: DATABASE_URL
      validation:
        baseline_candidate: true
        forbid_dirty: true
    - name: billing
      source_dir: migrations/billing
      database:
        env: DATABASE_URL
      validation:
        baseline_candidate: true
        forbid_dirty: true
`)
}

func writeMigrationBaselineConfigData(t *testing.T, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "infra.yaml")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func boolYAML(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func argValue(args []string, name string) string {
	for i := range args {
		if args[i] == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}
