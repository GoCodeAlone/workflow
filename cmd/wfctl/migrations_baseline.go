package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type migrationGitOperations struct {
	ChangedFiles      func(ctx context.Context, baselineRef, candidateRef string) ([]string, error)
	MaterializeSource func(ctx context.Context, ref, sourceDir string) (string, func(), error)
	CurrentCommit     func(ctx context.Context) (string, error)
}

var migrationGitOps = migrationGitOperations{}

type migrationEphemeralDatabaseOperations struct {
	Create func(ctx context.Context, name, baseDSN string) (string, func(), error)
}

var migrationEphemeralDB = migrationEphemeralDatabaseOperations{}

type migrationBaselineCandidateResult struct {
	Dirty   bool
	Pending []string
}

func (ops migrationGitOperations) withDefaults() migrationGitOperations {
	if ops.ChangedFiles == nil {
		ops.ChangedFiles = defaultMigrationChangedFiles
	}
	if ops.MaterializeSource == nil {
		ops.MaterializeSource = defaultMigrationMaterializeSource
	}
	if ops.CurrentCommit == nil {
		ops.CurrentCommit = defaultMigrationCurrentCommit
	}
	return ops
}

func hasBaselineCandidateValidation(migrations []resolvedMigrationConfig) bool {
	for _, migration := range migrations {
		if migration.Validation.BaselineCandidate {
			return true
		}
	}
	return false
}

func shouldRunBaselineCandidateValidation(ctx context.Context, gitOps migrationGitOperations, migration resolvedMigrationConfig, candidateRef string, force bool) (string, bool, error) {
	baselineRef := migration.BaselineRef
	if baselineRef == "" {
		baselineRef = "origin/main"
	}
	if candidateRef == "" {
		candidateRef = "HEAD"
	}

	changedFiles, err := gitOps.ChangedFiles(ctx, baselineRef, candidateRef)
	if err != nil {
		return "", false, fmt.Errorf("discover changed migration sources: %w", err)
	}
	if !force && !migrationSourceChanged(migration.SourceDir, changedFiles) {
		return baselineRef, false, nil
	}
	return baselineRef, true, nil
}

