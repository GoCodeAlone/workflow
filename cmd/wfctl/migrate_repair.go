package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/GoCodeAlone/workflow/config"
	"github.com/GoCodeAlone/workflow/interfaces"
)

const migrationRepairOperation = "migration_repair_dirty"

type stringListFlag []string

func (s *stringListFlag) String() string {
	if s == nil {
		return ""
	}
	return strings.Join(*s, ",")
}

func (s *stringListFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func runMigrateRepairDirty(args []string) error {
	return runMigrateRepairDirtyWithOutput(args, os.Stdout)
}

func runMigrateRepairDirtyWithOutput(args []string, out io.Writer) error {
	fs := flag.NewFlagSet("migrate repair-dirty", flag.ContinueOnError)
	fs.SetOutput(out)
	var cfgPath string
	var envName string
	var databaseName string
	var appName string
	var jobImage string
	var sourceDir string
	var expectedDirtyVersion string
	var forceVersion string
	var thenUp bool
	var upIfClean bool
	var confirmForce string
	var approveDestructive bool
	var approvalArtifact string
	var timeout time.Duration
	var jobEnv stringListFlag
	var jobEnvFromEnv stringListFlag
	fs.StringVar(&cfgPath, "config", "infra.yaml", "Infrastructure config file")
	fs.StringVar(&envName, "env", "", "Environment name")
	fs.StringVar(&databaseName, "database", "", "Database module name")
	fs.StringVar(&appName, "app", "", "App/container service module name")
	fs.StringVar(&jobImage, "job-image", "", "Migration job image")
	fs.StringVar(&sourceDir, "source-dir", "/migrations", "Migration source directory in the job image")
	fs.StringVar(&expectedDirtyVersion, "expected-dirty-version", "", "Dirty migration version expected before repair")
	fs.StringVar(&forceVersion, "force-version", "", "Version to force migration metadata to before running pending migrations")
	fs.BoolVar(&thenUp, "then-up", false, "Run pending migrations after metadata repair")
	fs.BoolVar(&upIfClean, "up-if-clean", false, "Run pending migrations if database is already clean")
	fs.StringVar(&confirmForce, "confirm-force", "", "Required confirmation string: FORCE_MIGRATION_METADATA")
	fs.BoolVar(&approveDestructive, "approve-destructive", false, "Approve the destructive metadata repair operation")
	fs.StringVar(&approvalArtifact, "approval-artifact", "", "Path to write destructive approval artifact")
	fs.DurationVar(&timeout, "timeout", 10*time.Minute, "Provider job timeout")
	fs.Var(&jobEnv, "job-env", "Environment variable for provider job as KEY=VALUE; repeatable")
	fs.Var(&jobEnvFromEnv, "job-env-from-env", "Read environment variable from current process into provider job; repeatable")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), `Usage: wfctl migrate repair-dirty [options]

Run a guarded dirty migration metadata repair inside a provider-managed runtime.

Required guard flags for non-dev environments:
  --expected-dirty-version <version>
  --force-version <version>
  --confirm-force FORCE_MIGRATION_METADATA
  --approve-destructive

Options:
`)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if err == flag.ErrHelp {
			return nil
		}
		return err
	}

	jobEnvMap, err := collectMigrationRepairEnv(jobEnv, jobEnvFromEnv)
	if err != nil {
		return err
	}

	appSpec, err := findMigrateRepairCanonicalSpecByName(cfgPath, envName, appName)
	if err != nil {
		return err
	}
	dbSpec, err := findMigrateRepairCanonicalSpecByName(cfgPath, envName, databaseName)
	if err != nil {
		return err
	}
	appProviderRef := resourceSpecProviderRef(appSpec)
	dbProviderRef := resourceSpecProviderRef(dbSpec)
	if appProviderRef == "" || dbProviderRef == "" {
		return fmt.Errorf("app %q and database %q must both resolve a provider module", appSpec.Name, dbSpec.Name)
	}
	if appProviderRef != dbProviderRef {
		return fmt.Errorf("app %q and database %q must reference the same provider module, got %q and %q", appSpec.Name, dbSpec.Name, appProviderRef, dbProviderRef)
	}
	appProviderType, appProviderCfg, err := resolveProviderForSpec(cfgPath, envName, appSpec)
	if err != nil {
		return err
	}

	req := interfaces.MigrationRepairRequest{
		AppResourceName:      appSpec.Name,
		DatabaseResourceName: dbSpec.Name,
		JobImage:             jobImage,
		SourceDir:            sourceDir,
		ExpectedDirtyVersion: expectedDirtyVersion,
		ForceVersion:         forceVersion,
		ThenUp:               thenUp,
		UpIfClean:            upIfClean,
		ConfirmForce:         confirmForce,
		Env:                  jobEnvMap,
		TimeoutSeconds:       int(timeout.Seconds()),
	}
	if err := req.Validate(); err != nil {
		return err
	}

	decision := destructiveDecision{
		Operation:            migrationRepairOperation,
		Env:                  envName,
		App:                  appName,
		Database:             databaseName,
		ExpectedDirtyVersion: expectedDirtyVersion,
		ForceVersion:         forceVersion,
	}
	if result, err := requireDestructiveApproval(decision, approveDestructive, approvalArtifact); err != nil {
		printMigrationRepairResult(out, result, nil)
		_ = writeMigrationRepairSummary(result, envName, appName, nil)
		return err
	}

	provider, closer, err := resolveIaCProvider(context.Background(), appProviderType, appProviderCfg)
	if err != nil {
		return fmt.Errorf("load provider %q: %w", appProviderType, err)
	}
	if closer != nil {
		defer closer.Close() //nolint:errcheck // best-effort plugin shutdown
	}
	repairer, ok := provider.(interfaces.ProviderMigrationRepairer)
	if !ok {
		result := &interfaces.MigrationRepairResult{Status: interfaces.MigrationRepairStatusUnsupported}
		printMigrationRepairResult(out, result, nil)
		_ = writeMigrationRepairSummary(result, envName, appName, nil)
		return fmt.Errorf("provider %q does not support migration repair", appProviderType)
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	result, err := repairer.RepairDirtyMigration(ctx, req)
	if result == nil {
		if err != nil {
			result = &interfaces.MigrationRepairResult{Status: interfaces.MigrationRepairStatusFailed}
		} else {
			result = &interfaces.MigrationRepairResult{Status: interfaces.MigrationRepairStatusSucceeded}
		}
	}
	if statusErr := validateMigrationRepairResultStatus(result.Status); statusErr != nil {
		return redactMigrationRepairError(statusErr, jobEnvMap)
	}
	printMigrationRepairResult(out, result, jobEnvMap)
	if summaryErr := writeMigrationRepairSummary(result, envName, appName, jobEnvMap); summaryErr != nil && err == nil {
		err = summaryErr
	}
	if err != nil {
		return redactMigrationRepairError(err, jobEnvMap)
	}
	if result.Status != interfaces.MigrationRepairStatusSucceeded {
		return fmt.Errorf("migration repair finished with status %s", result.Status)
	}
	return nil
}

