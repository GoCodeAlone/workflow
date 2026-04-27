package main

import (
	"context"
	"encoding/json"
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
		"materialize origin/main migrations",
		"run test /tmp/baseline/migrations",
		"cleanup /tmp/baseline/migrations",
		"materialize HEAD migrations",
		"run up /tmp/candidate/migrations",
		"run status /tmp/candidate/migrations",
		"cleanup /tmp/candidate/migrations",
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
	if strings.Contains(string(data), "postgres://secret@example/db") {
		t.Fatal("result file leaked DSN")
	}
}

func stubMigrationBaselineHooks(t *testing.T, calls *[]string, changedFiles []string, commit string) func() {
	t.Helper()
	oldRunner := newMigrationPluginRunner
	newMigrationPluginRunner = func() migrationPluginRunner {
		return migrationPluginRunner{
			exec: func(_ context.Context, _ string, args []string, _ map[string]string) (migrationCommandResult, error) {
				command := args[2]
				sourceDir := argValue(args, "--source-dir")
				*calls = append(*calls, "run "+command+" "+sourceDir)
				if command == "status" {
					return migrationCommandResult{Stdout: `{"dirty":false,"pending":[]}`}, nil
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

	return func() {
		newMigrationPluginRunner = oldRunner
		migrationGitOps = oldGit
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
