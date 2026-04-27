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