func findMigrateRepairCanonicalSpecByName(cfgFile, envName, name string) (interfaces.ResourceSpec, error) {
	if strings.TrimSpace(name) == "" {
		return interfaces.ResourceSpec{}, fmt.Errorf("infra resource name is required")
	}
	specs, err := parseInfraResourceSpecsForEnv(cfgFile, envName)
	if err != nil {
		return interfaces.ResourceSpec{}, err
	}
	if envName != "" {
		if resolvedNames, err := findMigrateRepairResolvedNames(cfgFile, envName, name); err != nil {
			return interfaces.ResourceSpec{}, err
		} else if len(resolvedNames) > 0 {
			for _, spec := range specs {
				if resolvedNames[spec.Name] || resolvedNames[resourceSpecNameFromConfig(spec.Config)] {
					return spec, nil
				}
			}
		}
	}
	for _, spec := range specs {
		if spec.Name == name || resourceSpecNameFromConfig(spec.Config) == name {
			return spec, nil
		}
	}
	if envName != "" {
		return interfaces.ResourceSpec{}, fmt.Errorf("infra resource %q not found in %s for env %q", name, cfgFile, envName)
	}
	return interfaces.ResourceSpec{}, fmt.Errorf("infra resource %q not found in %s", name, cfgFile)
}

func findMigrateRepairResolvedNames(cfgFile, envName, name string) (map[string]bool, error) {
	cfg, err := config.LoadFromFile(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load %s: %w", cfgFile, err)
	}
	for i := range cfg.Modules {
		m := &cfg.Modules[i]
		if !isInfraType(m.Type) || m.Name != name {
			continue
		}
		resolved, ok := m.ResolveForEnv(envName)
		if !ok {
			return map[string]bool{}, nil
		}
		names := map[string]bool{
			resolved.Name: true,
		}
		if resolved.Config != nil {
			if configName, _ := resolved.Config["name"].(string); strings.TrimSpace(configName) != "" {
				names[strings.TrimSpace(configName)] = true
			}
		}
		return names, nil
	}
	return map[string]bool{}, nil
}

