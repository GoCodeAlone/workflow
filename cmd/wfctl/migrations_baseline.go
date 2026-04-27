package main

import (
	"archive/tar"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	neturl "net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

type migrationGitOperations struct {
	ChangedFiles      func(ctx context.Context, baselineRef, candidateRef string) ([]string, error)
	MaterializeSource func(ctx context.Context, ref, sourceDir string) (string, func(), error)
	CurrentCommit     func(ctx context.Context) (string, error)
}

var migrationGitOps = migrationGitOperations{}

var errMigrationSourceMissing = errors.New("migration source missing at ref")

type migrationEphemeralDatabaseOperations struct {
	Create func(ctx context.Context, name, baseDSN string) (string, func(), error)
}

var migrationEphemeralDB = migrationEphemeralDatabaseOperations{}

const maxMigrationArchiveFileBytes = 64 << 20

type migrationBaselineCandidateResult struct {
	Current string   `json:"current"`
	Dirty   bool     `json:"dirty"`
	Pending []string `json:"pending"`
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

	baselineSource, cleanupBaseline, err := materializeBaselineSource(ctx, gitOps, baselineRef, migration.SourceDir)
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
	if err := requireCleanMigrationStatus(parsedStatus); err != nil {
		return migrationBaselineCandidateResult{}, fmt.Errorf("migration %s status is not clean: %w", migration.Name, err)
	}
	return parsedStatus, nil
}

func migrationSourceChanged(sourceDir string, changedFiles []string) bool {
	sourceDir = normalizeMigrationSourceDir(sourceDir)
	if sourceDir == "." {
		return len(changedFiles) > 0
	}
	for _, changed := range changedFiles {
		changed = strings.Trim(strings.TrimSpace(filepath.ToSlash(changed)), "/")
		if changed == sourceDir || strings.HasPrefix(changed, sourceDir+"/") {
			return true
		}
	}
	return false
}

func normalizeMigrationSourceDir(sourceDir string) string {
	sourceDir = strings.TrimSpace(filepath.ToSlash(sourceDir))
	sourceDir = strings.TrimPrefix(sourceDir, "./")
	sourceDir = strings.Trim(sourceDir, "/")
	if sourceDir == "" || sourceDir == "." {
		return "."
	}
	return sourceDir
}

func parseMigrationStatus(stdout string) (migrationBaselineCandidateResult, error) {
	var status migrationBaselineCandidateResult
	if strings.HasPrefix(strings.TrimSpace(stdout), "{") {
		var raw map[string]json.RawMessage
		if err := json.Unmarshal([]byte(stdout), &raw); err != nil {
			return migrationBaselineCandidateResult{}, fmt.Errorf("decode status JSON: %w", err)
		}
		if !jsonStatusHas(raw, "current") || !jsonStatusHas(raw, "dirty") || !jsonStatusHas(raw, "pending") {
			return migrationBaselineCandidateResult{}, fmt.Errorf("incomplete migration status JSON")
		}
		if err := json.Unmarshal([]byte(stdout), &status); err != nil {
			return migrationBaselineCandidateResult{}, fmt.Errorf("decode status JSON: %w", err)
		}
		return status, nil
	}
	recognized := false
	currentSeen := false
	pendingSeen := false
	dirtySeen := false
	for _, line := range strings.Split(stdout, "\n") {
		line = strings.TrimSpace(line)
		lowerLine := strings.ToLower(line)
		switch {
		case line == "":
			continue
		case strings.HasPrefix(lowerLine, "dirty:"):
			recognized = true
			dirtySeen = true
			status.Dirty = strings.Contains(lowerLine, "true") || strings.Contains(lowerLine, "yes")
		case strings.Contains(lowerLine, "dirty state"):
			recognized = true
			dirtySeen = true
			status.Dirty = true
		case strings.HasPrefix(line, "Current:"):
			recognized = true
			currentSeen = true
			status.Current = strings.TrimSpace(strings.TrimPrefix(line, "Current:"))
		case strings.HasPrefix(line, "Pending:"):
			recognized = true
			pendingSeen = true
			pending := strings.TrimSpace(strings.TrimPrefix(line, "Pending:"))
			pending = strings.Trim(pending, "[]")
			if pending != "" {
				status.Pending = strings.Fields(pending)
			}
		case line == "No pending migrations." || line == "No migrations applied.":
			recognized = true
			pendingSeen = true
			if line == "No migrations applied." {
				currentSeen = true
			}
		}
	}
	if !recognized {
		return migrationBaselineCandidateResult{}, fmt.Errorf("unrecognized migration status output")
	}
	if !currentSeen || !pendingSeen || !dirtySeen {
		return migrationBaselineCandidateResult{}, fmt.Errorf("incomplete migration status output")
	}
	return status, nil
}

func jsonStatusHas(raw map[string]json.RawMessage, key string) bool {
	if _, ok := raw[key]; ok {
		return true
	}
	titleKey := strings.ToUpper(key[:1]) + key[1:]
	_, ok := raw[titleKey]
	return ok
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

func runFreshCycleValidation(ctx context.Context, runner migrationPluginRunner, runCfg migrationPluginRunConfig, migration resolvedMigrationConfig) error {
	ephemeralOps := migrationEphemeralDB.withDefaults()
	validationDSN, cleanupDB, err := ephemeralOps.Create(ctx, migration.Name+"-fresh", runCfg.DSN)
	if err != nil {
		return fmt.Errorf("create ephemeral migration database: %w", err)
	}
	if cleanupDB != nil {
		defer cleanupDB()
	}
	freshCfg := runCfg
	freshCfg.DSN = validationDSN
	_, err = runner.run(ctx, freshCfg, "test")
	return err
}

func (ops migrationEphemeralDatabaseOperations) withDefaults() migrationEphemeralDatabaseOperations {
	if ops.Create == nil {
		ops.Create = defaultMigrationEphemeralDatabase
	}
	return ops
}

func defaultMigrationEphemeralDatabase(ctx context.Context, name, baseDSN string) (string, func(), error) {
	adminDSN := os.Getenv("WFCTL_MIGRATION_VALIDATION_DATABASE_URL")
	if adminDSN == "" {
		return "", nil, fmt.Errorf("WFCTL_MIGRATION_VALIDATION_DATABASE_URL is required for migration validation")
	}
	validationURL, err := neturl.Parse(adminDSN)
	if err != nil {
		return "", nil, fmt.Errorf("parse validation database URL: %w", err)
	}
	if validationURL.Scheme != "postgres" && validationURL.Scheme != "postgresql" {
		return "", nil, fmt.Errorf("WFCTL_MIGRATION_VALIDATION_DATABASE_URL must be a postgres URL")
	}
	dbName := migrationValidationDatabaseName(name)
	db, err := sql.Open("postgres", adminDSN)
	if err != nil {
		return "", nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return "", nil, fmt.Errorf("connect validation database: %w", err)
	}
	if _, err := db.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(dbName)); err != nil {
		_ = db.Close()
		return "", nil, fmt.Errorf("create validation database: %w", err)
	}
	validationURL.Path = "/" + dbName
	validationDSN := validationURL.String()
	cleanup := func() {
		_, _ = db.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(dbName))
		_ = db.Close()
	}
	return validationDSN, cleanup, nil
}

