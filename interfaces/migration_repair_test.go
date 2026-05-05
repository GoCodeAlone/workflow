package interfaces_test

import (
	"strings"
	"testing"

	"github.com/GoCodeAlone/workflow/interfaces"
)

func TestMigrationRepairRequestValidateRequiresGuardFields(t *testing.T) {
	req := interfaces.MigrationRepairRequest{
		AppResourceName:      "bmw-app",
		DatabaseResourceName: "bmw-database",
		JobImage:             "registry.example/workflow-migrate:sha",
		SourceDir:            "/migrations",
	}

	err := req.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	for _, want := range []string{"expected_dirty_version", "force_version", "confirm_force"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error %q missing %q", err, want)
		}
	}
}

func TestMigrationRepairRequestValidateRequiresConfirmationValue(t *testing.T) {
	req := interfaces.MigrationRepairRequest{
		AppResourceName:      "bmw-app",
		DatabaseResourceName: "bmw-database",
		JobImage:             "registry.example/workflow-migrate:sha",
		SourceDir:            "/migrations",
		ExpectedDirtyVersion: "20260426000005",
		ForceVersion:         "20260422000001",
		ConfirmForce:         "yes",
	}

	err := req.Validate()
	if err == nil {
		t.Fatal("expected validation error")
	}
	if !strings.Contains(err.Error(), "FORCE_MIGRATION_METADATA") {
		t.Fatalf("error %q missing required confirmation value", err)
	}
}

func TestMigrationRepairRequestValidateAcceptsCompleteRequest(t *testing.T) {
	req := interfaces.MigrationRepairRequest{
		AppResourceName:      "bmw-app",
		DatabaseResourceName: "bmw-database",
		JobImage:             "registry.example/workflow-migrate:sha",
		SourceDir:            "/migrations",
		ExpectedDirtyVersion: "20260426000005",
		ForceVersion:         "20260422000001",
		ThenUp:               true,
		ConfirmForce:         interfaces.MigrationRepairConfirmation,
		Env:                  map[string]string{"DATABASE_URL": "postgres://example"},
	}

	if err := req.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}