func resourceSpecNameFromConfig(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	name, _ := cfg["name"].(string)
	return strings.TrimSpace(name)
}

func collectMigrationRepairEnv(jobEnv, jobEnvFromEnv []string) (map[string]string, error) {
	out := map[string]string{}
	for _, entry := range jobEnv {
		key, value, ok := strings.Cut(entry, "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			return nil, fmt.Errorf("--job-env must be KEY=VALUE with a non-empty key")
		}
		out[key] = value
	}
	for _, key := range jobEnvFromEnv {
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("--job-env-from-env requires a variable name")
		}
		value, ok := os.LookupEnv(key)
		if !ok {
			return nil, fmt.Errorf("--job-env-from-env %s is not set", key)
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func printMigrationRepairResult(out io.Writer, result *interfaces.MigrationRepairResult, secrets map[string]string) {
	if result == nil {
		return
	}
	providerJobID := redactMigrationRepairSecrets(result.ProviderJobID, secrets)
	status := redactMigrationRepairSecrets(result.Status, secrets)
	if result.ProviderJobID != "" {
		fmt.Fprintf(out, "provider job %s: %s\n", providerJobID, status)
	} else if result.Status != "" {
		fmt.Fprintf(out, "migration repair: %s\n", status)
	}
	if strings.TrimSpace(result.Logs) != "" {
		fmt.Fprintln(out, redactMigrationRepairSecrets(result.Logs, secrets))
	}
}

func writeMigrationRepairSummary(result *interfaces.MigrationRepairResult, envName, appName string, secrets map[string]string) error {
	if result == nil || os.Getenv("GITHUB_STEP_SUMMARY") == "" {
		return nil
	}
	outcome := migrationRepairSummaryOutcome(result.Status)
	diagnostics := redactMigrationRepairDiagnostics(result.Diagnostics, secrets)
	if result.Logs != "" {
		diagnostics = append(diagnostics, interfaces.Diagnostic{
			ID:     redactMigrationRepairSecrets(result.ProviderJobID, secrets),
			Phase:  redactMigrationRepairSecrets(result.Status, secrets),
			Cause:  "migration repair logs",
			Detail: redactMigrationRepairSecrets(result.Logs, secrets),
		})
	}
	if len(diagnostics) == 0 && result.ProviderJobID != "" {
		diagnostics = append(diagnostics, interfaces.Diagnostic{
			ID:    redactMigrationRepairSecrets(result.ProviderJobID, secrets),
			Phase: redactMigrationRepairSecrets(result.Status, secrets),
			Cause: "provider job",
		})
	}
	return WriteStepSummary(detectCIProvider(), SummaryInput{
		Operation:   migrationRepairOperation,
		Env:         envName,
		Resource:    appName,
		Outcome:     outcome,
		Diagnostics: diagnostics,
	})
}

func migrationRepairSummaryOutcome(status string) string {
	if status == interfaces.MigrationRepairStatusSucceeded {
		return "SUCCESS"
	}
	return "FAILED"
}

func redactMigrationRepairDiagnostics(diagnostics []interfaces.Diagnostic, secrets map[string]string) []interfaces.Diagnostic {
	if len(diagnostics) == 0 {
		return nil
	}
	redacted := make([]interfaces.Diagnostic, 0, len(diagnostics))
	for _, diagnostic := range diagnostics {
		diagnostic.ID = redactMigrationRepairSecrets(diagnostic.ID, secrets)
		diagnostic.Phase = redactMigrationRepairSecrets(diagnostic.Phase, secrets)
		diagnostic.Cause = redactMigrationRepairSecrets(diagnostic.Cause, secrets)
		diagnostic.Detail = redactMigrationRepairSecrets(diagnostic.Detail, secrets)
		redacted = append(redacted, diagnostic)
	}
	return redacted
}

func redactMigrationRepairSecrets(value string, secrets map[string]string) string {
	for _, secret := range secrets {
		if secret != "" {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	return value
}

func redactMigrationRepairError(err error, secrets map[string]string) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", redactMigrationRepairSecrets(err.Error(), secrets))
}

func validateMigrationRepairResultStatus(status string) error {
	switch status {
	case "":
		return fmt.Errorf("empty migration repair status from provider")
	case interfaces.MigrationRepairStatusSucceeded,
		interfaces.MigrationRepairStatusFailed,
		interfaces.MigrationRepairStatusApprovalRequired,
		interfaces.MigrationRepairStatusUnsupported:
		return nil
	default:
		return fmt.Errorf("unknown migration repair status %q from provider", status)
	}
}
