package interfaces

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

const (
	MigrationRepairConfirmation = "FORCE_MIGRATION_METADATA"

	MigrationRepairStatusSucceeded        = "succeeded"
	MigrationRepairStatusFailed           = "failed"
	MigrationRepairStatusApprovalRequired = "approval_required"
	MigrationRepairStatusUnsupported      = "unsupported"
)

type MigrationRepairRequest struct {
	AppResourceName      string            `json:"app_resource_name"`
	DatabaseResourceName string            `json:"database_resource_name"`
	JobImage             string            `json:"job_image"`
	SourceDir            string            `json:"source_dir"`
	ExpectedDirtyVersion string            `json:"expected_dirty_version"`
	ForceVersion         string            `json:"force_version"`
	ThenUp               bool              `json:"then_up"`
	UpIfClean            bool              `json:"up_if_clean"`
	ConfirmForce         string            `json:"confirm_force"`
	Env                  map[string]string `json:"env,omitempty"`
	TimeoutSeconds       int               `json:"timeout_seconds,omitempty"`
}

func (r MigrationRepairRequest) Validate() error {
	var missing []string
	if strings.TrimSpace(r.AppResourceName) == "" {
		missing = append(missing, "app_resource_name")
	}
	if strings.TrimSpace(r.DatabaseResourceName) == "" {
		missing = append(missing, "database_resource_name")
	}
	if strings.TrimSpace(r.JobImage) == "" {
		missing = append(missing, "job_image")
	}
	if strings.TrimSpace(r.SourceDir) == "" {
		missing = append(missing, "source_dir")
	}
	if strings.TrimSpace(r.ExpectedDirtyVersion) == "" {
		missing = append(missing, "expected_dirty_version")
	}
	if strings.TrimSpace(r.ForceVersion) == "" {
		missing = append(missing, "force_version")
	}
	if strings.TrimSpace(r.ConfirmForce) == "" {
		missing = append(missing, "confirm_force")
	}
	if len(missing) > 0 {
		return fmt.Errorf("migration repair request missing required fields: %s", strings.Join(missing, ", "))
	}
	if r.ConfirmForce != MigrationRepairConfirmation {
		return fmt.Errorf("confirm_force must equal %q", MigrationRepairConfirmation)
	}
	if r.ThenUp && r.UpIfClean {
		return errors.New("then_up and up_if_clean are mutually exclusive")
	}
	return nil
}

type MigrationRepairResult struct {
	ProviderJobID string       `json:"provider_job_id,omitempty"`
	Status        string       `json:"status"`
	Applied       []string     `json:"applied,omitempty"`
	Logs          string       `json:"logs,omitempty"`
	Diagnostics   []Diagnostic `json:"diagnostics,omitempty"`
}

type ProviderMigrationRepairer interface {
	RepairDirtyMigration(ctx context.Context, req MigrationRepairRequest) (*MigrationRepairResult, error)
}
