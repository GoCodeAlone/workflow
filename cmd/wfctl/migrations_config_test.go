package main

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/config"
)

func TestResolveMigrationConfigsDefaultsPluginAndDriver(t *testing.T) {
	cfg := &config.WorkflowConfig{CI: &config.CIConfig{Migrations: []config.CIMigrationConfig{{
		Name:      "app",
		SourceDir: "migrations",
		Database:  config.CIMigrationDatabaseConfig{Env: "DATABASE_URL"},
	}}}}
	t.Setenv("DATABASE_URL", "postgres://secret@example/db")

	got, err := resolveMigrationConfigs(cfg, "staging")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Plugin != "workflow-plugin-migrations" || got[0].Driver != "golang-migrate" {
		t.Fatalf("defaults not applied: %+v", got[0])
	}
	if got[0].DSN != "postgres://secret@example/db" {
		t.Fatal("dsn not resolved from env")
	}
}

func TestResolveMigrationConfigsAppliesEnvironmentOverrides(t *testing.T) {
	cfg := &config.WorkflowConfig{CI: &config.CIConfig{Migrations: []config.CIMigrationConfig{{
		Name:      "app",
		Plugin:    "workflow-plugin-migrations",
		Driver:    "golang-migrate",
		SourceDir: "migrations",
		Database:  config.CIMigrationDatabaseConfig{Env: "DATABASE_URL"},
		Environments: map[string]*config.CIMigrationEnvironmentConfig{
			"prod": {
				SourceDir: "migrations/prod",
				Database:  config.CIMigrationDatabaseConfig{Env: "PROD_DATABASE_URL"},
			},
		},
	}}}}
	t.Setenv("DATABASE_URL", "postgres://staging-secret@example/db")
	t.Setenv("PROD_DATABASE_URL", "postgres://prod-secret@example/db")

	got, err := resolveMigrationConfigs(cfg, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if got[0].SourceDir != "migrations/prod" {
		t.Fatalf("source_dir = %q", got[0].SourceDir)
	}
	if got[0].DSN != "postgres://prod-secret@example/db" {
		t.Fatalf("dsn = %q", got[0].DSN)
	}
}

func TestResolveMigrationConfigsSkipsDisabledEnvironment(t *testing.T) {
	cfg := &config.WorkflowConfig{CI: &config.CIConfig{Migrations: []config.CIMigrationConfig{{
		Name:      "app",
		SourceDir: "migrations",
		Database:  config.CIMigrationDatabaseConfig{DSN: "postgres://secret@example/db"},
		Environments: map[string]*config.CIMigrationEnvironmentConfig{
			"prod": nil,
		},
	}}}}

	got, err := resolveMigrationConfigs(cfg, "prod")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected disabled migration to be skipped, got %+v", got)
	}
}

func TestResolveMigrationConfigsReadsDSNFromEnvWithoutLoggingValue(t *testing.T) {
	const secretDSN = "postgres://secret@example/db"
	cfg := &config.WorkflowConfig{CI: &config.CIConfig{Migrations: []config.CIMigrationConfig{{
		Name:     "app",
		Database: config.CIMigrationDatabaseConfig{Env: "DATABASE_URL"},
	}}}}
	t.Setenv("DATABASE_URL", secretDSN)

	_, err := resolveMigrationConfigs(cfg, "staging")
	if err == nil {
		t.Fatal("expected missing source_dir error")
	}
	if strings.Contains(err.Error(), secretDSN) {
		t.Fatalf("error leaked resolved DSN: %v", err)
	}
	if !strings.Contains(err.Error(), "source_dir") {
		t.Fatalf("expected source_dir error, got %v", err)
	}
}

func TestResolveMigrationConfigsRejectsMissingSourceDir(t *testing.T) {
	cfg := &config.WorkflowConfig{CI: &config.CIConfig{Migrations: []config.CIMigrationConfig{{
		Name:     "app",
		Database: config.CIMigrationDatabaseConfig{DSN: "postgres://secret@example/db"},
	}}}}

	_, err := resolveMigrationConfigs(cfg, "staging")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "source_dir") {
		t.Fatalf("expected source_dir error, got %v", err)
	}
}
