package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

var newMigrationPluginRunner = func() migrationPluginRunner {
	return migrationPluginRunner{}
}

type migrationValidationResult struct {
	Decision   string                      `json:"decision"`
	Commit     string                      `json:"commit,omitempty"`
	Migrations []migrationValidationRecord `json:"migrations"`
	Error      string                      `json:"error,omitempty"`
}

type migrationValidationRecord struct {
	Name              string   `json:"name"`
	Lint              string   `json:"lint,omitempty"`
	FreshCycle        string   `json:"fresh_cycle,omitempty"`
	BaselineCandidate string   `json:"baseline_candidate,omitempty"`
	Driver            string   `json:"driver,omitempty"`
	Current           string   `json:"current,omitempty"`
	Dirty             bool     `json:"dirty"`
	Pending           []string `json:"pending,omitempty"`
	Error             string   `json:"error,omitempty"`
}

type migrationStatusResult struct {
	Decision              string                      `json:"decision"`
	Reasons               []string                    `json:"reasons,omitempty"`
	Destructive           bool                        `json:"destructive"`
	HumanApprovalRequired bool                        `json:"human_approval_required"`
	Migrations            []migrationValidationRecord `json:"migrations"`
}

type migrationRepairDirtyResult struct {
	Decision              string                    `json:"decision"`
	Reasons               []string                  `json:"reasons,omitempty"`
	Destructive           bool                      `json:"destructive"`
	HumanApprovalRequired bool                      `json:"human_approval_required"`
	ApprovalCommand       string                    `json:"approval_command,omitempty"`
	Migration             migrationValidationRecord `json:"migration,omitempty"`
}

func runMigrations(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: wfctl migrations <validate|status|up|ci-check|repair-dirty>")
	}
	switch args[0] {
	case "validate":
		return runMigrationsValidate(args[1:])
	case "status":
		return runMigrationsStatus(args[1:])
	case "up":
		return runMigrationsUp(args[1:])
	case "ci-check":
		return runMigrationsCICheck(args[1:])
	case "repair-dirty":
		return runMigrationsRepairDirty(args[1:])
	default:
		return fmt.Errorf("unknown migrations subcommand %q", args[0])
	}
}

