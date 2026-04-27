package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/GoCodeAlone/workflow/config"
)

const (
	defaultMigrationPlugin = "workflow-plugin-migrations"
	defaultMigrationDriver = "golang-migrate"
)

type resolvedMigrationConfig struct {
	Name         string
	Plugin       string
	Driver       string
	SourceDir    string
	DSN          string
	BaselineRef  string
	BaselineMode string
	Validation   config.CIMigrationValidationConfig
}

func resolveMigrationConfigs(cfg *config.WorkflowConfig, envName string) ([]resolvedMigrationConfig, error) {
	if cfg == nil || cfg.CI == nil || len(cfg.CI.Migrations) == 0 {
		return nil, nil
	}

	resolved := make([]resolvedMigrationConfig, 0, len(cfg.CI.Migrations))
	var errs []error
	for i := range cfg.CI.Migrations {
		migration := &cfg.CI.Migrations[i]
		envMigration, ok, envErr := migrationForEnv(*migration, envName)
		if envErr != nil {
			errs = append(errs, fmt.Errorf("ci.migrations[%d]: %w", i, envErr))
			continue
		}
		if !ok {
			continue
		}
		migration = &envMigration
		itemName := strings.TrimSpace(migration.Name)
		label := itemName
		if label == "" {
			label = fmt.Sprintf("%d", i)
			errs = append(errs, fmt.Errorf("ci.migrations[%d]: name is required", i))
		}
		if strings.TrimSpace(migration.SourceDir) == "" {
			errs = append(errs, fmt.Errorf("ci.migrations[%s]: source_dir is required", label))
		}

		dsn, err := resolveMigrationDSN(*migration)
		if err != nil {
			errs = append(errs, fmt.Errorf("ci.migrations[%s]: %w", label, err))
		}

		plugin := strings.TrimSpace(migration.Plugin)
		if plugin == "" {
			plugin = defaultMigrationPlugin
		}
		if err := validateMigrationPluginName(plugin); err != nil {
			errs = append(errs, fmt.Errorf("ci.migrations[%s]: %w", label, err))
		}
		driver := strings.TrimSpace(migration.Driver)
		if driver == "" {
			driver = defaultMigrationDriver
		}

		resolved = append(resolved, resolvedMigrationConfig{
			Name:         itemName,
			Plugin:       plugin,
			Driver:       driver,
			SourceDir:    strings.TrimSpace(migration.SourceDir),
			DSN:          dsn,
			BaselineRef:  strings.TrimSpace(migration.Baseline.Ref),
			BaselineMode: strings.TrimSpace(migration.Baseline.Mode),
			Validation:   migration.Validation,
		})
	}

	return resolved, errors.Join(errs...)
}

func migrationForEnv(migration config.CIMigrationConfig, envName string) (config.CIMigrationConfig, bool, error) {
	if envName == "" || len(migration.Environments) == 0 {
		return migration, true, nil
	}
	override, listed := migration.Environments[envName]
	if !listed {
		return migration, true, nil
	}
	if override == nil {
		return config.CIMigrationConfig{}, false, nil
	}
	if strings.TrimSpace(override.Plugin) != "" {
		migration.Plugin = override.Plugin
	}
	if strings.TrimSpace(override.Driver) != "" {
		migration.Driver = override.Driver
	}
	if strings.TrimSpace(override.SourceDir) != "" {
		migration.SourceDir = override.SourceDir
	}
	if override.Database.Env != "" || override.Database.DSN != "" {
		migration.Database = override.Database
	}
	if override.Baseline.Ref != "" || override.Baseline.Mode != "" {
		migration.Baseline = override.Baseline
	}
	if override.ValidationSet {
		migration.Validation = override.Validation
	}
	return migration, true, nil
}

func resolveMigrationDSN(migration config.CIMigrationConfig) (string, error) {
	envVar := strings.TrimSpace(migration.Database.Env)
	if envVar != "" {
		if value := os.Getenv(envVar); value != "" {
			return value, nil
		}
		return "", fmt.Errorf("database env %q is not set", envVar)
	}
	if strings.TrimSpace(migration.Database.DSN) != "" {
		return migration.Database.DSN, nil
	}
	return "", fmt.Errorf("database env or dsn is required")
}