func runBaselineCandidateValidation(ctx context.Context, runner migrationPluginRunner, gitOps migrationGitOperations, runCfg migrationPluginRunConfig, migration resolvedMigrationConfig, baselineRef string, candidateRef string, keepTemp bool) (migrationBaselineCandidateResult, error) {
	if baselineRef == "" {
		baselineRef = "origin/main"
	}
	if candidateRef == "" {
		candidateRef = "HEAD"
	}
	ephemeralOps := migrationEphemeralDB.withDefaults()
	validationDSN, cleanupDB, err := ephemeralOps.Create(ctx, migration.Name, runCfg.DSN)
	if err != nil {
		return migrationBaselineCandidateResult{}, fmt.Errorf("create ephemeral migration database: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}

	baselineSource, cleanupBaseline, err := gitOps.MaterializeSource(ctx, baselineRef, migration.SourceDir)
	if err != nil {
		return migrationBaselineCandidateResult{}, fmt.Errorf("materialize baseline migration source: %w", err)
	}
	cleanupBaselineDone := false
	if cleanupBaseline != nil && !keepTemp {
		defer func() {
			if !cleanupBaselineDone {
				cleanupBaseline()
			}
		}()
	}

	baselineCfg := runCfg
	baselineCfg.SourceDir = baselineSource
	baselineCfg.DSN = validationDSN
	if _, err := runner.run(ctx, baselineCfg, "test --keep-alive"); err != nil {
		return migrationBaselineCandidateResult{}, err
	}
	if cleanupBaseline != nil && !keepTemp {
		cleanupBaseline()
		cleanupBaselineDone = true
	}

	candidateSource, cleanupCandidate, err := gitOps.MaterializeSource(ctx, candidateRef, migration.SourceDir)
	if err != nil {
		return migrationBaselineCandidateResult{}, fmt.Errorf("materialize candidate migration source: %w", err)
	}
	if cleanupCandidate != nil && !keepTemp {
		defer cleanupCandidate()
	}

	candidateCfg := runCfg
	candidateCfg.SourceDir = candidateSource
	candidateCfg.DSN = validationDSN
	if _, err := runner.run(ctx, candidateCfg, "up"); err != nil {
		return migrationBaselineCandidateResult{}, err
	}
	status, err := runner.run(ctx, candidateCfg, "status")
	if err != nil {
		return migrationBaselineCandidateResult{}, err
	}
	parsedStatus, err := parseMigrationStatus(status.Stdout)
	if err != nil {
		return migrationBaselineCandidateResult{}, err
	}
	if migration.Validation.ForbidDirty {
		if err := requireCleanMigrationStatus(parsedStatus); err != nil {
			return migrationBaselineCandidateResult{}, fmt.Errorf("migration %s status is not clean: %w", migration.Name, err)
		}
	}
	return parsedStatus, nil
}

func migrationSourceChanged(sourceDir string, changedFiles []string) bool {
	sourceDir = strings.Trim(strings.TrimSpace(filepath.ToSlash(sourceDir)), "/")
	for _, changed := range changedFiles {
		changed = strings.Trim(strings.TrimSpace(filepath.ToSlash(changed)), "/")
		if changed == sourceDir || strings.HasPrefix(changed, sourceDir+"/") {
			return true
		}
	}
	return false
}

func parseMigrationStatus(stdout string) (migrationBaselineCandidateResult, error) {
	var status migrationBaselineCandidateResult
	if err := json.Unmarshal([]byte(stdout), &status); err == nil {
		return status, nil
	}
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		switch {
		case line == "":
			continue
		case strings.Contains(strings.ToLower(line), "dirty"):
			status.Dirty = true
		case strings.HasPrefix(line, "Pending:"):
			pending := strings.TrimSpace(strings.TrimPrefix(line, "Pending:"))
			pending = strings.Trim(pending, "[]")
			if pending != "" {
				status.Pending = strings.Fields(pending)
			}
		}
	}
	return status, nil
}

func requireCleanMigrationStatus(status migrationBaselineCandidateResult) error {
	if status.Dirty {
		return fmt.Errorf("dirty")
	}
	if len(status.Pending) > 0 {
		return fmt.Errorf("pending migrations: %s", strings.Join(status.Pending, ", "))
	}
	return nil
}

func (ops migrationEphemeralDatabaseOperations) withDefaults() migrationEphemeralDatabaseOperations {
	if ops.Create == nil {
		ops.Create = defaultMigrationEphemeralDatabase
	}
	return ops
}

func defaultMigrationEphemeralDatabase(ctx context.Context, name, baseDSN string) (string, func(), error) {
	schema := "wfctl_migrations_" + sanitizeMigrationSchemaName(name) + "_" + fmt.Sprintf("%d", time.Now().UnixNano())
	db, err := sql.Open("postgres", baseDSN)
	if err != nil {
		return "", nil, err
	}
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		_ = db.Close()
		return "", nil, err
	}
	cleanup := func() {
		_, _ = db.ExecContext(context.Background(), `DROP SCHEMA IF EXISTS "`+schema+`" CASCADE`)
		_ = db.Close()
	}
	dsn, err := dsnWithSearchPath(baseDSN, schema)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	return dsn, cleanup, nil
}

var migrationSchemaUnsafeChars = regexp.MustCompile(`[^a-zA-Z0-9_]+`)

func sanitizeMigrationSchemaName(name string) string {
	name = migrationSchemaUnsafeChars.ReplaceAllString(name, "_")
	name = strings.Trim(name, "_")
	if name == "" {
		return "app"
	}
	return strings.ToLower(name)
}

func dsnWithSearchPath(rawDSN, schema string) (string, error) {
	u, err := url.Parse(rawDSN)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

func defaultMigrationChangedFiles(ctx context.Context, baselineRef, candidateRef string) ([]string, error) {
	out, err := exec.CommandContext(ctx, "git", "diff", "--name-only", baselineRef+"..."+candidateRef).Output()
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) == 1 && lines[0] == "" {
		return nil, nil
	}
	return lines, nil
}

func defaultMigrationMaterializeSource(ctx context.Context, ref, sourceDir string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "wfctl-migrations-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(tmpDir)
	}

	archive := exec.CommandContext(ctx, "git", "archive", "--format=tar", ref, sourceDir)
	extract := exec.CommandContext(ctx, "tar", "-xf", "-", "-C", tmpDir)
	pipe, err := archive.StdoutPipe()
	if err != nil {
		cleanup()
		return "", nil, err
	}
	extract.Stdin = pipe
	if err := extract.Start(); err != nil {
		cleanup()
		return "", nil, err
	}
	if err := archive.Run(); err != nil {
		cleanup()
		_ = extract.Wait()
		return "", nil, err
	}
	if err := extract.Wait(); err != nil {
		cleanup()
		return "", nil, err
	}
	return filepath.Join(tmpDir, sourceDir), cleanup, nil
}

func defaultMigrationCurrentCommit(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