func runMigrationsStatus(args []string) error {
	fs := flag.NewFlagSet("migrations status", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	fs.StringVar(configFile, "c", "app.yaml", "Config file (short for --config)")
	envName := fs.String("env", "", "Environment name")
	pluginDir := fs.String("plugin-dir", defaultMigrationPluginDir(), "Plugin directory")
	format := fs.String("format", "text", "Output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadMigrationWorkflowConfig(*configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	migrations, err := resolveMigrationConfigs(cfg, *envName)
	if err != nil {
		return err
	}
	result, err := collectMigrationStatusForConfigs(context.Background(), migrations, *pluginDir)
	result.Reasons = append(result.Reasons, migrationStatusDirtyReasons(result.Migrations, migrations)...)
	if len(result.Reasons) > 0 {
		result.Decision = "fail"
	}
	if writeErr := writeMigrationStatusOutput(result, *format, "migrations"); writeErr != nil {
		return writeErr
	}
	if err != nil {
		return err
	}
	if result.Decision == "fail" {
		return errors.New(strings.Join(result.Reasons, "; "))
	}
	return nil
}

func runMigrationsUp(args []string) error {
	fs := flag.NewFlagSet("migrations up", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	fs.StringVar(configFile, "c", "app.yaml", "Config file (short for --config)")
	envName := fs.String("env", "", "Environment name")
	pluginDir := fs.String("plugin-dir", defaultMigrationPluginDir(), "Plugin directory")
	format := fs.String("format", "text", "Output format: text or json")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadMigrationWorkflowConfig(*configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	migrations, err := resolveMigrationConfigs(cfg, *envName)
	if err != nil {
		return err
	}
	result, err := applyMigrationsForConfigs(context.Background(), migrations, *pluginDir)
	if writeErr := writeMigrationStatusOutput(result, *format, "migrations up"); writeErr != nil {
		return writeErr
	}
	if err != nil {
		return err
	}
	if result.Decision == "fail" {
		return errors.New(strings.Join(result.Reasons, "; "))
	}
	return nil
}

func runMigrationsCICheck(args []string) error {
	fs := flag.NewFlagSet("migrations ci-check", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	fs.StringVar(configFile, "c", "app.yaml", "Config file (short for --config)")
	envName := fs.String("env", "", "Environment name")
	pluginDir := fs.String("plugin-dir", defaultMigrationPluginDir(), "Plugin directory")
	format := fs.String("format", "text", "Output format: text or json")
	commit := fs.String("commit", "", "Commit SHA to check")
	validationResult := fs.String("validation-result", "", "Validation result JSON from wfctl migrations validate")
	requireValidationResult := fs.Bool("require-validation-result", false, "Require a passing validation result for this commit")
	requireSameSHA := fs.Bool("require-same-sha", false, "Require a passing validation result for the same commit SHA")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadMigrationWorkflowConfig(*configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	migrations, err := resolveMigrationConfigs(cfg, *envName)
	if err != nil {
		return err
	}
	result, statusErr := collectMigrationStatusForConfigs(context.Background(), migrations, *pluginDir)
	result.Reasons = append(result.Reasons, migrationStatusCleanReasons(result.Migrations)...)
	if *requireSameSHA {
		*requireValidationResult = true
	}
	if *requireValidationResult {
		result.Reasons = append(result.Reasons, checkMigrationValidationResult(*validationResult, *commit, *requireSameSHA, migrations)...)
	}
	if statusErr != nil && len(result.Reasons) == 0 {
		result.Reasons = append(result.Reasons, statusErr.Error())
	}
	if len(result.Reasons) > 0 {
		result.Decision = "fail"
	}
	if writeErr := writeMigrationStatusOutput(result, *format, "migrations ci-check"); writeErr != nil {
		return writeErr
	}
	if result.Decision == "fail" {
		return errors.New(strings.Join(result.Reasons, "; "))
	}
	return nil
}

func runMigrationsRepairDirty(args []string) error {
	fs := flag.NewFlagSet("migrations repair-dirty", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	fs.StringVar(configFile, "c", "app.yaml", "Config file (short for --config)")
	envName := fs.String("env", "", "Environment name")
	pluginDir := fs.String("plugin-dir", defaultMigrationPluginDir(), "Plugin directory")
	format := fs.String("format", "text", "Output format: text or json")
	expectedDirtyVersion := fs.String("expected-dirty-version", "", "Exact dirty version expected before repair")
	forceVersion := fs.String("force-version", "", "Version to force migration metadata to")
	confirmForce := fs.String("confirm-force", "", "Typed confirmation token")
	approvedToken := fs.String("approved-token", "", "External approval token for non-dev destructive repair")
	approvedTokenEnv := fs.String("approved-token-env", "", "Environment variable containing the external approval token")
	thenUp := fs.Bool("then-up", false, "Run migrate up after metadata repair")
	upIfClean := fs.Bool("up-if-clean", false, "Run migrate up when the database is already clean")
	if err := fs.Parse(args); err != nil {
		return err
	}

	result := migrationRepairDirtyResult{Decision: "fail", Destructive: true}
	if *confirmForce != "FORCE_MIGRATION_METADATA" {
		result.Reasons = []string{"--confirm-force FORCE_MIGRATION_METADATA is required"}
		return finishMigrationRepairDirty(result, *format)
	}
	if strings.TrimSpace(*expectedDirtyVersion) == "" || strings.TrimSpace(*forceVersion) == "" {
		result.Reasons = []string{"--expected-dirty-version and --force-version are required"}
		return finishMigrationRepairDirty(result, *format)
	}
	if isProtectedMigrationEnvironment(*envName) {
		requiredToken := os.Getenv("WFCTL_MIGRATION_REPAIR_APPROVAL_TOKEN")
		submittedToken := strings.TrimSpace(*approvedToken)
		if submittedToken == "" && strings.TrimSpace(*approvedTokenEnv) != "" {
			submittedToken = os.Getenv(strings.TrimSpace(*approvedTokenEnv))
		}
		if submittedToken == "" || requiredToken == "" || submittedToken != requiredToken {
			result.HumanApprovalRequired = true
			result.Reasons = []string{"human approval is required for production migration metadata repair"}
			result.ApprovalCommand = buildMigrationRepairApprovalCommand(*configFile, *envName, *pluginDir, *expectedDirtyVersion, *forceVersion, *thenUp, *upIfClean)
			return finishMigrationRepairDirty(result, "json")
		}
	}

	cfg, err := loadMigrationWorkflowConfig(*configFile)
	if err != nil {
		result.Reasons = []string{fmt.Sprintf("load config: %v", err)}
		return finishMigrationRepairDirty(result, *format)
	}
	migrations, err := resolveMigrationConfigs(cfg, *envName)
	if err != nil {
		result.Reasons = []string{err.Error()}
		return finishMigrationRepairDirty(result, *format)
	}
	if len(migrations) != 1 {
		result.Reasons = []string{fmt.Sprintf("repair-dirty requires exactly one configured migration, found %d", len(migrations))}
		return finishMigrationRepairDirty(result, *format)
	}

	ctx := context.Background()
	runner := newMigrationPluginRunner()
	migration := migrations[0]
	runCfg := migrationPluginRunConfig{
		Plugin:    migration.Plugin,
		PluginDir: *pluginDir,
		Driver:    migration.Driver,
		SourceDir: migration.SourceDir,
		DSN:       migration.DSN,
	}
	before, err := runner.run(ctx, runCfg, "status")
	if err != nil {
		result.Reasons = []string{fmt.Sprintf("migration %s status failed: %s", migration.Name, redactMigrationDSN(err.Error(), migration.DSN))}
		return finishMigrationRepairDirty(result, *format)
	}
	beforeStatus, err := parseMigrationStatus(before.Stdout)
	if err != nil {
		result.Reasons = []string{fmt.Sprintf("migration %s status failed: %s", migration.Name, err)}
		return finishMigrationRepairDirty(result, *format)
	}
	if !beforeStatus.Dirty {
		if *upIfClean {
			if _, err := runner.run(ctx, runCfg, "up"); err != nil {
				result.Reasons = []string{fmt.Sprintf("migration %s up failed: %s", migration.Name, redactMigrationDSN(err.Error(), migration.DSN))}
				return finishMigrationRepairDirty(result, *format)
			}
			after, err := runner.run(ctx, runCfg, "status")
			if err != nil {
				result.Reasons = []string{fmt.Sprintf("migration %s post-up status failed: %s", migration.Name, redactMigrationDSN(err.Error(), migration.DSN))}
				return finishMigrationRepairDirty(result, *format)
			}
			afterStatus, err := parseMigrationStatus(after.Stdout)
			if err != nil {
				result.Reasons = []string{fmt.Sprintf("migration %s post-up status failed: %s", migration.Name, err)}
				return finishMigrationRepairDirty(result, *format)
			}
			result.Migration = migrationValidationRecord{Name: migration.Name, Driver: migration.Driver, Current: afterStatus.Current, Dirty: afterStatus.Dirty, Pending: afterStatus.Pending}
			if afterStatus.Dirty {
				result.Reasons = []string{fmt.Sprintf("migration %s is dirty after up", migration.Name)}
				return finishMigrationRepairDirty(result, *format)
			}
			result.Decision = "pass"
			return finishMigrationRepairDirty(result, *format)
		}
		result.Reasons = []string{fmt.Sprintf("migration %s is not dirty", migration.Name)}
		return finishMigrationRepairDirty(result, *format)
	}
	if beforeStatus.Current != *expectedDirtyVersion {
		result.Reasons = []string{fmt.Sprintf("migration %s dirty version %s does not match expected %s", migration.Name, migrationCurrentOrUnknown(beforeStatus.Current), *expectedDirtyVersion)}
		return finishMigrationRepairDirty(result, *format)
	}

	repairArgs := []string{"repair-dirty", "--expected-dirty-version", *expectedDirtyVersion, "--force-version", *forceVersion, "--confirm-force", "FORCE_MIGRATION_METADATA"}
	if *thenUp {
		repairArgs = append(repairArgs, "--then-up")
	}
	if *upIfClean {
		repairArgs = append(repairArgs, "--up-if-clean")
	}
	if _, err := runner.runArgs(ctx, runCfg, repairArgs); err != nil {
		result.Reasons = []string{fmt.Sprintf("migration %s repair failed: %s", migration.Name, redactMigrationDSN(err.Error(), migration.DSN))}
		return finishMigrationRepairDirty(result, *format)
	}
	after, err := runner.run(ctx, runCfg, "status")
	if err != nil {
		result.Reasons = []string{fmt.Sprintf("migration %s post-repair status failed: %s", migration.Name, redactMigrationDSN(err.Error(), migration.DSN))}
		return finishMigrationRepairDirty(result, *format)
	}
	afterStatus, err := parseMigrationStatus(after.Stdout)
	if err != nil {
		result.Reasons = []string{fmt.Sprintf("migration %s post-repair status failed: %s", migration.Name, err)}
		return finishMigrationRepairDirty(result, *format)
	}
	result.Decision = "pass"
	result.Migration = migrationValidationRecord{
		Name:    migration.Name,
		Driver:  migration.Driver,
		Current: afterStatus.Current,
		Dirty:   afterStatus.Dirty,
		Pending: afterStatus.Pending,
	}
	if afterStatus.Dirty {
		result.Decision = "fail"
		result.Reasons = []string{fmt.Sprintf("migration %s remains dirty at version %s", migration.Name, migrationCurrentOrUnknown(afterStatus.Current))}
	}
	return finishMigrationRepairDirty(result, *format)
}

func isProtectedMigrationEnvironment(envName string) bool {
	envName = strings.ToLower(strings.TrimSpace(envName))
	return envName == "prod" || envName == "production"
}

func buildMigrationRepairApprovalCommand(configFile, envName, pluginDir, expectedDirtyVersion, forceVersion string, thenUp, upIfClean bool) string {
	parts := []string{
		"wfctl", "migrations", "repair-dirty",
		"--config", configFile,
		"--env", envName,
		"--plugin-dir", pluginDir,
		"--expected-dirty-version", expectedDirtyVersion,
		"--force-version", forceVersion,
		"--confirm-force", "FORCE_MIGRATION_METADATA",
		"--approved-token-env", "WFCTL_MIGRATION_REPAIR_APPROVED_TOKEN",
	}
	if thenUp {
		parts = append(parts, "--then-up")
	}
	if upIfClean {
		parts = append(parts, "--up-if-clean")
	}
	quoted := make([]string, 0, len(parts))
	for _, part := range parts {
		quoted = append(quoted, shellQuote(part))
	}
	return strings.Join(quoted, " ")
}

func finishMigrationRepairDirty(result migrationRepairDirtyResult, format string) error {
	switch format {
	case "json":
		data, err := json.Marshal(result)
		if err != nil {
			return err
		}
		fmt.Println(string(data))
	case "text", "":
		fmt.Printf("migrations repair-dirty: %s\n", result.Decision)
		for _, reason := range result.Reasons {
			fmt.Printf("  - %s\n", reason)
		}
		if result.Decision == "pass" {
			fmt.Printf("  %s current=%s dirty=%v\n", result.Migration.Name, result.Migration.Current, result.Migration.Dirty)
		}
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
	if result.Decision == "fail" {
		return errors.New(strings.Join(result.Reasons, "; "))
	}
	return nil
}

func runMigrationsValidate(args []string) error {
	fs := flag.NewFlagSet("migrations validate", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	fs.StringVar(configFile, "c", "app.yaml", "Config file (short for --config)")
	envName := fs.String("env", "", "Environment name")
	pluginDir := fs.String("plugin-dir", defaultMigrationPluginDir(), "Plugin directory")
	format := fs.String("format", "text", "Output format: text or json")
	resultFile := fs.String("result-file", "", "Write validation result JSON to this path")
	commit := fs.String("commit", "", "Commit SHA associated with this validation")
	candidateRef := fs.String("candidate-ref", "HEAD", "Candidate git ref to validate")
	forceBaselineCandidate := fs.Bool("force-baseline-candidate", false, "Run baseline/candidate replay even when no migration source changed")
	debugKeepTemp := fs.Bool("debug-keep-temp", false, "Keep temporary migration source materializations")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := loadMigrationWorkflowConfig(*configFile)
	if err != nil {
		return failMigrationValidationEarly(*resultFile, *format, *commit, fmt.Errorf("load config: %w", err))
	}
	migrations, err := resolveMigrationConfigs(cfg, *envName)
	if err != nil {
		return failMigrationValidationEarly(*resultFile, *format, *commit, err)
	}

	ctx := context.Background()
	runner := newMigrationPluginRunner()
	gitOps := migrationGitOps.withDefaults()
	if *commit == "" && hasBaselineCandidateValidation(migrations) {
		resolvedCommit, err := gitOps.CurrentCommit(ctx)
		if err != nil {
			return failMigrationValidationEarly(*resultFile, *format, *commit, fmt.Errorf("resolve current commit: %w", err))
		}
		*commit = resolvedCommit
	}
	result := migrationValidationResult{
		Decision:   "pass",
		Commit:     *commit,
		Migrations: make([]migrationValidationRecord, 0, len(migrations)),
	}
	for _, migration := range migrations {
		record := migrationValidationRecord{Name: migration.Name}
		baselineRef := ""
		runBaselineCandidate := false
		if migration.Validation.BaselineCandidate {
			var err error
			baselineRef, runBaselineCandidate, err = shouldRunBaselineCandidateValidation(ctx, gitOps, migration, *candidateRef, *forceBaselineCandidate)
			if err != nil {
				return failMigrationValidation(result, record, *resultFile, *format, migration, "baseline_candidate", err)
			}
		}
		runCfg := migrationPluginRunConfig{
			Plugin:    migration.Plugin,
			PluginDir: *pluginDir,
			Driver:    migration.Driver,
			SourceDir: migration.SourceDir,
			DSN:       migration.DSN,
		}
		if migration.Validation.Lint {
			lintCfg := runCfg
			lintCfg.DSN = ""
			if _, err := runner.runLint(ctx, lintCfg); err != nil {
				return failMigrationValidation(result, record, *resultFile, *format, migration, "lint", err)
			}
			record.Lint = "pass"
		}
		if runBaselineCandidate {
			baselineResult, err := runBaselineCandidateValidation(ctx, runner, gitOps, runCfg, migration, baselineRef, *candidateRef, *debugKeepTemp)
			if err != nil {
				return failMigrationValidation(result, record, *resultFile, *format, migration, "baseline_candidate", err)
			}
			record.BaselineCandidate = "pass"
			record.Dirty = baselineResult.Dirty
			record.Pending = baselineResult.Pending
		} else if migration.Validation.BaselineCandidate {
			record.BaselineCandidate = "skip"
		}
		if migration.Validation.FreshCycle {
			if err := runFreshCycleValidation(ctx, runner, runCfg, migration); err != nil {
				return failMigrationValidation(result, record, *resultFile, *format, migration, "fresh_cycle", err)
			}
			record.FreshCycle = "pass"
		}
		result.Migrations = append(result.Migrations, record)
	}

	if *resultFile != "" {
		if err := writeMigrationValidationResult(*resultFile, result); err != nil {
			return err
		}
	}

	switch *format {
	case "json":
		data, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("encode validation result: %w", err)
		}
		fmt.Println(string(data))
	case "text", "":
		fmt.Printf("migrations validation: %s\n", result.Decision)
	default:
		return fmt.Errorf("unsupported format %q", *format)
	}
	return nil
}

func failMigrationValidation(result migrationValidationResult, record migrationValidationRecord, resultFile string, format string, migration resolvedMigrationConfig, check string, err error) error {
	result.Decision = "fail"
	record.Error = redactMigrationDSN(err.Error(), migration.DSN)
	switch check {
	case "lint":
		record.Lint = "fail"
	case "baseline_candidate":
		record.BaselineCandidate = "fail"
	case "fresh_cycle":
		record.FreshCycle = "fail"
	}
	result.Migrations = append(result.Migrations, record)
	if resultFile != "" {
		if writeErr := writeMigrationValidationResult(resultFile, result); writeErr != nil {
			return writeErr
		}
	}
	if format == "json" {
		data, encodeErr := json.Marshal(result)
		if encodeErr != nil {
			return encodeErr
		}
		fmt.Println(string(data))
	}
	return errors.New(record.Error)
}

func failMigrationValidationEarly(resultFile string, format string, commit string, err error) error {
	result := migrationValidationResult{
		Decision:   "fail",
		Commit:     commit,
		Migrations: []migrationValidationRecord{},
		Error:      err.Error(),
	}
	if resultFile != "" {
		if writeErr := writeMigrationValidationResult(resultFile, result); writeErr != nil {
			return writeErr
		}
	}
	if format == "json" {
		data, encodeErr := json.Marshal(result)
		if encodeErr != nil {
			return encodeErr
		}
		fmt.Println(string(data))
	}
	return err
}

func loadMigrationWorkflowConfig(configFile string) (*config.WorkflowConfig, error) {
	cfg, err := config.LoadFromFile(configFile)
	if err != nil {
		return nil, err
	}
	if cfg != nil && cfg.CI != nil {
		if err := cfg.CI.Validate(); err != nil {
			return nil, fmt.Errorf("validate ci config: %w", err)
		}
	}
	return cfg, nil
}

func collectMigrationStatusForConfigs(ctx context.Context, migrations []resolvedMigrationConfig, pluginDir string) (migrationStatusResult, error) {
	result := migrationStatusResult{
		Decision:              "pass",
		Destructive:           false,
		HumanApprovalRequired: false,
		Migrations:            make([]migrationValidationRecord, 0, len(migrations)),
	}
	runner := newMigrationPluginRunner()
	var errs []error
	for _, migration := range migrations {
		runCfg := migrationPluginRunConfig{
			Plugin:    migration.Plugin,
			PluginDir: pluginDir,
			Driver:    migration.Driver,
			SourceDir: migration.SourceDir,
			DSN:       migration.DSN,
		}
		record := migrationValidationRecord{Name: migration.Name, Driver: migration.Driver}
		statusOutput, err := runner.run(ctx, runCfg, "status")
		if err != nil {
			reason := fmt.Sprintf("migration %s status failed: %s", migration.Name, redactMigrationDSN(err.Error(), migration.DSN))
			result.Reasons = append(result.Reasons, reason)
			record.Error = reason
			result.Migrations = append(result.Migrations, record)
			errs = append(errs, errors.New(reason))
			continue
		}
		status, err := parseMigrationStatus(statusOutput.Stdout)
		if err != nil {
			reason := fmt.Sprintf("migration %s status failed: %s", migration.Name, redactMigrationDSN(err.Error(), migration.DSN))
			result.Reasons = append(result.Reasons, reason)
			record.Error = reason
			result.Migrations = append(result.Migrations, record)
			errs = append(errs, errors.New(reason))
			continue
		}
		record.Current = status.Current
		record.Dirty = status.Dirty
		record.Pending = status.Pending
		result.Migrations = append(result.Migrations, record)
	}
	if len(result.Reasons) > 0 {
		result.Decision = "fail"
	}
	return result, errors.Join(errs...)
}

func applyMigrationsForConfigs(ctx context.Context, migrations []resolvedMigrationConfig, pluginDir string) (migrationStatusResult, error) {
	result := migrationStatusResult{
		Decision:              "pass",
		Destructive:           false,
		HumanApprovalRequired: false,
		Migrations:            make([]migrationValidationRecord, 0, len(migrations)),
	}
	runner := newMigrationPluginRunner()
	var errs []error
	for _, migration := range migrations {
		runCfg := migrationPluginRunConfig{
			Plugin:    migration.Plugin,
			PluginDir: pluginDir,
			Driver:    migration.Driver,
			SourceDir: migration.SourceDir,
			DSN:       migration.DSN,
		}
		record := migrationValidationRecord{Name: migration.Name, Driver: migration.Driver}
		if _, err := runner.run(ctx, runCfg, "up"); err != nil {
			reason := fmt.Sprintf("migration %s up failed: %s", migration.Name, redactMigrationDSN(err.Error(), migration.DSN))
			result.Reasons = append(result.Reasons, reason)
			record.Error = reason
			result.Migrations = append(result.Migrations, record)
			errs = append(errs, errors.New(reason))
			continue
		}
		statusOutput, err := runner.run(ctx, runCfg, "status")
		if err != nil {
			reason := fmt.Sprintf("migration %s post-up status failed: %s", migration.Name, redactMigrationDSN(err.Error(), migration.DSN))
			result.Reasons = append(result.Reasons, reason)
			record.Error = reason
			result.Migrations = append(result.Migrations, record)
			errs = append(errs, errors.New(reason))
			continue
		}
		status, err := parseMigrationStatus(statusOutput.Stdout)
		if err != nil {
			reason := fmt.Sprintf("migration %s post-up status failed: %s", migration.Name, redactMigrationDSN(err.Error(), migration.DSN))
			result.Reasons = append(result.Reasons, reason)
			record.Error = reason
			result.Migrations = append(result.Migrations, record)
			errs = append(errs, errors.New(reason))
			continue
		}
		record.Current = status.Current
		record.Dirty = status.Dirty
		record.Pending = status.Pending
		for _, reason := range migrationStatusCleanReasons([]migrationValidationRecord{record}) {
			reason = strings.Replace(reason, " is dirty ", " is dirty after up ", 1)
			result.Reasons = append(result.Reasons, reason)
			errs = append(errs, errors.New(reason))
		}
		result.Migrations = append(result.Migrations, record)
	}
	if len(result.Reasons) > 0 {
		result.Decision = "fail"
	}
	return result, errors.Join(errs...)
}

func migrationCurrentOrUnknown(current string) string {
	current = strings.TrimSpace(current)
	if current == "" {
		return "unknown"
	}
	return current
}

func migrationStatusCleanReasons(migrations []migrationValidationRecord) []string {
	var reasons []string
	for i := range migrations {
		migration := &migrations[i]
		if migration.Dirty {
			reasons = append(reasons, fmt.Sprintf("migration %s is dirty at version %s", migration.Name, migrationCurrentOrUnknown(migration.Current)))
		}
		if len(migration.Pending) > 0 {
			reasons = append(reasons, fmt.Sprintf("migration %s has pending migrations: %s", migration.Name, strings.Join(migration.Pending, ", ")))
		}
	}
	return reasons
}

func migrationStatusDirtyReasons(migrations []migrationValidationRecord, configs []resolvedMigrationConfig) []string {
	var reasons []string
	forbidDirtyByName := make(map[string]bool, len(configs))
	for i := range configs {
		forbidDirtyByName[configs[i].Name] = configs[i].Validation.ForbidDirty
	}
	for i := range migrations {
		migration := &migrations[i]
		if migration.Dirty && forbidDirtyByName[migration.Name] {
			reasons = append(reasons, fmt.Sprintf("migration %s is dirty at version %s", migration.Name, migrationCurrentOrUnknown(migration.Current)))
		}
	}
	return reasons
}

func defaultMigrationPluginDir() string {
	if pluginDir := strings.TrimSpace(os.Getenv("WFCTL_PLUGIN_DIR")); pluginDir != "" {
		return pluginDir
	}
	return defaultDataDir
}

func checkMigrationValidationResult(path, commit string, requireSameSHA bool, expectedMigrations []resolvedMigrationConfig) []string {
	var reasons []string
	if strings.TrimSpace(path) == "" {
		return []string{"validation result is required"}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{fmt.Sprintf("read validation result: %v", err)}
	}
	var result migrationValidationResult
	if err := json.Unmarshal(data, &result); err != nil {
		return []string{fmt.Sprintf("decode validation result: %v", err)}
	}
	if result.Decision != "pass" {
		reasons = append(reasons, fmt.Sprintf("validation result decision is %q", result.Decision))
	}
	if requireSameSHA && strings.TrimSpace(commit) == "" {
		reasons = append(reasons, "commit is required when --require-same-sha is set")
	}
	if strings.TrimSpace(commit) != "" && result.Commit != commit {
		reasons = append(reasons, fmt.Sprintf("validation result commit %s does not match %s", result.Commit, commit))
	}
	records := make(map[string]migrationValidationRecord, len(result.Migrations))
	for i := range result.Migrations {
		migration := &result.Migrations[i]
		records[migration.Name] = *migration
	}
	for _, expected := range expectedMigrations {
		record, ok := records[expected.Name]
		if !ok {
			reasons = append(reasons, fmt.Sprintf("validation result missing migration %s", expected.Name))
			continue
		}
		if record.Error != "" {
			reasons = append(reasons, fmt.Sprintf("validation result migration %s has error", expected.Name))
		}
		if record.Lint == "fail" || record.FreshCycle == "fail" || record.BaselineCandidate == "fail" {
			reasons = append(reasons, fmt.Sprintf("validation result migration %s has failed checks", expected.Name))
		}
		if record.Lint == "" && record.FreshCycle == "" && record.BaselineCandidate == "" {
			reasons = append(reasons, fmt.Sprintf("validation result migration %s has no passing checks", expected.Name))
		}
		if expected.Validation.Lint && record.Lint != "pass" {
			reasons = append(reasons, fmt.Sprintf("validation result migration %s missing required lint check", expected.Name))
		}
		if expected.Validation.FreshCycle && record.FreshCycle != "pass" {
			reasons = append(reasons, fmt.Sprintf("validation result migration %s missing required fresh_cycle check", expected.Name))
		}
		if expected.Validation.BaselineCandidate && record.BaselineCandidate != "pass" && record.BaselineCandidate != "skip" {
			reasons = append(reasons, fmt.Sprintf("validation result migration %s missing required baseline_candidate check", expected.Name))
		}
	}
	return reasons
}

func writeMigrationStatusOutput(result migrationStatusResult, format, label string) error {
	switch format {
	case "json":
		data, err := json.Marshal(result)
		if err != nil {
			return fmt.Errorf("encode migrations status: %w", err)
		}
		fmt.Println(string(data))
	case "text", "":
		fmt.Printf("%s: %s\n", label, result.Decision)
		for _, reason := range result.Reasons {
			fmt.Printf("  - %s\n", reason)
		}
	default:
		return fmt.Errorf("unsupported format %q", format)
	}
	return nil
}

func writeMigrationValidationResult(path string, result migrationValidationResult) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("encode validation result: %w", err)
	}
	if dir := filepath.Dir(path); dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return fmt.Errorf("create validation result directory: %w", err)
		}
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o600); err != nil {
		return fmt.Errorf("write validation result: %w", err)
	}
	return nil
}
