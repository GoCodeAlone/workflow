package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/GoCodeAlone/workflow/config"
)

var newMigrationPluginRunner = func() migrationPluginRunner {
	return migrationPluginRunner{}
}

type migrationValidationResult struct {
	Decision   string                      `json:"decision"`
	Commit     string                      `json:"commit,omitempty"`
	Migrations []migrationValidationRecord `json:"migrations"`
}

type migrationValidationRecord struct {
	Name       string `json:"name"`
	Lint       string `json:"lint,omitempty"`
	FreshCycle string `json:"fresh_cycle,omitempty"`
	Dirty      bool   `json:"dirty"`
}

func runMigrations(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: wfctl migrations <validate|status|ci-check|repair-dirty>")
	}
	switch args[0] {
	case "validate":
		return runMigrationsValidate(args[1:])
	default:
		return fmt.Errorf("unknown migrations subcommand %q", args[0])
	}
}

func runMigrationsValidate(args []string) error {
	fs := flag.NewFlagSet("migrations validate", flag.ContinueOnError)
	configFile := fs.String("config", "app.yaml", "Workflow config file")
	fs.StringVar(configFile, "c", "app.yaml", "Config file (short for --config)")
	envName := fs.String("env", "", "Environment name")
	pluginDir := fs.String("plugin-dir", defaultDataDir, "Plugin directory")
	format := fs.String("format", "text", "Output format: text or json")
	resultFile := fs.String("result-file", "", "Write validation result JSON to this path")
	commit := fs.String("commit", "", "Commit SHA associated with this validation")
	candidateRef := fs.String("candidate-ref", "HEAD", "Candidate git ref to validate")
	forceBaselineCandidate := fs.Bool("force-baseline-candidate", false, "Run baseline/candidate replay even when no migration source changed")
	debugKeepTemp := fs.Bool("debug-keep-temp", false, "Keep temporary migration source materializations")
	if err := fs.Parse(args); err != nil {
		return err
	}

	cfg, err := config.LoadFromFile(*configFile)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	migrations, err := resolveMigrationConfigs(cfg, *envName)
	if err != nil {
		return err
	}

	ctx := context.Background()
	runner := newMigrationPluginRunner()
	gitOps := migrationGitOps.withDefaults()
	if *commit == "" && hasBaselineCandidateValidation(migrations) {
		resolvedCommit, err := gitOps.CurrentCommit(ctx)
		if err != nil {
			return fmt.Errorf("resolve current commit: %w", err)
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
				return err
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
			if _, err := runner.run(ctx, runCfg, "lint"); err != nil {
				return err
			}
			record.Lint = "pass"
		}
		if runBaselineCandidate {
			if err := runBaselineCandidateValidation(ctx, runner, gitOps, runCfg, migration, baselineRef, *candidateRef, *debugKeepTemp); err != nil {
				return err
			}
		}
		if migration.Validation.FreshCycle {
			if _, err := runner.run(ctx, runCfg, "test"); err != nil {
				return err
			}
			record.FreshCycle = "pass"
		}
		result.Migrations = append(result.Migrations, record)
	}

	if *resultFile != "" {
		data, err := json.MarshalIndent(result, "", "  ")
		if err != nil {
			return fmt.Errorf("encode validation result: %w", err)
		}
		if err := os.WriteFile(*resultFile, append(data, '\n'), 0o644); err != nil {
			return fmt.Errorf("write validation result: %w", err)
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
