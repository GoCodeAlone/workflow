package main

import (
	"archive/tar"
	"bytes"
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

func TestRunMigrationsValidateRecordsSkippedBaselineCandidateWhenSourceUnchanged(t *testing.T) {
	cfgPath := writeMigrationBaselineConfig(t, true)
	resultPath := filepath.Join(t.TempDir(), "result.json")
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	var calls []string
	restore := stubMigrationBaselineHooks(t, &calls, nil, "abc123")
	defer restore()

	if err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci", "--result-file", resultPath}); err != nil {
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
	if got.Decision != "pass" || len(got.Migrations) != 1 || got.Migrations[0].BaselineCandidate != "skip" {
		t.Fatalf("unexpected skipped baseline result: %+v", got)
	}
	if strings.Contains(strings.Join(calls, "\n"), "materialize") {
		t.Fatalf("baseline/candidate replay should not materialize unchanged sources: %v", calls)
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
	if _, err := parseMigrationStatus("Current: 202604270001\nNo pending migrations.\n"); err == nil {
		t.Fatal("expected missing dirty status error")
	}
	status, err := parseMigrationStatus("Current: 202604270001\nNo pending migrations.\nWARNING: database is in dirty state!\n")
	if err != nil {
		t.Fatal(err)
	}
	if !status.Dirty {
		t.Fatal("expected plugin dirty warning to mark status dirty")
	}
}

func TestParseMigrationStatusFreshDatabaseNoMigrationsApplied(t *testing.T) {
	// A fresh database with no migrations applied cannot be dirty. Drivers such as
	// atlas omit the "Dirty:" line in this case; parseMigrationStatus must not
	// require it when "No migrations applied." is the reported state.
	status, err := parseMigrationStatus("No migrations applied.\n")
	if err != nil {
		t.Fatalf("fresh-database status without Dirty line: %v", err)
	}
	if status.Dirty {
		t.Fatal("fresh-database status: expected dirty=false")
	}
	if status.Current != "" {
		t.Fatalf("fresh-database status: expected empty current, got %q", status.Current)
	}
	if len(status.Pending) != 0 {
		t.Fatalf("fresh-database status: expected no pending, got %v", status.Pending)
	}

	// Explicit "Dirty: false" alongside "No migrations applied." must also work.
	status, err = parseMigrationStatus("No migrations applied.\nDirty: false\n")
	if err != nil {
		t.Fatalf("fresh-database status with explicit Dirty: false: %v", err)
	}
	if status.Dirty {
		t.Fatal("expected dirty=false")
	}

	// "Dirty: true" with "No migrations applied." is unusual but must be respected
	// when the driver explicitly signals it (edge case: corrupted revisions table).
	status, err = parseMigrationStatus("No migrations applied.\nDirty: true\n")
	if err != nil {
		t.Fatalf("fresh-database status with explicit Dirty: true: %v", err)
	}
	if !status.Dirty {
		t.Fatal("expected dirty=true when driver explicitly reports it")
	}

	// JSON format: fresh database with empty current and no pending.
	status, err = parseMigrationStatus(`{"current":"","dirty":false,"pending":[]}`)
	if err != nil {
		t.Fatalf("fresh-database JSON status: %v", err)
	}
	if status.Dirty || status.Current != "" || len(status.Pending) != 0 {
		t.Fatalf("unexpected fresh-database JSON status: %+v", status)
	}

	// JSON format: null pending (some drivers emit null instead of []).
	status, err = parseMigrationStatus(`{"current":"","dirty":false,"pending":null}`)
	if err != nil {
		t.Fatalf("fresh-database JSON status with null pending: %v", err)
	}
	if status.Dirty || status.Current != "" || len(status.Pending) != 0 {
		t.Fatalf("unexpected fresh-database JSON status (null pending): %+v", status)
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
        forbid_dirty: true
`)
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	var calls []string
	restore := stubMigrationBaselineHooks(t, &calls, []string{"migrations/202604270001_add_users.up.sql"}, "abc123")
	defer restore()
	oldRunner := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(_ context.Context, _ string, args []string, env map[string]string) (migrationCommandResult, error) {
				command := migrationCommandFromArgs(args)
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

func TestRunMigrationsValidateRecordsDirtyBaselineStatusWhenNotForbidden(t *testing.T) {
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
	resultPath := filepath.Join(t.TempDir(), "result.json")
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	var calls []string
	restore := stubMigrationBaselineHooks(t, &calls, []string{"migrations/202604270001_add_users.up.sql"}, "abc123")
	defer restore()
	oldRunner := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(_ context.Context, _ string, args []string, _ map[string]string) (migrationCommandResult, error) {
				if migrationCommandFromArgs(args) == "status" {
					return migrationCommandResult{Stdout: "Current: 202604270001\nDirty: true\nNo pending migrations.\n"}, nil
				}
				return migrationCommandResult{}, nil
			},
		}
	}
	defer func() { newMigrationPluginRunner = oldRunner }()

	if err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci", "--result-file", resultPath}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var got migrationValidationResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Decision != "pass" || len(got.Migrations) != 1 || !got.Migrations[0].Dirty {
		t.Fatalf("unexpected validation result: %+v", got)
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

func TestExtractTarRejectsTraversalEntry(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	data := []byte("bad")
	if err := tw.WriteHeader(&tar.Header{Name: "../escape.sql", Typeflag: tar.TypeReg, Size: int64(len(data)), Mode: 0o600}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	err := extractTar(bytes.NewReader(buf.Bytes()), t.TempDir())
	if err == nil {
		t.Fatal("expected traversal entry to be rejected")
	}
	if !strings.Contains(err.Error(), "escapes destination") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestExtractTarRejectsCleanedTargetOutsideDestination(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	data := []byte("bad")
	if err := tw.WriteHeader(&tar.Header{Name: "migrations/../../escape.sql", Typeflag: tar.TypeReg, Size: int64(len(data)), Mode: 0o600}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	err := extractTar(bytes.NewReader(buf.Bytes()), t.TempDir())
	if err == nil {
		t.Fatal("expected cleaned traversal target to be rejected")
	}
	if !strings.Contains(err.Error(), "escapes destination") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunMigrationsValidateAtlasFreshDatabaseStatusWithoutDirtyLine(t *testing.T) {
	// Simulates the atlas driver returning "No migrations applied." without a
	// "Dirty:" line after running `up` on a fresh (empty) ephemeral database.
	// This is the scenario surfaced by the atlas executor panic fix: once the
	// binary no longer panics, wfctl must parse the status output correctly.
	cfgPath := writeMigrationBaselineConfigData(t, `
version: 1
ci:
  migrations:
    - name: app
      driver: atlas
      source_dir: migrations
      database:
        env: DATABASE_URL
      validation:
        baseline_candidate: true
`)
	resultPath := filepath.Join(t.TempDir(), "result.json")
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	var calls []string
	restore := stubMigrationBaselineHooks(t, &calls, []string{"migrations/202604270001_add_users.up.sql"}, "abc123")
	defer restore()
	oldRunner := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(_ context.Context, _ string, args []string, _ map[string]string) (migrationCommandResult, error) {
				command := migrationCommandFromArgs(args)
				if command == "status" {
					// Atlas driver output for a fresh database: no "Dirty:" line.
					return migrationCommandResult{Stdout: "No migrations applied.\n"}, nil
				}
				return migrationCommandResult{}, nil
			},
		}
	}
	defer func() { newMigrationPluginRunner = oldRunner }()

	if err := runMigrations([]string{"validate", "--config", cfgPath, "--env", "ci", "--result-file", resultPath}); err != nil {
		t.Fatalf("validation failed for fresh-database atlas status: %v", err)
	}
	data, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	var got migrationValidationResult
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decode result file: %v\n%s", err, data)
	}
	if got.Decision != "pass" || len(got.Migrations) != 1 {
		t.Fatalf("unexpected validation result: %+v", got)
	}
	if got.Migrations[0].BaselineCandidate != "pass" {
		t.Fatalf("baseline_candidate check should pass: %+v", got.Migrations[0])
	}
	if got.Migrations[0].Dirty {
		t.Fatal("fresh database should not be dirty")
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
				command := migrationCommandFromArgs(args)
				sourceDir := migrationSourceFromArgs(args)
				call := "run " + command + " " + sourceDir
				if command != "lint" {
					call += " " + env["DATABASE_URL"]
				}
				*calls = append(*calls, call)
				if strings.HasPrefix(command, "status") {
					return migrationCommandResult{Stdout: "Current: 202604270001\nDirty: false\nNo pending migrations.\n"}, nil
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

func migrationCommandFromArgs(args []string) string {
	if len(args) < 2 {
		return ""
	}
	if args[1] == "lint" {
		return "lint"
	}
	command := []string{args[1]}
	for i := 2; i < len(args); i++ {
		if args[i] == "--driver" || args[i] == "--source-dir" || args[i] == "--dsn" {
			break
		}
		command = append(command, args[i])
	}
	return strings.Join(command, " ")
}

func migrationSourceFromArgs(args []string) string {
	if len(args) >= 3 && args[1] == "lint" {
		return args[2]
	}
	return argValue(args, "--source-dir")
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
