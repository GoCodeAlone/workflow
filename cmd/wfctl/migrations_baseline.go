package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type migrationGitOperations struct {
	ChangedFiles      func(ctx context.Context, baselineRef, candidateRef string) ([]string, error)
	MaterializeSource func(ctx context.Context, ref, sourceDir string) (string, func(), error)
	CurrentCommit     func(ctx context.Context) (string, error)
}

var migrationGitOps = migrationGitOperations{}

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

func runBaselineCandidateValidation(ctx context.Context, runner migrationPluginRunner, gitOps migrationGitOperations, runCfg migrationPluginRunConfig, migration resolvedMigrationConfig, baselineRef string, candidateRef string, keepTemp bool) error {
	if baselineRef == "" {
		baselineRef = "origin/main"
	}
	if candidateRef == "" {
		candidateRef = "HEAD"
	}
	baselineSource, cleanupBaseline, err := gitOps.MaterializeSource(ctx, baselineRef, migration.SourceDir)
	if err != nil {
		return fmt.Errorf("materialize baseline migration source: %w", err)
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
	if _, err := runner.run(ctx, baselineCfg, "test"); err != nil {
		return err
	}
	if cleanupBaseline != nil && !keepTemp {
		cleanupBaseline()
		cleanupBaselineDone = true
	}

	candidateSource, cleanupCandidate, err := gitOps.MaterializeSource(ctx, candidateRef, migration.SourceDir)
	if err != nil {
		return fmt.Errorf("materialize candidate migration source: %w", err)
	}
	if cleanupCandidate != nil && !keepTemp {
		defer cleanupCandidate()
	}

	candidateCfg := runCfg
	candidateCfg.SourceDir = candidateSource
	if _, err := runner.run(ctx, candidateCfg, "up"); err != nil {
		return err
	}
	status, err := runner.run(ctx, candidateCfg, "status")
	if err != nil {
		return err
	}
	if migration.Validation.ForbidDirty {
		if err := requireCleanMigrationStatus(status.Stdout); err != nil {
			return fmt.Errorf("migration %s status is not clean: %w", migration.Name, err)
		}
	}
	return nil
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

func requireCleanMigrationStatus(stdout string) error {
	var status struct {
		Dirty   bool     `json:"dirty"`
		Pending []string `json:"pending"`
	}
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		return fmt.Errorf("decode status JSON: %w", err)
	}
	if status.Dirty {
		return fmt.Errorf("dirty")
	}
	if len(status.Pending) > 0 {
		return fmt.Errorf("pending migrations: %s", strings.Join(status.Pending, ", "))
	}
	return nil
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