func migrationValidationDatabaseName(name string) string {
	var b strings.Builder
	b.WriteString("wfctl_migrations_")
	for _, r := range strings.ToLower(name) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		b.WriteRune('_')
	}
	b.WriteString("_")
	b.WriteString(strconv.FormatInt(time.Now().UnixNano(), 10))
	return b.String()
}

func defaultMigrationChangedFiles(ctx context.Context, baselineRef, candidateRef string) ([]string, error) {
	// #nosec G204 -- git is a fixed executable and refs are passed as argv without shell interpolation.
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

	// #nosec G204 -- git is a fixed executable and refs/source paths are passed as argv without shell interpolation.
	out, err := exec.CommandContext(ctx, "git", "archive", "--format=tar", ref, sourceDir).CombinedOutput()
	if err != nil {
		cleanup()
		if isMissingMigrationSourceArchiveError(string(out)) {
			return "", nil, errMigrationSourceMissing
		}
		return "", nil, err
	}
	if err := extractTar(bytes.NewReader(out), tmpDir); err != nil {
		cleanup()
		return "", nil, err
	}
	return filepath.Join(tmpDir, sourceDir), cleanup, nil
}

func isMissingMigrationSourceArchiveError(output string) bool {
	output = strings.ToLower(output)
	return strings.Contains(output, "pathspec") && strings.Contains(output, "did not match")
}

func materializeBaselineSource(ctx context.Context, gitOps migrationGitOperations, ref, sourceDir string) (string, func(), error) {
	source, cleanup, err := gitOps.MaterializeSource(ctx, ref, sourceDir)
	if err == nil {
		return source, cleanup, nil
	}
	if !errors.Is(err, errMigrationSourceMissing) {
		return "", nil, err
	}
	return emptyMigrationSource(sourceDir)
}

func emptyMigrationSource(sourceDir string) (string, func(), error) {
	tmpDir, err := os.MkdirTemp("", "wfctl-migrations-empty-*")
	if err != nil {
		return "", nil, err
	}
	source := filepath.Join(tmpDir, sourceDir)
	if err := os.MkdirAll(source, 0o750); err != nil {
		_ = os.RemoveAll(tmpDir)
		return "", nil, err
	}
	return source, func() { _ = os.RemoveAll(tmpDir) }, nil
}

func extractTar(r *bytes.Reader, dest string) error {
	tr := tar.NewReader(r)
	for {
		header, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		// #nosec G305 -- the joined path is cleaned and checked against dest before use.
		target := filepath.Join(dest, header.Name)
		cleanDest := filepath.Clean(dest) + string(os.PathSeparator)
		cleanTarget := filepath.Clean(target)
		if !strings.HasPrefix(cleanTarget, cleanDest) && cleanTarget != filepath.Clean(dest) {
			return fmt.Errorf("archive entry escapes destination: %s", header.Name)
		}
		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o750); err != nil {
				return err
			}
		case tar.TypeReg:
			if header.Size > maxMigrationArchiveFileBytes {
				return fmt.Errorf("archive entry exceeds size limit: %s", header.Name)
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return err
			}
			file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
			if err != nil {
				return err
			}
			if _, err := io.CopyN(file, tr, header.Size); err != nil {
				_ = file.Close()
				return err
			}
			if err := file.Close(); err != nil {
				return err
			}
		}
	}
}

func defaultMigrationCurrentCommit(ctx context.Context) (string, error) {
	out, err := exec.CommandContext(ctx, "git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
